package writer

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode"

	"server/internal/model"
	"server/internal/repository/film/playlist"
	poster "server/internal/repository/film/poster"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func applyMasterBusinessUpdateStampsTx(tx *gorm.DB, infos []model.FilmIndex, detailsByKey map[string]model.MovieDetail, isManual bool) (map[string]struct{}, map[int64]model.MovieDetail, map[int64][]int, error) {
	unchangedKeys := make(map[string]struct{})
	oldDetailsByMid := make(map[int64]model.MovieDetail)
	// 按 mid 命中旧行：兼容 content_key 仍为 name_* 的库存（无需 bulk 迁移）。
	mids := filmIndexMIDs(infos)
	if len(mids) == 0 {
		return unchangedKeys, oldDetailsByMid, nil, nil
	}

	existingInfos := reloadFilmIndexesByMidsTx(tx, mids)
	if len(existingInfos) == 0 {
		return unchangedKeys, oldDetailsByMid, nil, nil
	}
	changedAt := time.Now().Unix()
	existingByMid := make(map[int64]model.FilmIndex, len(existingInfos))
	for _, existing := range existingInfos {
		if existing.Mid > 0 {
			existingByMid[existing.Mid] = existing
		}
	}

	existingDetailsByMid, err := loadMovieDetailsByMidsTx(tx, mids)
	if err != nil {
		return nil, nil, nil, err
	}
	oldDetailsByMid = existingDetailsByMid
	// 写前读全库集数（主站旧详情 + 其它源 playlist）；失败则中止，避免空 map 被当成「没有集」整页刷 stamp
	existingCountsMap, err := shared.LoadExistingEpisodeCountsByMIDs(tx, mids, "")
	if err != nil {
		return nil, nil, nil, err
	}
	sourceID := ""
	if len(infos) > 0 {
		sourceID = infos[0].SourceId
	}
	if err := poster.ApplyExternalPosterSourceToMasterWritesTx(tx, sourceID, infos, detailsByKey, existingByMid, isManual); err != nil {
		log.Printf("poster.ApplyExternalPosterSourceToMasterWritesTx Error: %v", err)
	}
	for index := range infos {
		existing, ok := existingByMid[infos[index].Mid]
		if !ok || existing.UpdateStamp <= 0 {
			continue
		}
		oldDetail, ok := existingDetailsByMid[existing.Mid]
		if !ok {
			continue
		}
		newDetail, ok := detailsByKey[infos[index].ContentKey]
		if !ok {
			continue
		}
		applyPersistedMasterCategory(&infos[index], existing)
		if sameStoredMasterDetail(oldDetail, newDetail) {
			// 业务无变更但 content_key 仍为 name_* 等旧值：强制写一次完成懒升（重采即升 vod_*）。
			if strings.TrimSpace(existing.ContentKey) != strings.TrimSpace(infos[index].ContentKey) {
				continue
			}
			unchangedKeys[infos[index].ContentKey] = struct{}{}
			continue
		}
		// 只有本源写出的最大集数严格大于全库已有最大集数才顶「最近更新」。
		// 附属站已先追到 120 时，主站后补 120 只写详情，不重进列表。
		existingCounts := existingCountsMap[existing.Mid]
		existingCounts = append(existingCounts, shared.ExtractEpisodeCountsFromDetail(oldDetail)...)
		if shared.IsEpisodeCountHigher(shared.ExtractEpisodeCountsFromDetail(newDetail), existingCounts) {
			infos[index].UpdateStamp = changedAt
		} else {
			infos[index].UpdateStamp = existing.UpdateStamp
		}
	}
	return unchangedKeys, oldDetailsByMid, existingCountsMap, nil
}

func loadMovieDetailsByMidsTx(tx *gorm.DB, mids []int64) (map[int64]model.MovieDetail, error) {
	result := make(map[int64]model.MovieDetail)
	if len(mids) == 0 {
		return result, nil
	}

	var detailInfos []model.MovieDetailInfo
	if err := tx.Where("mid IN ?", mids).Find(&detailInfos).Error; err != nil {
		return nil, err
	}
	for _, detailInfo := range detailInfos {
		var detail model.MovieDetail
		if err := json.Unmarshal([]byte(detailInfo.Content), &detail); err != nil {
			return nil, fmt.Errorf("parse movie detail mid=%d failed: %w", detailInfo.Mid, err)
		}
		result[detailInfo.Mid] = detail
	}
	return result, nil
}

// sameStoredMasterDetail 判断主站详情是否「业务上无实质变更」。
// 不可用整份 JSON 全量对比：源站每次采集常变 Hits/UpdateTime/AddTime/DbScore 等噪声字段，
// 会导致连续采集把同一批片反复当成「有更新」。
func sameStoredMasterDetail(oldDetail model.MovieDetail, newDetail model.MovieDetail) bool {
	return masterBusinessSignature(oldDetail) == masterBusinessSignature(newDetail)
}

func applyPersistedMasterCategory(newInfo *model.FilmIndex, existing model.FilmIndex) {
	newInfo.FilmIndexCategory = existing.FilmIndexCategory
}

// masterBusinessSignature 业务变更指纹：片名/副标/封面/播放源/备注/状态/演职/年代地区/分类。
// 有意不纳入：Hits/UpdateTime/AddTime/DbScore（噪声）；Content/PictureSlide/Language/EnName
// （简介/横图等变更不进采集更新列表，避免刷屏）。封面只比 path（忽略 CDN query）。
// 片名参与指纹前做归一化：源站片名标点/空白不稳定（如「烬九州：第四季」↔「烬九州第四季」）
// 不应视为内容更新；片名实质变化（改名）仍会触发。
func masterBusinessSignature(detail model.MovieDetail) string {
	payload := struct {
		Name            string                 `json:"name"`
		SubTitle        string                 `json:"subTitle"`
		Picture         string                 `json:"picture"`
		CustomPicture   string                 `json:"customPicture"`
		IsCustomPicture bool                   `json:"isCustomPicture"`
		PlayFrom        []string               `json:"playFrom"`
		PlayList        [][]model.MovieUrlInfo `json:"playList"`
		Remarks         string                 `json:"remarks"`
		State           string                 `json:"state"`
		Actor           string                 `json:"actor"`
		Director        string                 `json:"director"`
		Year            string                 `json:"year"`
		Area            string                 `json:"area"`
		ClassTag        string                 `json:"classTag"`
	}{
		Name:            normalizeNameForCompare(detail.Name),
		SubTitle:        strings.TrimSpace(detail.SubTitle),
		Picture:         shared.StripURLQuery(detail.Picture),
		CustomPicture:   shared.StripURLQuery(detail.CustomPicture),
		IsCustomPicture: detail.IsCustomPicture,
		PlayFrom:        normalizeStringSlice(detail.PlayFrom),
		PlayList:        normalizePlayList(detail.PlayList),
		Remarks:         strings.TrimSpace(detail.Remarks),
		State:           strings.TrimSpace(detail.State),
		Actor:           strings.TrimSpace(detail.Actor),
		Director:        strings.TrimSpace(detail.Director),
		Year:            strings.TrimSpace(detail.Year),
		Area:            strings.TrimSpace(detail.Area),
		ClassTag:        strings.TrimSpace(detail.ClassTag),
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

func normalizeStringSlice(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.TrimSpace(s)
	}
	return out
}

// normalizeNameForCompare 片名归一化用于变更对比：去空白与标点符号（全半角统一）。
// 仅用于指纹对比，不影响实际保存/展示的片名。
func normalizeNameForCompare(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func normalizePlayList(in [][]model.MovieUrlInfo) [][]model.MovieUrlInfo {
	if len(in) == 0 {
		return [][]model.MovieUrlInfo{}
	}
	out := make([][]model.MovieUrlInfo, len(in))
	for i, group := range in {
		if len(group) == 0 {
			out[i] = []model.MovieUrlInfo{}
			continue
		}
		g := make([]model.MovieUrlInfo, len(group))
		for j, u := range group {
			g[j] = model.MovieUrlInfo{
				Episode: strings.TrimSpace(u.Episode),
				// 链接去 query：防盗链签名等动态参数每次采集都不同，不应视为业务更新
				Link: shared.StripURLQuery(strings.TrimSpace(u.Link)),
			}
		}
		out[i] = g
	}
	return out
}

func movieDetailInfoUpsert() clause.OnConflict {
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: "mid"}},
		DoUpdates: clause.AssignmentColumns([]string{"source_id", "category_version", "rule_version", "content", "updated_at", "deleted_at"}),
	}
}

func buildMovieDetailInfos(sourceID string, details []model.MovieDetail, infoByKey map[string]model.FilmIndex, keyToMid map[string]int64) []model.MovieDetailInfo {
	detailInfos := make([]model.MovieDetailInfo, 0, len(details))
	for _, detail := range details {
		info, ok := infoByKey[shared.BuildContentKey(detail)]
		if !ok {
			continue
		}

		globalMid, ok := keyToMid[info.ContentKey]
		if !ok {
			globalMid = detail.Id
		}

		detail.Id = globalMid
		if strings.TrimSpace(info.Picture) != "" {
			detail.Picture = info.Picture
		}
		if strings.TrimSpace(info.PictureSlide) != "" {
			detail.PictureSlide = info.PictureSlide
		}
		detail.CustomPicture = info.CustomPicture
		detail.CustomPictureSlide = info.CustomPictureSlide
		detail.IsCustomPicture = info.IsCustomPicture
		data, _ := json.Marshal(detail)
		detailInfos = append(detailInfos, model.MovieDetailInfo{
			Mid:             globalMid,
			SourceId:        sourceID,
			CategoryVersion: info.CategoryVersion,
			RuleVersion:     info.RuleVersion,
			Content:         string(data),
		})
	}
	return detailInfos
}

func buildMovieMatchKeyMappings(details []model.MovieDetail, infoByKey map[string]model.FilmIndex, keyToMid map[string]int64) map[int64][]string {
	midToKeys := make(map[int64][]string, len(details))
	for _, detail := range details {
		info, ok := infoByKey[shared.BuildContentKey(detail)]
		if !ok {
			continue
		}
		globalMid, ok := keyToMid[info.ContentKey]
		if !ok || globalMid <= 0 {
			continue
		}
		pid := support.GetRootId(info.Pid)
		if pid <= 0 && info.Cid > 0 {
			pid = support.GetRootId(info.Cid)
		}
		if pid <= 0 {
			pid = shared.ResolveMovieDetailRootPid(detail)
		}
		midToKeys[globalMid] = shared.BuildMovieMatchKeysWithCategory(detail.DbId, detail.Name, pid)
	}
	return midToKeys
}

func saveMovieDetailInfosTx(tx *gorm.DB, detailInfos []model.MovieDetailInfo) error {
	if len(detailInfos) == 0 {
		return nil
	}
	return tx.Clauses(movieDetailInfoUpsert()).Create(&detailInfos).Error
}

// filterPlayStructureNotifyMIDs 从业务写入的影片中筛出应进「更新列表」的 mid：
// 新片，或本源最大集数严格大于全库已有最大集数（主站旧详情 + 附属站 playlist）。
// 其它源已经到 120 集后，本源再追到 120 只写库，不重进最近更新。
func filterPlayStructureNotifyMIDs(changed []model.FilmIndex, detailsByKey map[string]model.MovieDetail, oldByMid map[int64]model.MovieDetail, existingCountsMap map[int64][]int) []int64 {
	if len(changed) == 0 {
		return nil
	}

	out := make([]int64, 0, len(changed))
	seen := make(map[int64]struct{}, len(changed))
	for _, info := range changed {
		mid := info.Mid
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		newDetail, ok := detailsByKey[info.ContentKey]
		if !ok {
			continue
		}

		var existingCounts []int
		if existingCountsMap != nil {
			existingCounts = append(existingCounts, existingCountsMap[mid]...)
		}
		if oldDetail, hasOld := oldByMid[mid]; hasOld {
			existingCounts = append(existingCounts, shared.ExtractEpisodeCountsFromDetail(oldDetail)...)
		}
		if shared.IsEpisodeCountHigher(shared.ExtractEpisodeCountsFromDetail(newDetail), existingCounts) {
			seen[mid] = struct{}{}
			out = append(out, mid)
		}
	}
	return out
}

// masterPlaylistSignatures 把主站详情转成与附属站一致的线路签名列表（线路名 + 集数内容），
// 复用 playlist.PlaylistLastEpisodeChanged 做「最后一集变化」判定。
func masterPlaylistSignatures(d model.MovieDetail) []playlist.PlaylistSignature {
	n := len(d.PlayList)
	sigs := make([]playlist.PlaylistSignature, 0, n)
	for i := 0; i < n; i++ {
		name := ""
		if i < len(d.PlayFrom) {
			name = strings.TrimSpace(d.PlayFrom[i])
		}
		data, _ := json.Marshal(d.PlayList[i])
		sigs = append(sigs, playlist.PlaylistSignature{GroupIndex: i, GroupName: name, Content: string(data)})
	}
	return sigs
}

// masterLastEpisodeChanged 新详情相对旧详情，任一线路「最后一项分集标签」是否变化（含新增/回退）。
func masterLastEpisodeChanged(oldDetail, newDetail model.MovieDetail) bool {
	return playlist.PlaylistLastEpisodeChanged(masterPlaylistSignatures(oldDetail), masterPlaylistSignatures(newDetail))
}

// samePlayStructure 判断播放结构是否一致（线路 + 各线路最后一集标签）；忽略播放链接。
func samePlayStructure(oldDetail, newDetail model.MovieDetail) bool {
	return playStructureSignature(oldDetail) == playStructureSignature(newDetail)
}

func playStructureSignature(detail model.MovieDetail) string {
	type row struct {
		Index int    `json:"i"`
		Name  string `json:"name"`
		Last  string `json:"last"`
	}
	rows := make([]row, 0, len(detail.PlayList))
	for i, links := range detail.PlayList {
		name := ""
		if i < len(detail.PlayFrom) {
			name = strings.TrimSpace(detail.PlayFrom[i])
		}
		rows = append(rows, row{Index: i, Name: name, Last: playlist.LastEpisodeLabel(links)})
	}
	data, _ := json.Marshal(rows)
	return string(data)
}

func detailMapByContentKey(details []model.MovieDetail) map[string]model.MovieDetail {
	detailsByKey := make(map[string]model.MovieDetail, len(details))
	for _, detail := range details {
		key := shared.BuildContentKey(detail)
		if key == "" {
			// 无源站 id 且无法生成 name 指纹：不能作为主站身份，丢弃
			continue
		}
		detailsByKey[key] = detail
	}
	return detailsByKey
}

func filterChangedMasterWrites(infos []model.FilmIndex, details []model.MovieDetail, unchangedKeys map[string]struct{}) ([]model.FilmIndex, map[string]model.FilmIndex, []model.MovieDetail) {
	if len(unchangedKeys) == 0 {
		infoByKey := make(map[string]model.FilmIndex, len(infos))
		for _, info := range infos {
			infoByKey[info.ContentKey] = info
		}
		return infos, infoByKey, details
	}

	changedInfos := make([]model.FilmIndex, 0, len(infos))
	changedInfoByKey := make(map[string]model.FilmIndex, len(infos))
	for _, info := range infos {
		if _, ok := unchangedKeys[info.ContentKey]; ok {
			continue
		}
		changedInfos = append(changedInfos, info)
		changedInfoByKey[info.ContentKey] = info
	}

	changedDetails := make([]model.MovieDetail, 0, len(details))
	for _, detail := range details {
		if _, ok := unchangedKeys[shared.BuildContentKey(detail)]; ok {
			continue
		}
		changedDetails = append(changedDetails, detail)
	}
	return changedInfos, changedInfoByKey, changedDetails
}
