package snapshot

import (
	"fmt"
	"sort"
	"strings"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/film/query"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

const (
	categoryProbeMaxRows = 200
	categoryProbeMaxKeys = 400

	probeIndexRootUpdate = "idx_root_key_upd_stamp"
	probeIndexCatUpdate  = "idx_cat_key_upd_stamp"
	probeIndexPidUpdate  = "idx_film_index_pid_update_mid"
)

type probeRank struct {
	Mid         int64 `gorm:"column:mid"`
	UpdateStamp int64 `gorm:"column:update_stamp"`
}

type probeBranch struct {
	column string
	value  any
	index  string
}

// probeCategorySourceList 按分类键的有序索引各取一段，再用播放线路主键点查该站，合并后取够即停。
// 空分类每个键都是空范围，不会扫完整库。offset+limit 过大时返回 used=false，调用方走原查询。
func probeCategorySourceList(conn *gorm.DB, sourceID, field string, categoryID int64, offset, limit int) ([]model.FilmListSnapshot, bool, error) {
	sourceID = strings.TrimSpace(sourceID)
	if conn == nil || sourceID == "" || limit <= 0 || offset < 0 || offset+limit > categoryProbeMaxRows {
		return nil, false, nil
	}
	need := offset + limit
	if sourceKeys := sourceTypeKeysForProbe(sourceID, field, categoryID); len(sourceKeys) > 0 {
		ranks, err := scanProbeBranches(conn, sourceTypeProbeBranches(sourceKeys), sourceID, need)
		if err != nil {
			return nil, true, err
		}
		mids := pageProbeMids(ranks, offset, limit)
		snaps, err := listSnapshotsByMIDs(conn, mids)
		if err != nil {
			return nil, true, err
		}
		return snaps, true, nil
	}
	idColumn, id, categoryKeys, rootKeys := query.LiveCategoryProbeKeys(field, categoryID)
	branches := buildProbeBranches(idColumn, id, categoryKeys, rootKeys)
	if len(branches) == 0 || len(branches) > categoryProbeMaxKeys {
		return nil, false, nil
	}

	ranks, err := scanProbeBranches(conn, branches, sourceID, need)
	if err != nil {
		return nil, true, err
	}
	mids := pageProbeMids(ranks, offset, limit)
	snaps, err := listSnapshotsByMIDs(conn, mids)
	if err != nil {
		return nil, true, err
	}
	return snaps, true, nil
}

func sourceTypeKeysForProbe(sourceID, field string, categoryID int64) []string {
	ids := repository.PublicSourceTypeIDs(sourceID, field, categoryID)
	keys := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		key := support.BuildSourceCategoryKey(sourceID, id)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

// sourceTypeProbeBranches 按当前站分类树展开后的 type_id 点查 category_key。
func sourceTypeProbeBranches(sourceKeys []string) []probeBranch {
	branches := make([]probeBranch, 0, len(sourceKeys))
	for _, sourceKey := range sourceKeys {
		if sourceKey == "" {
			continue
		}
		branches = append(branches, probeBranch{column: "category_key", value: sourceKey, index: probeIndexCatUpdate})
	}
	return branches
}

func buildProbeBranches(idColumn string, id int64, categoryKeys, rootKeys []string) []probeBranch {
	branches := make([]probeBranch, 0, 1+len(categoryKeys)+len(rootKeys))
	if id > 0 && (idColumn == "pid" || idColumn == "cid") {
		index := ""
		if idColumn == "pid" {
			index = probeIndexPidUpdate
		}
		branches = append(branches, probeBranch{column: idColumn, value: id, index: index})
	}
	rootIndex := probeIndexRootUpdate
	catIndex := probeIndexCatUpdate
	for _, key := range rootKeys {
		if key == "" {
			continue
		}
		branches = append(branches, probeBranch{column: "root_category_key", value: key, index: rootIndex})
	}
	for _, key := range categoryKeys {
		if key == "" {
			continue
		}
		branches = append(branches, probeBranch{column: "category_key", value: key, index: catIndex})
	}
	return branches
}

func probeBranchSQL(dialect string, branch probeBranch) string {
	from := model.TableFilmIndex
	if dialect == "mysql" && branch.index != "" {
		from += " USE INDEX (`" + branch.index + "`)"
	}
	inner := fmt.Sprintf(
		"SELECT mid, update_stamp FROM %s WHERE deleted_at IS NULL AND %s = ? AND EXISTS (SELECT 1 FROM %s AS p WHERE p.mid = %s.mid AND p.source_id = ? AND p.line_kind = ?) ORDER BY update_stamp DESC, mid DESC LIMIT ?",
		from, branch.column, model.TableFilmSourcePlaylist, model.TableFilmIndex,
	)
	return "SELECT mid, update_stamp FROM (" + inner + ") AS probe_branch"
}

func scanProbeBranches(conn *gorm.DB, branches []probeBranch, sourceID string, need int) ([]probeRank, error) {
	dialect := dialectName(conn)
	parts := make([]string, 0, len(branches))
	args := make([]any, 0, len(branches)*4)
	for _, branch := range branches {
		parts = append(parts, probeBranchSQL(dialect, branch))
		args = append(args, branch.value, sourceID, model.LineKindPlay, need)
	}
	sql := strings.Join(parts, " UNION ALL ")
	var ranks []probeRank
	if err := conn.Raw(sql, args...).Scan(&ranks).Error; err != nil {
		return nil, err
	}
	return ranks, nil
}

func pageProbeMids(ranks []probeRank, offset, limit int) []int64 {
	if len(ranks) == 0 {
		return nil
	}
	sort.SliceStable(ranks, func(i, j int) bool {
		if ranks[i].UpdateStamp != ranks[j].UpdateStamp {
			return ranks[i].UpdateStamp > ranks[j].UpdateStamp
		}
		return ranks[i].Mid > ranks[j].Mid
	})
	seen := make(map[int64]struct{}, len(ranks))
	mids := make([]int64, 0, limit)
	skipped := 0
	for _, rank := range ranks {
		if rank.Mid <= 0 {
			continue
		}
		if _, ok := seen[rank.Mid]; ok {
			continue
		}
		seen[rank.Mid] = struct{}{}
		if skipped < offset {
			skipped++
			continue
		}
		mids = append(mids, rank.Mid)
		if len(mids) >= limit {
			break
		}
	}
	return mids
}

func listSnapshotsByMIDs(conn *gorm.DB, mids []int64) ([]model.FilmListSnapshot, error) {
	if len(mids) == 0 {
		return []model.FilmListSnapshot{}, nil
	}
	var indexes []model.FilmIndex
	if err := conn.Model(&model.FilmIndex{}).Select(basicSelectFields).Where("mid IN ?", mids).Find(&indexes).Error; err != nil {
		return nil, err
	}
	byID := make(map[int64]model.FilmIndex, len(indexes))
	for _, row := range indexes {
		byID[row.Mid] = row
	}
	ordered := make([]model.FilmIndex, 0, len(mids))
	for _, id := range mids {
		if row, ok := byID[id]; ok {
			ordered = append(ordered, row)
		}
	}
	return indexesToListSnapshots(ordered), nil
}
