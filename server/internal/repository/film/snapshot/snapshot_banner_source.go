package snapshot

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/support"
)

const (
	bannerIndexCatUpdate = "idx_cat_key_upd_stamp"
	bannerIndexCatHits   = "idx_cat_key_hits_val"
)

type bannerRank struct {
	Mid         int64   `gorm:"column:mid"`
	Hits        int64   `gorm:"column:hits"`
	Score       float64 `gorm:"column:score"`
	UpdateStamp int64   `gorm:"column:update_stamp"`
}

// bannerCandidatesForSource 每个分类键按对应索引只取前 limit 部，再合并。
// 不再对整站做一次排序。没有播放线路的影片在取前几部时就被跳过。
func bannerCandidatesForSource(ctx context.Context, strategy, sourceID string, categoryIDs []int64, limit int, exclude []int64) ([]model.FilmListSnapshot, error) {
	keys := bannerSourceCategoryKeys(sourceID, categoryIDs)
	if len(keys) == 0 {
		return []model.FilmListSnapshot{}, nil
	}
	ranks, err := scanBannerRanks(ctx, strategy, sourceID, keys, limit, exclude, true)
	if err != nil {
		return nil, err
	}
	picked := takeBannerRanks(strategy, ranks, limit)
	if strategy == "score_random" && len(picked) < limit {
		skip := append(append([]int64{}, exclude...), bannerRankMids(picked)...)
		more, err := scanBannerRanks(ctx, strategy, sourceID, keys, limit-len(picked), skip, false)
		if err != nil {
			return nil, err
		}
		picked = append(picked, takeBannerRanks(strategy, more, limit-len(picked))...)
	}
	return listSnapshotsByMIDs(db.Mdb.WithContext(ctx), bannerRankMids(picked))
}

func bannerSourceCategoryKeys(sourceID string, categoryIDs []int64) []string {
	keys := make([]string, 0, len(categoryIDs))
	seen := make(map[string]struct{}, len(categoryIDs))
	for _, id := range categoryIDs {
		for _, typeID := range repository.PublicSourceTypeIDs(sourceID, "pid", id) {
			key := support.BuildSourceCategoryKey(sourceID, typeID)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	return keys
}

func scanBannerRanks(ctx context.Context, strategy, sourceID string, keys []string, limit int, exclude []int64, scoreMin bool) ([]bannerRank, error) {
	if limit <= 0 || len(keys) == 0 {
		return nil, nil
	}
	indexName, order := bannerStrategyPlan(strategy)
	if strategy != "score_random" {
		scoreMin = false
	}
	conn := db.Mdb.WithContext(ctx)
	dialect := dialectName(conn)
	parts := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys)*(4+len(exclude)))
	for _, key := range keys {
		parts = append(parts, bannerBranchSQL(dialect, indexName, order, scoreMin, exclude))
		args = append(args, key)
		if scoreMin {
			args = append(args, 6.0)
		}
		for _, mid := range exclude {
			args = append(args, mid)
		}
		args = append(args, sourceID, model.LineKindPlay, limit)
	}
	var ranks []bannerRank
	if err := conn.Raw(strings.Join(parts, " UNION ALL "), args...).Scan(&ranks).Error; err != nil {
		return nil, err
	}
	return ranks, nil
}

func bannerStrategyPlan(strategy string) (indexName, order string) {
	switch strategy {
	case "latest_random":
		return bannerIndexCatUpdate, "update_stamp DESC, mid DESC"
	case "score_random":
		return repository.FilmCategoryScoreIndex, "score DESC, hits DESC, mid DESC"
	default:
		return bannerIndexCatHits, "hits DESC, mid DESC"
	}
}

func bannerBranchSQL(dialect, indexName, order string, scoreMin bool, exclude []int64) string {
	from := model.TableFilmIndex
	if dialect == "mysql" && indexName != "" {
		from += " USE INDEX (`" + indexName + "`)"
	}
	scoreSQL := ""
	if scoreMin {
		scoreSQL = " AND score >= ?"
	}
	excludeSQL := ""
	if len(exclude) > 0 {
		excludeSQL = " AND mid NOT IN (" + strings.TrimRight(strings.Repeat("?,", len(exclude)), ",") + ")"
	}
	inner := fmt.Sprintf(
		"SELECT mid, hits, score, update_stamp FROM %s WHERE deleted_at IS NULL AND category_key = ?%s%s AND EXISTS (SELECT 1 FROM %s AS p WHERE p.mid = %s.mid AND p.source_id = ? AND p.line_kind = ?) ORDER BY %s LIMIT ?",
		from, scoreSQL, excludeSQL, model.TableFilmSourcePlaylist, model.TableFilmIndex, order,
	)
	// USE INDEX 不能直接跟在 UNION ALL 后面，包进子查询后排序和条数才作用在这一支上。
	return "SELECT mid, hits, score, update_stamp FROM (" + inner + ") AS banner_branch"
}

func takeBannerRanks(strategy string, ranks []bannerRank, limit int) []bannerRank {
	if len(ranks) == 0 || limit <= 0 {
		return nil
	}
	sort.Slice(ranks, func(i, j int) bool {
		return bannerRankBefore(strategy, ranks[i], ranks[j])
	})
	seen := make(map[int64]struct{}, len(ranks))
	out := make([]bannerRank, 0, limit)
	for _, rank := range ranks {
		if rank.Mid <= 0 {
			continue
		}
		if _, ok := seen[rank.Mid]; ok {
			continue
		}
		seen[rank.Mid] = struct{}{}
		out = append(out, rank)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func bannerRankBefore(strategy string, a, b bannerRank) bool {
	switch strategy {
	case "latest_random":
		if a.UpdateStamp != b.UpdateStamp {
			return a.UpdateStamp > b.UpdateStamp
		}
	case "score_random":
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Hits != b.Hits {
			return a.Hits > b.Hits
		}
	default:
		if a.Hits != b.Hits {
			return a.Hits > b.Hits
		}
	}
	return a.Mid > b.Mid
}

func bannerRankMids(ranks []bannerRank) []int64 {
	if len(ranks) == 0 {
		return nil
	}
	mids := make([]int64, 0, len(ranks))
	for _, rank := range ranks {
		if rank.Mid > 0 {
			mids = append(mids, rank.Mid)
		}
	}
	return mids
}
