package snapshot

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"

	"server/internal/film/catalog"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/taxonomy"

	"gorm.io/gorm"
)

const (
	bannerIndexCatUpdate   = "idx_cat_key_upd_stamp"
	bannerIndexCatHits     = "idx_cat_key_hits_val"
	bannerIndexCidUpdate   = "idx_cid_update"
	bannerIndexCidHits     = "idx_cid_hits"
	bannerIndexCid         = "idx_film_index_cid"
	bannerIndexPid         = "idx_film_index_pid"
	smallSourceFallbackCap = 2000
)

var bannerReadyIndexes sync.Map

type bannerRank struct {
	Mid         int64   `gorm:"column:mid"`
	Hits        int64   `gorm:"column:hits"`
	Score       float64 `gorm:"column:score"`
	UpdateStamp int64   `gorm:"column:update_stamp"`
}

// bannerCandidatesForSource 每个分类键按对应索引只取前 limit 部，再合并。
// 若本站首采影片数量不足，再补充同一展示分类下挂载了本站线路的聚合影片。
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
	if len(picked) < limit {
		localCids, err := bannerSourceLocalCategoryIDs(ctx, sourceID, categoryIDs)
		if err != nil {
			return nil, err
		}
		if len(localCids) > 0 {
			skip := append(append([]int64{}, exclude...), bannerRankMids(picked)...)
			more, err := scanBannerFallbackRanks(ctx, strategy, sourceID, localCids, limit-len(picked), skip, strategy == "score_random")
			if err != nil {
				return nil, err
			}
			picked = append(picked, takeBannerRanks(strategy, more, limit-len(picked))...)
			if strategy == "score_random" && len(picked) < limit {
				skip = append(append([]int64{}, exclude...), bannerRankMids(picked)...)
				more, err = scanBannerFallbackRanks(ctx, strategy, sourceID, localCids, limit-len(picked), skip, false)
				if err != nil {
					return nil, err
				}
				picked = append(picked, takeBannerRanks(strategy, more, limit-len(picked))...)
			}
		}
	}
	return listSnapshotsByMIDs(db.Mdb.WithContext(ctx), bannerRankMids(picked))
}

// bannerSourceLocalCategoryIDs 收集目标展示分类在当前站的精确 cid 以及跨站同名分类 cid，
// 避免放宽到根分类 pid 后把同根下的隐藏/成人子分类影片混入轮播。
func bannerSourceLocalCategoryIDs(ctx context.Context, sourceID string, categoryIDs []int64) ([]int64, error) {
	seenIDs := make(map[int64]struct{})
	cids := make([]int64, 0, len(categoryIDs))
	addID := func(id int64) {
		if id <= 0 {
			return
		}
		if _, ok := seenIDs[id]; ok {
			return
		}
		seenIDs[id] = struct{}{}
		cids = append(cids, id)
	}
	seenNames := make(map[string]struct{})
	names := make([]string, 0, len(categoryIDs))
	for _, id := range categoryIDs {
		for _, typeID := range catalog.PublicSourceTypeIDs(sourceID, "pid", id) {
			addID(taxonomy.GetLocalCategoryId(sourceID, typeID))
		}
		for _, name := range catalog.PublicSourceCategoryNames(sourceID, id) {
			if _, ok := seenNames[name]; ok {
				continue
			}
			seenNames[name] = struct{}{}
			names = append(names, name)
		}
	}
	if len(names) > 0 && db.Mdb != nil {
		var matchedIDs []int64
		if err := db.Mdb.WithContext(ctx).Model(&model.Category{}).
			Where("name IN ?", names).
			Pluck("id", &matchedIDs).Error; err != nil {
			return nil, err
		}
		for _, id := range matchedIDs {
			addID(id)
		}
	}
	return cids, nil
}

func scanBannerFallbackRanks(ctx context.Context, strategy, sourceID string, cids []int64, limit int, exclude []int64, scoreMin bool) ([]bannerRank, error) {
	if limit <= 0 || len(cids) == 0 {
		return nil, nil
	}
	if sourceMIDs, limited, err := smallSourceFallbackMIDs(ctx, sourceID); err != nil {
		return nil, err
	} else if !limited {
		if len(sourceMIDs) == 0 {
			return nil, nil
		}
		return scanSmallSourceFallbackRanks(ctx, strategy, cids, sourceMIDs, limit, exclude, scoreMin)
	}
	// 兜底改按单 cid 拆支走索引取前几部后再合并，避免 cid IN (...) 触发全表扫与 filesort 超时。
	return scanBannerBranches(ctx, strategy, sourceID, "cid", cids, true, limit, exclude, scoreMin)
}

func smallSourceFallbackMIDs(ctx context.Context, sourceID string) (mids []int64, exceeded bool, err error) {
	conn := db.Mdb.WithContext(ctx)
	from := model.TableFilmSourcePlaylist
	if dialectName(conn) == "mysql" && bannerTableIndexReady(conn, model.TableFilmSourcePlaylist, "idx_playlist_source_mid") {
		from += " USE INDEX (`idx_playlist_source_mid`)"
	}
	if err := conn.Table(from).
		Select("DISTINCT mid").
		Where("source_id = ? AND line_kind = ?", sourceID, model.LineKindPlay).
		Limit(smallSourceFallbackCap + 1).
		Scan(&mids).Error; err != nil {
		return nil, false, err
	}
	if len(mids) > smallSourceFallbackCap {
		return nil, true, nil
	}
	return mids, false, nil
}

func scanSmallSourceFallbackRanks(ctx context.Context, strategy string, cids, sourceMIDs []int64, limit int, exclude []int64, scoreMin bool) ([]bannerRank, error) {
	_, order := bannerStrategyPlan(strategy, true)
	q := db.Mdb.WithContext(ctx).Table(model.TableFilmIndex).
		Select("mid, hits, score, update_stamp").
		Where("deleted_at IS NULL AND mid IN ? AND cid IN ?", sourceMIDs, cids)
	if strategy == "score_random" && scoreMin {
		q = q.Where("score >= ?", 6.0)
	}
	if len(exclude) > 0 {
		q = q.Where("mid NOT IN ?", exclude)
	}
	queryLimit := limit
	if strategy == "random" {
		// 随机策略下允许在候选集内全量洗牌（上限为 smallSourceFallbackCap），避免总是命中前 limit 条。
		queryLimit = smallSourceFallbackCap
	}
	var ranks []bannerRank
	if err := q.Order(order).Limit(queryLimit).Scan(&ranks).Error; err != nil {
		return nil, err
	}
	return ranks, nil
}

func bannerSourceCategoryKeys(sourceID string, categoryIDs []int64) []string {
	keys := make([]string, 0, len(categoryIDs))
	seen := make(map[string]struct{}, len(categoryIDs))
	for _, id := range categoryIDs {
		for _, typeID := range catalog.PublicSourceTypeIDs(sourceID, "pid", id) {
			key := taxonomy.BuildSourceCategoryKey(sourceID, typeID)
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
	return scanBannerBranches(ctx, strategy, sourceID, "category_key", keys, false, limit, exclude, scoreMin)
}

func scanBannerBranches[T string | int64](ctx context.Context, strategy, sourceID, column string, values []T, byCID bool, limit int, exclude []int64, scoreMin bool) ([]bannerRank, error) {
	if limit <= 0 || len(values) == 0 {
		return nil, nil
	}
	if strategy == "random" {
		return scanRandomBannerBranches(ctx, sourceID, column, values, byCID, limit, exclude)
	}
	_, order := bannerStrategyPlan(strategy, byCID)
	if strategy != "score_random" {
		scoreMin = false
	}
	conn := db.Mdb.WithContext(ctx)
	dialect := dialectName(conn)
	indexHint := bannerUseIndexClause(conn, dialect, strategy, byCID)
	branchSQL := bannerBranchSQL(dialect, indexHint, column, order, scoreMin, "", exclude)

	parts := make([]string, 0, len(values))
	args := make([]any, 0, len(values)*(4+len(exclude)))
	for _, val := range values {
		parts = append(parts, branchSQL)
		args = append(args, val)
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

type bannerMidBounds struct {
	Min int64 `gorm:"column:min_mid"`
	Max int64 `gorm:"column:max_mid"`
}

func randomBannerPivot(ctx context.Context) (int64, error) {
	var bounds bannerMidBounds
	if err := db.Mdb.WithContext(ctx).Table(model.TableFilmIndex).
		Select("COALESCE(MIN(mid), 0) AS min_mid, COALESCE(MAX(mid), 0) AS max_mid").
		Scan(&bounds).Error; err != nil {
		return 0, err
	}
	if bounds.Max <= bounds.Min || bounds.Max <= 0 {
		return bounds.Min, nil
	}
	return bounds.Min + rand.Int63n(bounds.Max-bounds.Min+1), nil
}

func randomBannerWindowSize(limit int) int {
	n := limit * 6
	if n < 30 {
		n = 30
	}
	if n > 100 {
		n = 100
	}
	return n
}

func scanRandomBannerBranches[T string | int64](ctx context.Context, sourceID, column string, values []T, byCID bool, limit int, exclude []int64) ([]bannerRank, error) {
	pivot, err := randomBannerPivot(ctx)
	if err != nil {
		return nil, err
	}
	conn := db.Mdb.WithContext(ctx)
	dialect := dialectName(conn)
	indexHint := bannerUseIndexClause(conn, dialect, "random", byCID)
	_, order := bannerStrategyPlan("random", byCID)
	window := randomBannerWindowSize(limit)

	runRange := func(pivotOp, rangeOrder string, fetchLimit int) ([]bannerRank, error) {
		if fetchLimit <= 0 {
			return nil, nil
		}
		branchSQL := bannerBranchSQL(dialect, indexHint, column, rangeOrder, false, " AND mid "+pivotOp+" ?", exclude)
		parts := make([]string, 0, len(values))
		args := make([]any, 0, len(values)*(5+len(exclude)))
		for _, val := range values {
			parts = append(parts, branchSQL)
			args = append(args, val, pivot)
			for _, mid := range exclude {
				args = append(args, mid)
			}
			args = append(args, sourceID, model.LineKindPlay, fetchLimit)
		}
		var out []bannerRank
		if err := conn.Raw(strings.Join(parts, " UNION ALL "), args...).Scan(&out).Error; err != nil {
			return nil, err
		}
		return out, nil
	}

	// 从随机主键值向后窗口抽取，避免 ORDER BY RAND() 全表扫描；不足时沿索引反向从枢轴向左补齐，避免固定命中头部影片。
	ranks, err := runRange(">=", order, window)
	if err != nil {
		return nil, err
	}
	if len(ranks) < window {
		more, err := runRange("<", "mid DESC", window-len(ranks))
		if err != nil {
			return nil, err
		}
		ranks = append(ranks, more...)
	}
	return ranks, nil
}

func bannerStrategyPlan(strategy string, byCID bool) (indexName, order string) {
	switch strategy {
	case "latest_random":
		if byCID {
			return bannerIndexCidUpdate, "update_stamp DESC, mid DESC"
		}
		return bannerIndexCatUpdate, "update_stamp DESC, mid DESC"
	case "score_random":
		if byCID {
			return catalog.FilmCidScoreIndex, "score DESC, hits DESC, mid DESC"
		}
		return catalog.FilmCategoryScoreIndex, "score DESC, hits DESC, mid DESC"
	case "random":
		if byCID {
			return bannerIndexCid, "mid ASC"
		}
		return catalog.FilmCategoryDeletedIndex, "mid ASC"
	default:
		if byCID {
			return bannerIndexCidHits, "hits DESC, mid DESC"
		}
		return bannerIndexCatHits, "hits DESC, mid DESC"
	}
}

func bannerUseIndexClause(conn *gorm.DB, dialect, strategy string, byCID bool) string {
	if dialect != "mysql" {
		return ""
	}
	indexName, _ := bannerStrategyPlan(strategy, byCID)
	if indexName == "" || !bannerTableIndexReady(conn, model.TableFilmIndex, indexName) {
		return ""
	}
	return " USE INDEX (`" + indexName + "`)"
}

func bannerTableIndexReady(conn *gorm.DB, tableName, indexName string) bool {
	if conn == nil {
		return false
	}
	if conn.DryRun {
		return true
	}
	if conn.ConnPool == nil {
		return false
	}
	type cacheKey struct {
		pool  gorm.ConnPool
		table string
		index string
	}
	key := cacheKey{pool: conn.ConnPool, table: tableName, index: indexName}
	if ready, ok := bannerReadyIndexes.Load(key); ok {
		return ready.(bool)
	}
	var n int64
	err := conn.Raw(`
		SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?
	`, tableName, indexName).Scan(&n).Error
	if err == nil && n > 0 {
		bannerReadyIndexes.Store(key, true)
		return true
	}
	return false
}

func bannerBranchSQL(dialect, filmIndexHint, column, order string, scoreMin bool, extraWhere string, exclude []int64) string {
	from := model.TableFilmIndex + filmIndexHint
	playlistFrom := model.TableFilmSourcePlaylist + " AS p"
	if dialect == "mysql" {
		// 强制 EXISTS 从主键 (mid, source_id, line_kind) 探测，避免优化器反转为先扫 playlist 再 filesort。
		playlistFrom += " FORCE INDEX (PRIMARY)"
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
		"SELECT mid, hits, score, update_stamp FROM %s WHERE deleted_at IS NULL AND %s = ?%s%s%s AND EXISTS (SELECT 1 FROM %s WHERE p.mid = %s.mid AND p.source_id = ? AND p.line_kind = ?) ORDER BY %s LIMIT ?",
		from, column, scoreSQL, extraWhere, excludeSQL, playlistFrom, model.TableFilmIndex, order,
	)
	// USE INDEX 不能直接跟在 UNION ALL 后面，包进子查询后排序和条数才作用在这一支上。
	return "SELECT mid, hits, score, update_stamp FROM (" + inner + ") AS banner_branch"
}

func takeBannerRanks(strategy string, ranks []bannerRank, limit int) []bannerRank {
	if len(ranks) == 0 || limit <= 0 {
		return nil
	}
	ordered := ranks
	if strategy == "random" {
		ordered = append([]bannerRank(nil), ranks...)
		rand.Shuffle(len(ordered), func(i, j int) {
			ordered[i], ordered[j] = ordered[j], ordered[i]
		})
	} else {
		sort.Slice(ordered, func(i, j int) bool {
			return bannerRankBefore(strategy, ordered[i], ordered[j])
		})
	}
	seen := make(map[int64]struct{}, len(ordered))
	out := make([]bannerRank, 0, limit)
	for _, rank := range ordered {
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
