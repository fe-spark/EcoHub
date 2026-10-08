package writer

import (
	"fmt"
	"sort"
	"strings"

	"server/internal/model"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const peerPagePlaylistBatch = 40

type peerPageItem struct {
	detail       model.MovieDetail
	keys         []string
	mid          int64
	film         *model.FilmIndex
	isNew        bool
	linesChanged bool
	playChanged  bool
	create       bool
	owner        int
}

// filterPeerPageDetails 丢掉无效条目。同一源站 vod_id 在一页里出现多次时保留最后一条。
func filterPeerPageDetails(list []model.MovieDetail) []model.MovieDetail {
	lastAt := make(map[int64]int, len(list))
	for i, detail := range list {
		if detail.Id <= 0 || strings.TrimSpace(detail.Name) == "" {
			continue
		}
		lastAt[detail.Id] = i
	}
	if len(lastAt) == 0 {
		return nil
	}
	out := make([]model.MovieDetail, 0, len(lastAt))
	for i, detail := range list {
		if at, ok := lastAt[detail.Id]; ok && at == i {
			out = append(out, detail)
		}
	}
	return out
}

func writePeerPageTx(tx *gorm.DB, source *model.FilmSource, details []model.MovieDetail, sources []model.FilmSource) ([]int64, []int64, error) {
	if len(details) == 0 || source == nil {
		return nil, nil, nil
	}
	items := make([]peerPageItem, len(details))
	sourceMids := make([]int64, len(details))
	for i, detail := range details {
		items[i] = peerPageItem{detail: detail, keys: peerMatchKeys(source, detail), owner: -1}
		sourceMids[i] = detail.Id
	}

	mapped, err := loadPageMappingsTx(tx, source.Id, sourceMids)
	if err != nil {
		return nil, nil, err
	}
	if err = bindMappedFilmsTx(tx, items, mapped); err != nil {
		return nil, nil, err
	}
	if err = claimFilmsByMatchKeysTx(tx, items); err != nil {
		return nil, nil, err
	}
	linkSamePageFilms(items)
	if err = createPageFilmsTx(tx, source, items); err != nil {
		return nil, nil, err
	}
	applyPageOwners(items)
	if err = insertPageMatchKeysTx(tx, items); err != nil {
		return nil, nil, err
	}
	if err = upsertPageMappingsTx(tx, source.Id, items, mapped); err != nil {
		return nil, nil, err
	}
	if err = savePagePlaylistsTx(tx, source.Id, items); err != nil {
		return nil, nil, err
	}
	if err = backfillPageFilmsTx(tx, source, items); err != nil {
		return nil, nil, err
	}
	remarkMids := make([]int64, 0, len(items))
	affected := make([]int64, 0, len(items))
	notify := make([]int64, 0, len(items))
	for i := range items {
		if items[i].mid <= 0 {
			continue
		}
		affected = append(affected, items[i].mid)
		if items[i].isNew || items[i].playChanged {
			notify = append(notify, items[i].mid)
		}
		if items[i].isNew || items[i].linesChanged {
			remarkMids = append(remarkMids, items[i].mid)
		}
	}
	if err = refreshRemarksAndPlaySummaryMidsTx(tx, remarkMids, sources); err != nil {
		return nil, nil, err
	}
	return affected, notify, nil
}

func loadPageMappingsTx(tx *gorm.DB, sourceID string, sourceMids []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(sourceMids))
	var rows []model.MovieSourceMapping
	if err := tx.Select("source_mid", "global_mid").
		Where("source_id = ? AND source_mid IN ?", sourceID, sourceMids).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.SourceMid > 0 && row.GlobalMid > 0 {
			out[row.SourceMid] = row.GlobalMid
		}
	}
	return out, nil
}

func bindMappedFilmsTx(tx *gorm.DB, items []peerPageItem, mapped map[int64]int64) error {
	mids := make([]int64, 0, len(mapped))
	seen := make(map[int64]struct{}, len(mapped))
	for _, mid := range mapped {
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		mids = append(mids, mid)
	}
	films, err := loadFilmsByMidsTx(tx, mids)
	if err != nil {
		return err
	}
	for i := range items {
		mid, ok := mapped[items[i].detail.Id]
		if !ok {
			continue
		}
		film, ok := films[mid]
		if !ok {
			continue
		}
		items[i].mid = mid
		items[i].film = film
	}
	return nil
}

func loadFilmsByMidsTx(tx *gorm.DB, mids []int64) (map[int64]*model.FilmIndex, error) {
	out := make(map[int64]*model.FilmIndex, len(mids))
	if len(mids) == 0 {
		return out, nil
	}
	var rows []model.FilmIndex
	if err := tx.Where("mid IN ?", mids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		out[rows[i].Mid] = &rows[i]
	}
	return out, nil
}

type matchKeyMid struct {
	MatchKey string `gorm:"column:match_key"`
	Mid      int64  `gorm:"column:mid"`
}

func claimFilmsByMatchKeysTx(tx *gorm.DB, items []peerPageItem) error {
	keys := make([]string, 0, len(items)*3)
	seen := make(map[string]struct{})
	for i := range items {
		if items[i].film != nil {
			continue
		}
		for _, key := range items[i].keys {
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
	if len(keys) == 0 {
		return nil
	}
	var rows []matchKeyMid
	if err := tx.Model(&model.MovieMatchKey{}).
		Select("match_key, MIN(mid) AS mid").
		Where("match_key IN ?", keys).
		Group("match_key").
		Scan(&rows).Error; err != nil {
		return err
	}
	keyMid := make(map[string]int64, len(rows))
	claimMids := make([]int64, 0, len(rows))
	claimed := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		if row.Mid <= 0 || row.MatchKey == "" {
			continue
		}
		keyMid[row.MatchKey] = row.Mid
		if _, ok := claimed[row.Mid]; ok {
			continue
		}
		claimed[row.Mid] = struct{}{}
		claimMids = append(claimMids, row.Mid)
	}
	films, err := loadFilmsByMidsTx(tx, claimMids)
	if err != nil {
		return err
	}
	for i := range items {
		if items[i].film != nil || len(items[i].keys) == 0 {
			continue
		}
		var mid int64
		found := false
		for _, key := range items[i].keys {
			candidate, ok := keyMid[key]
			if !ok {
				continue
			}
			if !found || candidate < mid {
				mid = candidate
				found = true
			}
		}
		if !found {
			continue
		}
		film, ok := films[mid]
		if !ok {
			continue
		}
		items[i].mid = film.Mid
		items[i].film = film
	}
	return nil
}

// linkSamePageFilms 把本页里匹配键相交、库中还没有档案的条目合成同一部。
func linkSamePageFilms(items []peerPageItem) {
	ownerByKey := make(map[string]int, len(items)*3)
	for i := range items {
		if items[i].film != nil {
			rememberPageKeys(ownerByKey, items[i].keys, i)
			continue
		}
		best := -1
		var bestMid int64
		for _, key := range items[i].keys {
			owner, ok := ownerByKey[key]
			if !ok {
				continue
			}
			if best < 0 || betterPageOwner(items[owner], bestMid) {
				best = owner
				bestMid = items[owner].mid
			}
		}
		if best >= 0 {
			items[i].owner = best
			rememberPageKeys(ownerByKey, items[i].keys, best)
			continue
		}
		items[i].create = true
		rememberPageKeys(ownerByKey, items[i].keys, i)
	}
}

func betterPageOwner(item peerPageItem, bestMid int64) bool {
	if item.mid <= 0 {
		return false
	}
	return bestMid <= 0 || item.mid < bestMid
}

func rememberPageKeys(ownerByKey map[string]int, keys []string, idx int) {
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, ok := ownerByKey[key]; ok {
			continue
		}
		ownerByKey[key] = idx
	}
}

func createPageFilmsTx(tx *gorm.DB, source *model.FilmSource, items []peerPageItem) error {
	categoryVersion := support.GetCategoryVersion()
	ruleVersion := support.GetRuleVersion()
	newFilms := make([]model.FilmIndex, 0)
	newIdx := make([]int, 0)
	for i := range items {
		if !items[i].create {
			continue
		}
		fi, err := ConvertFilmIndex(source.Id, items[i].detail, categoryVersion, ruleVersion)
		if err != nil {
			return err
		}
		fi.Mid = 0
		fi.FirstSourceId = source.Id
		newFilms = append(newFilms, fi)
		newIdx = append(newIdx, i)
	}
	if len(newFilms) == 0 {
		return nil
	}
	if err := tx.Create(&newFilms).Error; err != nil {
		return err
	}
	for j, idx := range newIdx {
		if newFilms[j].Mid <= 0 {
			return fmt.Errorf("film_index mid not assigned vod_id=%d", items[idx].detail.Id)
		}
		items[idx].film = &newFilms[j]
		items[idx].mid = newFilms[j].Mid
		items[idx].isNew = true
	}
	return nil
}

func applyPageOwners(items []peerPageItem) {
	for i := range items {
		if items[i].film != nil || items[i].owner < 0 {
			continue
		}
		src := items[items[i].owner]
		items[i].film = src.film
		items[i].mid = src.mid
	}
}

func insertPageMatchKeysTx(tx *gorm.DB, items []peerPageItem) error {
	matchKeys := make([]model.MovieMatchKey, 0)
	seen := make(map[string]struct{})
	for i := range items {
		if !items[i].isNew || items[i].mid <= 0 {
			continue
		}
		pid := items[i].film.Pid
		if pid <= 0 {
			pid = items[i].detail.RawPid
		}
		keys := shared.BuildMovieMatchKeysWithCategory(items[i].detail.DbId, items[i].detail.Name, pid)
		sort.Strings(keys)
		for _, key := range keys {
			if key == "" {
				continue
			}
			id := fmt.Sprintf("%d\x00%s", items[i].mid, key)
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			matchKeys = append(matchKeys, model.MovieMatchKey{Mid: items[i].mid, MatchKey: key})
		}
	}
	if len(matchKeys) == 0 {
		return nil
	}
	sort.Slice(matchKeys, func(i, j int) bool {
		if matchKeys[i].Mid == matchKeys[j].Mid {
			return matchKeys[i].MatchKey < matchKeys[j].MatchKey
		}
		return matchKeys[i].Mid < matchKeys[j].Mid
	})
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&matchKeys).Error
}

func upsertPageMappingsTx(tx *gorm.DB, sourceID string, items []peerPageItem, mapped map[int64]int64) error {
	mappings := make([]model.MovieSourceMapping, 0, len(items))
	for i := range items {
		if items[i].mid <= 0 {
			continue
		}
		if existing, ok := mapped[items[i].detail.Id]; ok && existing == items[i].mid {
			continue
		}
		mappings = append(mappings, model.MovieSourceMapping{
			SourceId:  sourceID,
			SourceMid: items[i].detail.Id,
			GlobalMid: items[i].mid,
		})
	}
	if len(mappings) == 0 {
		return nil
	}
	sort.Slice(mappings, func(i, j int) bool { return mappings[i].SourceMid < mappings[j].SourceMid })
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_mid"}},
		DoUpdates: clause.AssignmentColumns([]string{"global_mid", "updated_at"}),
	}).Create(&mappings).Error
}

func savePagePlaylistsTx(tx *gorm.DB, sourceID string, items []peerPageItem) error {
	mids := make([]int64, 0, len(items))
	seen := make(map[int64]struct{}, len(items))
	for i := range items {
		if items[i].mid <= 0 {
			continue
		}
		if _, ok := seen[items[i].mid]; ok {
			continue
		}
		seen[items[i].mid] = struct{}{}
		mids = append(mids, items[i].mid)
	}
	if len(mids) == 0 {
		return nil
	}
	sort.Slice(mids, func(i, j int) bool { return mids[i] < mids[j] })

	var oldLines []model.FilmSourcePlaylist
	if err := playlistMetaQuery(tx).
		Where("source_id = ? AND mid IN ?", sourceID, mids).
		Find(&oldLines).Error; err != nil {
		return err
	}
	oldByMid := make(map[int64][]model.FilmSourcePlaylist, len(mids))
	for _, line := range oldLines {
		oldByMid[line.Mid] = append(oldByMid[line.Mid], line)
	}

	// 同一 mid 在本页有多条源站记录时，后一条覆盖前一条，和原先逐片写入的结果一致。
	lastIdx := make(map[int64]int, len(mids))
	for i := range items {
		if items[i].mid > 0 {
			lastIdx[items[i].mid] = i
		}
	}
	writers := make([]int, 0, len(lastIdx))
	for _, idx := range lastIdx {
		writers = append(writers, idx)
	}
	sort.Slice(writers, func(i, j int) bool {
		if items[writers[i]].mid == items[writers[j]].mid {
			return writers[i] < writers[j]
		}
		return items[writers[i]].mid < items[writers[j]].mid
	})

	vanished := make([]model.FilmSourcePlaylist, 0)
	upserts := make([]model.FilmSourcePlaylist, 0)
	for _, idx := range writers {
		newLines := buildPlaylistsFromDetail(items[idx].mid, sourceID, items[idx].detail)
		old := oldByMid[items[idx].mid]
		if isPlaylistSignatureIdentical(old, newLines) {
			continue
		}
		items[idx].linesChanged = true
		items[idx].playChanged = playHashesChanged(old, newLines)
		vanished = append(vanished, vanishedPlaylists(old, newLines)...)
		upserts = append(upserts, newLines...)
	}
	sortPlaylists(vanished)
	for _, line := range vanished {
		if err := deletePlaylistTx(tx, line); err != nil {
			return err
		}
	}
	if len(upserts) == 0 {
		return nil
	}
	sortPlaylists(upserts)
	return tx.Clauses(playlistUpsertClause()).CreateInBatches(&upserts, peerPagePlaylistBatch).Error
}

func backfillPageFilmsTx(tx *gorm.DB, source *model.FilmSource, items []peerPageItem) error {
	order := make([]int, 0, len(items))
	for i := range items {
		if items[i].linesChanged && !items[i].isNew && items[i].film != nil {
			order = append(order, i)
		}
	}
	sort.Slice(order, func(i, j int) bool {
		if items[order[i]].mid == items[order[j]].mid {
			return order[i] < order[j]
		}
		return items[order[i]].mid < items[order[j]].mid
	})
	for _, idx := range order {
		if _, err := backfillFilmMetadataTx(tx, items[idx].film, source, items[idx].detail); err != nil {
			return err
		}
	}
	return nil
}
