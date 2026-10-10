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
	probeIndexRootHits   = "idx_root_key_hits_val"
	probeIndexCatUpdate  = "idx_cat_key_upd_stamp"
	probeIndexCatHits    = "idx_cat_key_hits_val"
	probeIndexPidUpdate  = "idx_film_index_pid_update_mid"
)

type probeRank struct {
	Mid         int64 `gorm:"column:mid"`
	Hits        int64 `gorm:"column:hits"`
	UpdateStamp int64 `gorm:"column:update_stamp"`
}

type probeBranch struct {
	column string
	value  any
	index  string
}

// probeCategorySourceList 按分类键的有序索引各取一段，再用播放线路主键点查该站，合并后取够即停。
// 空分类每个键都是空范围，不会扫完整库。offset+limit 过大时返回 used=false，调用方走原查询。
func probeCategorySourceList(conn *gorm.DB, sourceID, field string, categoryID int64, orderKind string, offset, limit int) ([]model.FilmListSnapshot, bool, error) {
	sourceID = strings.TrimSpace(sourceID)
	if conn == nil || sourceID == "" || limit <= 0 || offset < 0 || offset+limit > categoryProbeMaxRows {
		return nil, false, nil
	}
	if orderKind != "hits" && orderKind != "update" {
		return nil, false, nil
	}
	need := offset + limit
	if sourceKeys := sourceTypeKeysForProbe(sourceID, field, categoryID); len(sourceKeys) > 0 {
		ranks, err := scanProbeBranches(conn, sourceTypeProbeBranches(field, orderKind, sourceKeys), sourceID, orderKind, need)
		if err != nil {
			return nil, true, err
		}
		mids := pageProbeMids(ranks, orderKind, offset, limit)
		snaps, err := listSnapshotsByMIDs(conn, mids)
		if err != nil {
			return nil, true, err
		}
		return snaps, true, nil
	}
	idColumn, id, categoryKeys, rootKeys := query.LiveCategoryProbeKeys(field, categoryID)
	branches := buildProbeBranches(orderKind, idColumn, id, categoryKeys, rootKeys)
	if len(branches) == 0 || len(branches) > categoryProbeMaxKeys {
		return nil, false, nil
	}

	ranks, err := scanProbeBranches(conn, branches, sourceID, orderKind, need)
	if err != nil {
		return nil, true, err
	}
	mids := pageProbeMids(ranks, orderKind, offset, limit)
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

// sourceTypeProbeBranches 按当前站 type_id 点查。
// 一级分类要同时走 root_category_key 和 category_key。合并后的分类每个成员各查一次。
func sourceTypeProbeBranches(field, orderKind string, sourceKeys []string) []probeBranch {
	rootIndex := probeIndexRootUpdate
	catIndex := probeIndexCatUpdate
	if orderKind == "hits" {
		rootIndex = probeIndexRootHits
		catIndex = probeIndexCatHits
	}
	branches := make([]probeBranch, 0, len(sourceKeys)*2)
	for _, sourceKey := range sourceKeys {
		if sourceKey == "" {
			continue
		}
		if field == "cid" {
			branches = append(branches, probeBranch{column: "category_key", value: sourceKey, index: catIndex})
			continue
		}
		branches = append(branches,
			probeBranch{column: "root_category_key", value: sourceKey, index: rootIndex},
			probeBranch{column: "category_key", value: sourceKey, index: catIndex},
		)
	}
	return branches
}

func buildProbeBranches(orderKind, idColumn string, id int64, categoryKeys, rootKeys []string) []probeBranch {
	branches := make([]probeBranch, 0, 1+len(categoryKeys)+len(rootKeys))
	if id > 0 && (idColumn == "pid" || idColumn == "cid") {
		index := ""
		if orderKind == "update" && idColumn == "pid" {
			index = probeIndexPidUpdate
		}
		branches = append(branches, probeBranch{column: idColumn, value: id, index: index})
	}
	rootIndex := probeIndexRootUpdate
	catIndex := probeIndexCatUpdate
	if orderKind == "hits" {
		rootIndex = probeIndexRootHits
		catIndex = probeIndexCatHits
	}
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

func probeOrderSQL(orderKind string) string {
	if orderKind == "hits" {
		return "hits DESC, mid DESC"
	}
	return "update_stamp DESC, mid DESC"
}

func probeBranchSQL(dialect string, branch probeBranch, orderKind string) string {
	from := model.TableFilmIndex
	if dialect == "mysql" && branch.index != "" {
		from += " USE INDEX (`" + branch.index + "`)"
	}
	inner := fmt.Sprintf(
		"SELECT mid, hits, update_stamp FROM %s WHERE deleted_at IS NULL AND %s = ? AND EXISTS (SELECT 1 FROM %s AS p WHERE p.mid = %s.mid AND p.source_id = ? AND p.line_kind = ?) ORDER BY %s LIMIT ?",
		from, branch.column, model.TableFilmSourcePlaylist, model.TableFilmIndex, probeOrderSQL(orderKind),
	)
	return "SELECT mid, hits, update_stamp FROM (" + inner + ") AS probe_branch"
}

func scanProbeBranches(conn *gorm.DB, branches []probeBranch, sourceID, orderKind string, need int) ([]probeRank, error) {
	dialect := dialectName(conn)
	parts := make([]string, 0, len(branches))
	args := make([]any, 0, len(branches)*4)
	for _, branch := range branches {
		parts = append(parts, probeBranchSQL(dialect, branch, orderKind))
		args = append(args, branch.value, sourceID, "play", need)
	}
	sql := strings.Join(parts, " UNION ALL ")
	var ranks []probeRank
	if err := conn.Raw(sql, args...).Scan(&ranks).Error; err != nil {
		return nil, err
	}
	return ranks, nil
}

func pageProbeMids(ranks []probeRank, orderKind string, offset, limit int) []int64 {
	if len(ranks) == 0 {
		return nil
	}
	sort.SliceStable(ranks, func(i, j int) bool {
		if orderKind == "hits" {
			if ranks[i].Hits != ranks[j].Hits {
				return ranks[i].Hits > ranks[j].Hits
			}
			return ranks[i].Mid > ranks[j].Mid
		}
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
