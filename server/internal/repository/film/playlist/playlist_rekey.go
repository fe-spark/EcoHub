package playlist

import (
	"encoding/json"
	"sort"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/film/shared"
	"server/internal/repository/film/snapshot"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	rekeyKeyChunkSize     = 500
	rekeyMidChunkSize     = 500
	rekeyWriteBatchSize   = 200
	rekeyDefaultBatchSize = 2000
	rekeyLogEveryN        = 20000
	playlistScanColumns   = "id, source_id, movie_key, group_index, group_name, updated_at"
)

type rekeySlot struct {
	sourceID   string
	movieKey   string
	groupIndex int
}

func canonicalPlaylistMovieKey(film model.FilmIndex) string {
	keys := shared.BuildMovieMatchKeysWithCategory(film.DbId, film.Name, filmIndexRootPid(film))
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func loadFilmIndexesByMids(mids []int64) (map[int64]model.FilmIndex, error) {
	mids = snapshot.NormalizeSnapshotMIDs(mids)
	out := make(map[int64]model.FilmIndex, len(mids))
	for _, chunk := range snapshot.ChunkSnapshotMIDs(mids, rekeyMidChunkSize) {
		var films []model.FilmIndex
		if err := db.Mdb.Where("mid IN ?", chunk).Find(&films).Error; err != nil {
			return nil, err
		}
		for _, film := range films {
			out[film.Mid] = film
		}
	}
	return out, nil
}

// SlavePlaylistRekeyTargets 旧键 → 该片规范主键。仅唯一命中一部影片、且目标不是多片共享键时才归并。
func SlavePlaylistRekeyTargets(keys []string) (map[string]string, error) {
	keys = shared.UniqueKeys(keys)
	if len(keys) == 0 {
		return nil, nil
	}

	midsByKey := shared.LoadMidCandidatesByMatchKeys(keys)
	allMids := make([]int64, 0, len(keys))
	for _, mids := range midsByKey {
		allMids = append(allMids, mids...)
	}
	films, err := loadFilmIndexesByMids(allMids)
	if err != nil {
		return nil, err
	}

	canonicalByMid := make(map[int64]string, len(films))
	destKeys := make([]string, 0, len(films))
	for mid, film := range films {
		dest := canonicalPlaylistMovieKey(film)
		if dest == "" {
			continue
		}
		canonicalByMid[mid] = dest
		destKeys = append(destKeys, dest)
	}
	destMidsByKey := shared.LoadMidCandidatesByMatchKeys(destKeys)

	targets := make(map[string]string, len(keys))
	for _, key := range keys {
		mids := snapshot.NormalizeSnapshotMIDs(midsByKey[key])
		if len(mids) != 1 {
			continue
		}
		dest := canonicalByMid[mids[0]]
		if dest == "" || dest == key {
			continue
		}
		owners := snapshot.NormalizeSnapshotMIDs(destMidsByKey[dest])
		if len(owners) > 1 {
			continue
		}
		if len(owners) == 1 && owners[0] != mids[0] {
			continue
		}
		targets[key] = dest
	}
	return targets, nil
}

func playlistJSONEpisodeCount(content string) int {
	var links []model.MovieUrlInfo
	if err := json.Unmarshal([]byte(content), &links); err != nil {
		return 0
	}
	return shared.EpisodeCount(links)
}

func loadSlavePlaylistsBySlots(slots []rekeySlot) (map[rekeySlot]model.SlaveMoviePlaylist, error) {
	if len(slots) == 0 {
		return nil, nil
	}
	sourceIDs := make([]string, 0, len(slots))
	movieKeys := make([]string, 0, len(slots))
	seenSource := make(map[string]struct{}, len(slots))
	seenKey := make(map[string]struct{}, len(slots))
	wanted := make(map[rekeySlot]struct{}, len(slots))
	for _, slot := range slots {
		wanted[slot] = struct{}{}
		if _, ok := seenSource[slot.sourceID]; !ok {
			seenSource[slot.sourceID] = struct{}{}
			sourceIDs = append(sourceIDs, slot.sourceID)
		}
		if _, ok := seenKey[slot.movieKey]; !ok {
			seenKey[slot.movieKey] = struct{}{}
			movieKeys = append(movieKeys, slot.movieKey)
		}
	}

	var rows []model.SlaveMoviePlaylist
	if err := db.Mdb.Where("source_id IN ? AND movie_key IN ?", sourceIDs, movieKeys).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[rekeySlot]model.SlaveMoviePlaylist, len(slots))
	for _, row := range rows {
		slot := rekeySlot{sourceID: row.SourceId, movieKey: row.MovieKey, groupIndex: row.GroupIndex}
		if _, ok := wanted[slot]; !ok {
			continue
		}
		out[slot] = row
	}
	return out, nil
}

func attachPlaylistContent(rows []model.SlaveMoviePlaylist) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(rows))
	index := make(map[uint]int, len(rows))
	for i, row := range rows {
		if row.ID == 0 {
			continue
		}
		ids = append(ids, row.ID)
		index[row.ID] = i
	}
	if len(ids) == 0 {
		return nil
	}
	type contentRow struct {
		ID      uint
		Content string
	}
	for start := 0; start < len(ids); start += rekeyKeyChunkSize {
		end := start + rekeyKeyChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		var parts []contentRow
		if err := db.Mdb.Model(&model.SlaveMoviePlaylist{}).
			Select("id", "content").
			Where("id IN ?", ids[start:end]).
			Find(&parts).Error; err != nil {
			return err
		}
		for _, part := range parts {
			if i, ok := index[part.ID]; ok {
				rows[i].Content = part.Content
			}
		}
	}
	return nil
}

func rekeySlavePlaylistRows(rows []model.SlaveMoviePlaylist, targetByKey map[string]string, dryRun bool) (int64, error) {
	if len(rows) == 0 || len(targetByKey) == 0 {
		return 0, nil
	}

	deleteIDs := make([]uint, 0, len(rows))
	upsertBySlot := make(map[rekeySlot]model.SlaveMoviePlaylist, len(rows))
	upsertOrder := make([]rekeySlot, 0, len(rows))
	for _, row := range rows {
		target := strings.TrimSpace(targetByKey[row.MovieKey])
		if target == "" {
			continue
		}
		if row.ID > 0 {
			deleteIDs = append(deleteIDs, row.ID)
		}

		moved := row
		moved.ID = 0
		moved.MovieKey = target
		slot := rekeySlot{sourceID: moved.SourceId, movieKey: moved.MovieKey, groupIndex: moved.GroupIndex}
		if existing, ok := upsertBySlot[slot]; ok {
			if playlistJSONEpisodeCount(moved.Content) < playlistJSONEpisodeCount(existing.Content) {
				continue
			}
		} else {
			upsertOrder = append(upsertOrder, slot)
		}
		upsertBySlot[slot] = moved
	}
	if len(deleteIDs) == 0 {
		return 0, nil
	}
	if dryRun {
		return int64(len(deleteIDs)), nil
	}

	existingTargets, err := loadSlavePlaylistsBySlots(upsertOrder)
	if err != nil {
		return 0, err
	}
	kept := make([]rekeySlot, 0, len(upsertOrder))
	for _, slot := range upsertOrder {
		moved := upsertBySlot[slot]
		if current, ok := existingTargets[slot]; ok {
			if playlistJSONEpisodeCount(current.Content) >= playlistJSONEpisodeCount(moved.Content) {
				delete(upsertBySlot, slot)
				continue
			}
		}
		kept = append(kept, slot)
	}

	upserts := make([]model.SlaveMoviePlaylist, 0, len(kept))
	for _, slot := range kept {
		upserts = append(upserts, upsertBySlot[slot])
	}
	sort.Slice(upserts, func(i, j int) bool {
		if upserts[i].SourceId != upserts[j].SourceId {
			return upserts[i].SourceId < upserts[j].SourceId
		}
		if upserts[i].MovieKey != upserts[j].MovieKey {
			return upserts[i].MovieKey < upserts[j].MovieKey
		}
		return upserts[i].GroupIndex < upserts[j].GroupIndex
	})

	err = db.Mdb.Transaction(func(tx *gorm.DB) error {
		for start := 0; start < len(deleteIDs); start += rekeyWriteBatchSize {
			end := start + rekeyWriteBatchSize
			if end > len(deleteIDs) {
				end = len(deleteIDs)
			}
			if err := tx.Unscoped().Where("id IN ?", deleteIDs[start:end]).Delete(&model.SlaveMoviePlaylist{}).Error; err != nil {
				return err
			}
		}
		if len(upserts) == 0 {
			return nil
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source_id"}, {Name: "movie_key"}, {Name: "group_index"}},
			DoUpdates: clause.AssignmentColumns([]string{"group_name", "content", "updated_at"}),
		}).CreateInBatches(&upserts, rekeyWriteBatchSize).Error
	})
	if err != nil {
		return 0, err
	}
	return int64(len(deleteIDs)), nil
}

func matchKeysEqual(left, right []string) bool {
	left = shared.UniqueKeys(left)
	right = shared.UniqueKeys(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// referencedPlaylistKeys 返回 keys 中仍被附属站播放列表引用的键。
func referencedPlaylistKeys(keys []string) (map[string]struct{}, error) {
	keys = shared.UniqueKeys(keys)
	if len(keys) == 0 {
		return nil, nil
	}
	referenced := make(map[string]struct{}, len(keys))
	for start := 0; start < len(keys); start += rekeyKeyChunkSize {
		end := start + rekeyKeyChunkSize
		if end > len(keys) {
			end = len(keys)
		}
		var found []string
		if err := db.Mdb.Model(&model.SlaveMoviePlaylist{}).
			Distinct().
			Where("movie_key IN ?", keys[start:end]).
			Pluck("movie_key", &found).Error; err != nil {
			return nil, err
		}
		for _, key := range found {
			referenced[key] = struct{}{}
		}
	}
	return referenced, nil
}

func rewriteMatchKeysForFilms(films []model.FilmIndex, dryRun bool) (int64, error) {
	if len(films) == 0 {
		return 0, nil
	}
	mids := make([]int64, 0, len(films))
	for _, film := range films {
		if film.Mid > 0 {
			mids = append(mids, film.Mid)
		}
	}
	existing := shared.LoadMovieMatchKeysByMids(snapshot.NormalizeSnapshotMIDs(mids))

	canonicalByMid := make(map[int64][]string, len(films))
	droppedKeys := make([]string, 0, len(films))
	for _, film := range films {
		if film.Mid <= 0 {
			continue
		}
		keys := shared.BuildMovieMatchKeysWithCategory(film.DbId, film.Name, filmIndexRootPid(film))
		if len(keys) == 0 || matchKeysEqual(existing[film.Mid], keys) {
			continue
		}
		canonicalByMid[film.Mid] = keys
		inCanonical := make(map[string]struct{}, len(keys))
		for _, key := range keys {
			inCanonical[key] = struct{}{}
		}
		for _, key := range existing[film.Mid] {
			if _, ok := inCanonical[key]; !ok {
				droppedKeys = append(droppedKeys, key)
			}
		}
	}
	if len(canonicalByMid) == 0 {
		return 0, nil
	}

	// 仍被播放列表引用的旧键不能删：删掉后这些行既失去归属（详情页播放源消失），
	// 第二阶段的归并也再查不到它们，24 小时后会被孤儿清理物理删除。
	referenced, err := referencedPlaylistKeys(droppedKeys)
	if err != nil {
		return 0, err
	}

	mappings := make(map[int64][]string, len(canonicalByMid))
	for mid, keys := range canonicalByMid {
		kept := make(map[string]struct{}, len(keys))
		for _, key := range keys {
			kept[key] = struct{}{}
		}
		// 兜底键追加在规范键之后，保证 keys[0] 仍是该片规范主键；下一轮主站采集会重建键集。
		for _, key := range existing[mid] {
			if _, ok := kept[key]; ok {
				continue
			}
			if _, ok := referenced[key]; !ok {
				continue
			}
			keys = append(keys, key)
			kept[key] = struct{}{}
		}
		mappings[mid] = keys
	}
	if dryRun {
		return int64(len(mappings)), nil
	}
	if err := shared.SaveMovieMatchKeysByMid(mappings); err != nil {
		return 0, err
	}
	return int64(len(mappings)), nil
}

// SlavePlaylistKeyMigrationResult 历史数据一次性迁移结果。
type SlavePlaylistKeyMigrationResult struct {
	Scanned   int64 // 扫描的附属站播放列表行数
	Migrated  int64 // 归并到所属影片主键的播放列表行数
	Skipped   int64 // 同名跨类 / 对不上主站影片
	MatchKeys int64 // 重写匹配键的主站影片数
}

// MigrateSlavePlaylistKeys 历史数据一次性迁移：先按片名#大类重写 movie_match_key
// （仍被播放列表引用的旧键保留，留给第二阶段归并），
// 再把能唯一对上一部主站影片的附属站播放列表归并到该片规范主键。
// dryRun=true 只统计不写库。仅供 cmd/tool 一次性脚本调用。
func MigrateSlavePlaylistKeys(dryRun bool, batchSize int, logf func(format string, v ...any)) (SlavePlaylistKeyMigrationResult, error) {
	var result SlavePlaylistKeyMigrationResult
	if batchSize <= 0 {
		batchSize = rekeyDefaultBatchSize
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}

	lastFilmID := uint(0)
	for {
		var films []model.FilmIndex
		if err := db.Mdb.Where("id > ?", lastFilmID).Order("id ASC").Limit(batchSize).Find(&films).Error; err != nil {
			return result, err
		}
		if len(films) == 0 {
			break
		}
		lastFilmID = films[len(films)-1].ID
		rewritten, err := rewriteMatchKeysForFilms(films, dryRun)
		if err != nil {
			return result, err
		}
		result.MatchKeys += rewritten
		if rewritten > 0 {
			logf("[Migrate] 匹配键 已处理影片游标 id=%d 重写=%d", lastFilmID, result.MatchKeys)
		}
	}

	lastPlaylistID := uint(0)
	for {
		var rows []model.SlaveMoviePlaylist
		if err := db.Mdb.Select(playlistScanColumns).
			Where("id > ?", lastPlaylistID).
			Order("id ASC").
			Limit(batchSize).
			Find(&rows).Error; err != nil {
			return result, err
		}
		if len(rows) == 0 {
			break
		}
		lastPlaylistID = rows[len(rows)-1].ID
		result.Scanned += int64(len(rows))

		keys := make([]string, 0, len(rows))
		for _, row := range rows {
			keys = append(keys, row.MovieKey)
		}
		targets, err := SlavePlaylistRekeyTargets(keys)
		if err != nil {
			return result, err
		}
		moving := make([]model.SlaveMoviePlaylist, 0, len(rows))
		for _, row := range rows {
			if _, ok := targets[strings.TrimSpace(row.MovieKey)]; ok {
				moving = append(moving, row)
			}
		}
		if !dryRun && len(moving) > 0 {
			if err := attachPlaylistContent(moving); err != nil {
				return result, err
			}
		}
		moved, err := rekeySlavePlaylistRows(moving, targets, dryRun)
		if err != nil {
			return result, err
		}
		result.Migrated += moved
		result.Skipped += int64(len(rows)) - moved
		prev := result.Scanned - int64(len(rows))
		if moved > 0 || result.Scanned/rekeyLogEveryN != prev/rekeyLogEveryN {
			logf("[Migrate] 播放列表 扫描=%d 归并=%d 跳过=%d（cursor id=%d）", result.Scanned, result.Migrated, result.Skipped, lastPlaylistID)
		}
	}
	return result, nil
}
