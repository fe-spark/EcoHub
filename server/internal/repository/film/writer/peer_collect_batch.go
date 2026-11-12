package writer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository/film/shared"
	"server/internal/repository/film/snapshot"
	"server/internal/repository/support"
	"server/internal/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const peerPagePlaylistBatch = 40

type peerPageItem struct {
	detail       model.MovieDetail
	mid          int64
	film         *model.FilmIndex
	isNew        bool
	linesChanged bool
	playChanged  bool
	metaChanged  bool
	create       bool
	stale        bool
	skipKind     int
}

// filterPeerPageDetails 丢掉无效条目。过滤无片名或无有效播放链接的数据，同一源站 vod_id 在一页里出现多次时保留最后一条。
func filterPeerPageDetails(list []model.MovieDetail) []model.MovieDetail {
	lastAt := make(map[int64]int, len(list))
	for i, detail := range list {
		if detail.Id > 0 && strings.TrimSpace(detail.Name) != "" && shared.HasPlayableLinks(detail) {
			lastAt[detail.Id] = i
		}
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

func writePeerPageTx(tx *gorm.DB, source *model.FilmSource, details []model.MovieDetail, sources []model.FilmSource) ([]int64, []int64, int, error) {
	if len(details) == 0 || source == nil {
		return nil, nil, 0, nil
	}
	items := make([]peerPageItem, len(details))
	sourceMids := make([]int64, len(details))
	for i, detail := range details {
		items[i] = peerPageItem{
			detail: detail,
		}
		sourceMids[i] = detail.Id
	}

	mapped, err := loadPageMappingsTx(tx, source.Id, sourceMids)
	if err != nil {
		return nil, nil, 0, err
	}
	if len(mapped) == 0 && !isPrimaryCollectSourceTx(tx, source) {
		return writePeerPageCreateTx(tx, source, details, sources)
	}
	if err = bindMappedFilmsTx(tx, items, mapped); err != nil {
		return nil, nil, 0, err
	}
	dropForeignFilms(items, source.Id)
	if err = detachSharedSourceVodsTx(tx, source.Id, items); err != nil {
		return nil, nil, 0, err
	}
	markUnmappedCreates(items)
	stage := time.Now()
	if err = createPageFilmsTx(tx, source, items, sources); err != nil {
		return nil, nil, 0, err
	}
	if err = insertPageMatchKeysTx(tx, items); err != nil {
		return nil, nil, 0, err
	}
	if err = upsertPageMappingsTx(tx, source.Id, items, mapped); err != nil {
		return nil, nil, 0, err
	}
	mapCost := time.Since(stage)
	stage = time.Now()
	if err = savePagePlaylistsTx(tx, source.Id, items); err != nil {
		return nil, nil, 0, err
	}
	playlistCost := time.Since(stage)
	// 片单认领先标 stale。更旧的页不能再改档案和匹配键。
	if err = syncPageFilmsTx(tx, source, items); err != nil {
		return nil, nil, 0, err
	}
	newMids := make(map[int64]struct{}, len(items))
	for i := range items {
		if items[i].isNew && items[i].mid > 0 {
			newMids[items[i].mid] = struct{}{}
		}
	}
	remarkMids, stampMids, affected, notify := make([]int64, 0, len(items)), make(map[int64]struct{}, len(items)), make([]int64, 0, len(items)), make([]int64, 0, len(items))
	for i := range items {
		if items[i].mid <= 0 || items[i].stale {
			continue
		}
		if items[i].isNew || items[i].linesChanged || items[i].metaChanged {
			affected = append(affected, items[i].mid)
		}
		if items[i].isNew || items[i].playChanged {
			notify = append(notify, items[i].mid)
		}
		// 新片的备注和播放摘要已经写在插入里，不再逐片更新。
		if !items[i].isNew && items[i].linesChanged {
			remarkMids = append(remarkMids, items[i].mid)
		}
		if _, isNew := newMids[items[i].mid]; !isNew && items[i].playChanged {
			stampMids[items[i].mid] = struct{}{}
		}
	}
	stage = time.Now()
	if err = refreshRemarksAndPlaySummaryWithStampTx(tx, remarkMids, stampMids, sources); err != nil {
		return nil, nil, 0, err
	}
	notePeerTiming(tx, "full", mapCost, playlistCost, time.Since(stage))
	return affected, notify, countCategorySkips(items), nil
}

// writePeerPageCreateTx 非主站全新页只建自己的档案。四次批量写入，不逐片更新。
func writePeerPageCreateTx(tx *gorm.DB, source *model.FilmSource, details []model.MovieDetail, sources []model.FilmSource) ([]int64, []int64, int, error) {
	details = lastPeerDetails(details)
	items := make([]peerPageItem, len(details))
	for i, detail := range details {
		items[i] = peerPageItem{detail: detail, create: true}
	}
	stage := time.Now()
	if err := createPageFilmsTx(tx, source, items, sources); err != nil {
		return nil, nil, 0, err
	}
	if err := insertPageMatchKeysTx(tx, items); err != nil {
		return nil, nil, 0, err
	}
	if err := insertLightMappingsTx(tx, source.Id, items); err != nil {
		return nil, nil, 0, err
	}
	if err := claimLightPageTx(tx, source.Id, items); err != nil {
		return nil, nil, 0, err
	}
	mapCost := time.Since(stage)
	stage = time.Now()
	if err := insertNewPagePlaylistsTx(tx, source.Id, items); err != nil {
		return nil, nil, 0, err
	}
	notePeerTiming(tx, "light", mapCost, time.Since(stage), 0)
	affected := make([]int64, 0, len(items))
	for i := range items {
		if items[i].mid > 0 && !items[i].stale {
			affected = append(affected, items[i].mid)
		}
	}
	return affected, affected, countCategorySkips(items), nil
}

// lastPeerDetails 同一 source_mid 只留页内最后一条。
func lastPeerDetails(details []model.MovieDetail) []model.MovieDetail {
	last := make(map[int64]int, len(details))
	for i := range details {
		if details[i].Id > 0 {
			last[details[i].Id] = i
		}
	}
	if len(last) == len(details) {
		return details
	}
	out := make([]model.MovieDetail, 0, len(last))
	for i := range details {
		id := details[i].Id
		if id > 0 && last[id] != i {
			continue
		}
		out = append(out, details[i])
	}
	return out
}

func claimLightPageTx(tx *gorm.DB, sourceID string, items []peerPageItem) error {
	claims := make([]vodTimeClaim, 0, len(items))
	idxs := make([]int, 0, len(items))
	for i := range items {
		if items[i].mid <= 0 {
			continue
		}
		idxs = append(idxs, i)
		claims = append(claims, vodTimeClaim{sourceMid: items[i].detail.Id, updateTime: items[i].detail.UpdateTime})
	}
	won, err := claimPageVodTimesTx(tx, sourceID, claims)
	if err != nil {
		return err
	}
	for i, idx := range idxs {
		if !won[i] {
			items[idx].stale = true
		}
	}
	return nil
}

func isPrimaryCollectSourceTx(tx *gorm.DB, source *model.FilmSource) bool {
	if source == nil || tx == nil {
		return true
	}
	var primary model.FilmSource
	err := tx.Where("state = ?", true).Order("sort ASC, created_at ASC, id ASC").Limit(1).Find(&primary).Error
	if err != nil || primary.Id == "" {
		return true
	}
	return primary.Id == source.Id
}

func insertLightMappingsTx(tx *gorm.DB, sourceID string, items []peerPageItem) error {
	mappings := make([]model.MovieSourceMapping, 0, len(items))
	for i := range items {
		if items[i].mid <= 0 {
			continue
		}
		unix, err := utils.ParseCollectUpdateTime(items[i].detail.UpdateTime)
		if err != nil {
			unix = 0
		}
		mappings = append(mappings, model.MovieSourceMapping{
			SourceId:      sourceID,
			SourceMid:     items[i].detail.Id,
			GlobalMid:     items[i].mid,
			SourceVodUnix: unix,
		})
	}
	if len(mappings) == 0 {
		return nil
	}
	sort.Slice(mappings, func(i, j int) bool { return mappings[i].SourceMid < mappings[j].SourceMid })
	// 冲突不覆盖已有 source_vod_unix，新旧由随后的 claim 决定。
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_mid"}},
		DoUpdates: clause.AssignmentColumns([]string{"global_mid", "updated_at"}),
	}).Create(&mappings).Error
}

func insertNewPagePlaylistsTx(tx *gorm.DB, sourceID string, items []peerPageItem) error {
	lines := make([]model.FilmSourcePlaylist, 0, len(items))
	for i := range items {
		if items[i].mid <= 0 || items[i].stale {
			continue
		}
		lines = append(lines, buildPlaylistsFromDetail(items[i].mid, sourceID, items[i].detail)...)
	}
	if len(lines) == 0 {
		return nil
	}
	sortPlaylists(lines)
	return tx.Clauses(playlistUpsertClause()).CreateInBatches(&lines, peerPagePlaylistBatch).Error
}

func countCategorySkips(items []peerPageItem) int {
	unmapped := 0
	for i := range items {
		if items[i].skipKind == skipKindUnmapped {
			unmapped++
		}
	}
	return unmapped
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
		if _, ok := seen[mid]; !ok {
			seen[mid] = struct{}{}
			mids = append(mids, mid)
		}
	}
	films, err := loadFilmsByMidsTx(tx, mids)
	if err != nil {
		return err
	}
	for i := range items {
		if mid, ok := mapped[items[i].detail.Id]; ok {
			if film, exists := films[mid]; exists {
				items[i].mid, items[i].film = mid, film
			}
		}
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

func dropForeignFilms(items []peerPageItem, sourceID string) {
	for i := range items {
		if items[i].film == nil {
			continue
		}
		if items[i].film.FirstSourceId != sourceID {
			items[i].film, items[i].mid = nil, 0
		}
	}
}

// stampNewFilmPlaySummary 把新片的备注和播放摘要写进插入行。只取播放线路，下载组更长时不能盖住正片集标题。
func stampNewFilmPlaySummary(fi *model.FilmIndex, source *model.FilmSource, detail model.MovieDetail, sources []model.FilmSource) {
	if fi == nil || source == nil {
		return
	}
	lines := buildPlaylistsFromDetail(0, source.Id, detail)
	playLines := make([]model.FilmSourcePlaylist, 0, len(lines))
	for _, line := range lines {
		if line.LineKind == "play" {
			playLines = append(playLines, line)
		}
	}
	fi.Remarks = pickPlaylistRemarks(playLines, sources)
	fi.PlayFromSummary = snapshot.BuildPlayFromSummaryFromPlaylists(playLines, sources)
}

func createPageFilmsTx(tx *gorm.DB, source *model.FilmSource, items []peerPageItem, sources []model.FilmSource) error {
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
			if kind := categorySkipKind(err); kind != skipKindNone {
				items[i].create = false
				items[i].skipKind = kind
				continue
			}
			return err
		}
		fi.Mid = 0
		fi.FirstSourceId = source.Id
		stampNewFilmPlaySummary(&fi, source, items[i].detail, sources)
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

func syncPageFilmsTx(tx *gorm.DB, source *model.FilmSource, items []peerPageItem) error {
	order := make([]int, 0, len(items))
	for i := range items {
		if items[i].stale || items[i].isNew || items[i].film == nil {
			continue
		}
		order = append(order, i)
	}
	sort.Slice(order, func(i, j int) bool {
		if items[order[i]].mid == items[order[j]].mid {
			return order[i] < order[j]
		}
		return items[order[i]].mid < items[order[j]].mid
	})
	keyMap := make(map[int64][]string, len(order))
	for _, idx := range order {
		changed, keys, err := syncOwnedFilmFieldsTx(tx, items[idx].film, source.Id, items[idx].detail)
		if err != nil {
			if kind := categorySkipKind(err); kind != skipKindNone {
				items[idx].skipKind = kind
				continue
			}
			return err
		}
		if changed {
			items[idx].metaChanged = true
		}
		if keys != nil && items[idx].mid > 0 {
			keyMap[items[idx].mid] = keys
		}
	}
	return shared.SaveMovieMatchKeysByMidTx(tx, keyMap)
}
