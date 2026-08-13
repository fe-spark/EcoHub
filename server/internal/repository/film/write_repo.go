package film

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/support"
	"server/internal/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const searchTagsVisibleCacheInvalidateInterval = 2 * time.Second

const (
	searchTagsRebuildFilmBatchSize = 200
	upsertBatchSize                = 200
)

var searchTagsVisibleCacheState struct {
	mu     sync.Mutex
	lastAt time.Time
}

var movieSourceMappingWriteMu sync.Mutex

// filmIndexMidUpsert 按 mid 冲突更新（主站 mid = 源站 vod_id）。
// 旧库存 content_key=name_* 时仍能命中同一行，并在更新时懒升 content_key→vod_*。
func filmIndexMidUpsert() clause.OnConflict {
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: "mid"}},
		DoUpdates: clause.AssignmentColumns(filmIndexUpsertUpdateColumns),
	}
}

func movieSourceMappingUpsert() clause.OnConflict {
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_mid"}},
		DoUpdates: clause.AssignmentColumns([]string{"global_mid", "updated_at", "deleted_at"}),
	}
}

func filterValidFilmIndexes(list []model.FilmIndex) []model.FilmIndex {
	validList := make([]model.FilmIndex, 0, len(list))
	for _, item := range list {
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		validList = append(validList, item)
	}
	return validList
}

func upsertFilmIndexes(list []model.FilmIndex) error {
	return upsertFilmIndexesTx(db.Mdb, list)
}

func upsertFilmIndexesTx(tx *gorm.DB, list []model.FilmIndex) error {
	if len(list) == 0 {
		return nil
	}
	// 软删占键可安全释放；活跃行占键则报错，避免生产误改他片身份键。
	if err := releaseConflictingContentKeysTx(tx, list); err != nil {
		return err
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Mid < list[j].Mid
	})
	return tx.Clauses(filmIndexMidUpsert()).CreateInBatches(&list, upsertBatchSize).Error
}

// releaseConflictingContentKeysTx 处理 content_key=目标 且 mid≠本批归属 的占用行：
//   - 软删行：改名为 del_{id}，释放唯一键（常见、安全）；
//   - 活跃行：直接报错，不自动抢键（脏数据需人工处理，避免写坏库存）。
func releaseConflictingContentKeysTx(tx *gorm.DB, list []model.FilmIndex) error {
	for _, item := range list {
		key := strings.TrimSpace(item.ContentKey)
		if key == "" || item.Mid <= 0 {
			continue
		}
		var occupants []model.FilmIndex
		if err := tx.Unscoped().Model(&model.FilmIndex{}).
			Select("id", "mid", "deleted_at").
			Where("content_key = ? AND mid <> ?", key, item.Mid).
			Find(&occupants).Error; err != nil {
			return err
		}
		for _, o := range occupants {
			if !o.DeletedAt.Valid {
				return fmt.Errorf("content_key %q 已被活跃 mid=%d 占用，无法写入 mid=%d（请检查脏数据或重置冲突片）",
					key, o.Mid, item.Mid)
			}
			if err := tx.Unscoped().Model(&model.FilmIndex{}).
				Where("id = ?", o.ID).
				Update("content_key", fmt.Sprintf("del_%d", o.ID)).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// keyToMidFromIndexes 主站 mid 即全局 mid，映射可直接由本批索引构造（不依赖 DB content_key 是否已懒升）。
func keyToMidFromIndexes(list []model.FilmIndex) map[string]int64 {
	out := make(map[string]int64, len(list))
	for _, item := range list {
		key := strings.TrimSpace(item.ContentKey)
		if key == "" || item.Mid <= 0 {
			continue
		}
		out[key] = item.Mid
	}
	return out
}

func loadFilmIndexMidMapByContentKeys(contentKeys []string) map[string]int64 {
	return loadFilmIndexMidMapByContentKeysTx(db.Mdb, contentKeys)
}

func loadFilmIndexMidMapByContentKeysTx(tx *gorm.DB, contentKeys []string) map[string]int64 {
	if len(contentKeys) == 0 {
		return nil
	}

	var latestInfos []model.FilmIndex
	if err := tx.Where("content_key IN ?", contentKeys).Find(&latestInfos).Error; err != nil {
		return nil
	}

	keyToMid := make(map[string]int64, len(latestInfos))
	for _, info := range latestInfos {
		keyToMid[info.ContentKey] = info.Mid
	}
	return keyToMid
}

func buildContentKeys(list []model.FilmIndex) []string {
	contentKeys := make([]string, 0, len(list))
	for _, item := range list {
		contentKeys = append(contentKeys, item.ContentKey)
	}
	return contentKeys
}

func buildMovieSourceMappings(list []model.FilmIndex, keyToMid map[string]int64) []model.MovieSourceMapping {
	mappings := make([]model.MovieSourceMapping, 0, len(list))
	for _, item := range list {
		globalMid, ok := keyToMid[item.ContentKey]
		if !ok {
			continue
		}
		mappings = append(mappings, model.MovieSourceMapping{
			SourceId:  item.SourceId,
			SourceMid: item.Mid,
			GlobalMid: globalMid,
		})
	}
	return mappings
}

func saveFilmIndexesAndMappings(list []model.FilmIndex) (map[string]int64, error) {
	return saveFilmIndexesAndMappingsTx(db.Mdb, list)
}

func saveFilmIndexesAndMappingsTx(tx *gorm.DB, list []model.FilmIndex) (map[string]int64, error) {
	if len(list) == 0 {
		return nil, nil
	}

	if err := upsertFilmIndexesTx(tx, list); err != nil {
		return nil, err
	}

	keyToMid := keyToMidFromIndexes(list)
	if len(keyToMid) == 0 {
		return nil, fmt.Errorf("load film index mids failed")
	}
	if err := saveMovieSourceMappingsTxE(tx, buildMovieSourceMappings(list, keyToMid)); err != nil {
		return nil, err
	}
	return keyToMid, nil
}

func saveMovieSourceMappingsTxE(tx *gorm.DB, mappings []model.MovieSourceMapping) error {
	if len(mappings) == 0 {
		return nil
	}
	movieSourceMappingWriteMu.Lock()
	defer movieSourceMappingWriteMu.Unlock()
	sort.Slice(mappings, func(i, j int) bool {
		if mappings[i].SourceId == mappings[j].SourceId {
			return mappings[i].SourceMid < mappings[j].SourceMid
		}
		return mappings[i].SourceId < mappings[j].SourceId
	})
	return tx.Clauses(movieSourceMappingUpsert()).CreateInBatches(&mappings, upsertBatchSize).Error
}

func buildFilmIndexesFromDetails(sourceID string, details []model.MovieDetail) ([]model.FilmIndex, map[string]model.FilmIndex, error) {
	infoList := make([]model.FilmIndex, 0, len(details))
	infoByKey := make(map[string]model.FilmIndex, len(details))
	categoryVersion := support.GetCategoryVersion()
	ruleVersion := support.GetRuleVersion()
	for _, detail := range details {
		if strings.TrimSpace(detail.Name) == "" {
			continue
		}
		info, err := ConvertFilmIndex(sourceID, detail, categoryVersion, ruleVersion)
		if err != nil {
			return nil, nil, err
		}
		infoList = append(infoList, info)
		infoByKey[info.ContentKey] = info
	}
	return infoList, infoByKey, nil
}

func applyMasterBusinessUpdateStampsTx(tx *gorm.DB, infos []model.FilmIndex, detailsByKey map[string]model.MovieDetail) (map[string]struct{}, map[int64]model.MovieDetail, map[int64][]int, error) {
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
	// 写前读库存集数；失败则中止，避免空 map 被当成「库里没有集」而整页刷 stamp
	existingCountsMap, err := loadExistingEpisodeCountsByMIDs(tx, mids, "")
	if err != nil {
		return nil, nil, nil, err
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
		// 业务仍写库，但 update_stamp 只在概要会记为「更新」时刷新：
		// 新片或分集数量严格增加。备注/演员/封面等不顶「最近更新」。
		existingCounts := existingCountsMap[existing.Mid]
		existingCounts = append(existingCounts, extractEpisodeCountsFromDetail(oldDetail)...)
		if isEpisodeCountHigher(extractEpisodeCountsFromDetail(newDetail), existingCounts) {
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
		Name     string                 `json:"name"`
		SubTitle string                 `json:"subTitle"`
		Picture  string                 `json:"picture"`
		PlayFrom []string               `json:"playFrom"`
		PlayList [][]model.MovieUrlInfo `json:"playList"`
		Remarks  string                 `json:"remarks"`
		State    string                 `json:"state"`
		Actor    string                 `json:"actor"`
		Director string                 `json:"director"`
		Year     string                 `json:"year"`
		Area     string                 `json:"area"`
		ClassTag string                 `json:"classTag"`
	}{
		Name:     normalizeNameForCompare(detail.Name),
		SubTitle: strings.TrimSpace(detail.SubTitle),
		Picture:  stripURLQuery(detail.Picture),
		PlayFrom: normalizeStringSlice(detail.PlayFrom),
		PlayList: normalizePlayList(detail.PlayList),
		Remarks:  strings.TrimSpace(detail.Remarks),
		State:    strings.TrimSpace(detail.State),
		Actor:    strings.TrimSpace(detail.Actor),
		Director: strings.TrimSpace(detail.Director),
		Year:     strings.TrimSpace(detail.Year),
		Area:     strings.TrimSpace(detail.Area),
		ClassTag: strings.TrimSpace(detail.ClassTag),
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

func stripURLQuery(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i]
	}
	return raw
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
				Link: stripURLQuery(strings.TrimSpace(u.Link)),
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
		info, ok := infoByKey[BuildContentKey(detail)]
		if !ok {
			continue
		}

		globalMid, ok := keyToMid[info.ContentKey]
		if !ok {
			globalMid = detail.Id
		}

		detail.Id = globalMid
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
		info, ok := infoByKey[BuildContentKey(detail)]
		if !ok {
			continue
		}
		globalMid, ok := keyToMid[info.ContentKey]
		if !ok || globalMid <= 0 {
			continue
		}
		midToKeys[globalMid] = BuildMovieMatchKeys(detail.DbId, detail.Name)
	}
	return midToKeys
}

func saveMovieDetailInfos(detailInfos []model.MovieDetailInfo) error {
	return saveMovieDetailInfosTx(db.Mdb, detailInfos)
}

func saveMovieDetailInfosTx(tx *gorm.DB, detailInfos []model.MovieDetailInfo) error {
	if len(detailInfos) == 0 {
		return nil
	}
	return tx.Clauses(movieDetailInfoUpsert()).Create(&detailInfos).Error
}

func clearDetailCaches(pid int64) {
	ClearSearchTagsCache(pid)
	db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
}

func clearFilmIndexCachesByPids(list []model.FilmIndex) {
	pidSet := make(map[int64]struct{})
	for _, item := range list {
		pidSet[item.Pid] = struct{}{}
	}
	clearFilmIndexCachesByPidSet(pidSet)
}

func clearFilmIndexCachesByPidSet(pidSet map[int64]struct{}) {
	for pid := range pidSet {
		if pid <= 0 {
			continue
		}
		ClearSearchTagsCache(pid)
	}
	db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
	support.ClearIndexPageCache()
	ClearProvideListCache()
}

func BatchSaveOrUpdate(list []model.FilmIndex) map[string]int64 {
	list = filterValidFilmIndexes(list)
	if len(list) == 0 {
		return nil
	}

	keyToMid, err := saveFilmIndexesAndMappings(list)
	if err != nil {
		log.Printf("BatchSaveOrUpdate upsert 失败: %v\n", err)
		return nil
	}

	clearFilmIndexCachesByPids(list)
	BatchHandleSearchTag(list...)
	return keyToMid
}

func SaveFilmIndex(s model.FilmIndex) error {
	if _, err := saveFilmIndexesAndMappings([]model.FilmIndex{s}); err != nil {
		return err
	}
	clearFilmIndexCachesByPids([]model.FilmIndex{s})
	BatchHandleSearchTag(s)
	return nil
}

func SaveDetails(id string, list []model.MovieDetail) error {
	_, err := saveDetails(id, list, true)
	return err
}

// CollectWriteResult 采集写入结果。
// AffectedMIDs：有业务写入的 mid（缓存/快照收尾）；NotifyMIDs：应进更新列表的 mid（剧集结构变更或新片）。
type CollectWriteResult struct {
	AffectedMIDs []int64
	NotifyMIDs   []int64
}

// SaveDetailsForCollect 采集写主站详情。返回的 NotifyMIDs 仅含「剧集/播放源结构」真变更或新片，
// 片名标点/备注/封面/链接签名等噪声写库时不进入更新列表。
func SaveDetailsForCollect(id string, list []model.MovieDetail) (CollectWriteResult, error) {
	return saveDetails(id, list, false)
}

func saveDetails(id string, list []model.MovieDetail, refreshSearchTags bool) (CollectWriteResult, error) {
	var out CollectWriteResult
	infoList, _, err := buildFilmIndexesFromDetails(id, list)
	if err != nil {
		return out, err
	}
	infoList = filterValidFilmIndexes(infoList)
	if len(infoList) == 0 {
		return out, nil
	}

	detailsByKey := detailMapByContentKey(list)
	var changedInfos []model.FilmIndex
	var infoByKey map[string]model.FilmIndex
	var changedDetails []model.MovieDetail
	var oldDetailsByMid map[int64]model.MovieDetail
	var preWriteCounts map[int64][]int
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		unchangedKeys, oldDetails, counts, err := applyMasterBusinessUpdateStampsTx(tx, infoList, detailsByKey)
		if err != nil {
			return err
		}
		oldDetailsByMid = oldDetails
		preWriteCounts = counts
		changedInfos, infoByKey, changedDetails = filterChangedMasterWrites(infoList, list, unchangedKeys)

		if len(changedInfos) > 0 {
			if err := upsertFilmIndexesTx(tx, changedInfos); err != nil {
				return err
			}
		}

		// 主站 mid=源站 id：直接由本批构造，避免未懒升行 content_key 仍为 name_* 时反查 miss。
		keyToMid := keyToMidFromIndexes(infoList)
		if len(keyToMid) == 0 {
			return fmt.Errorf("load film index mids failed")
		}
		if err := saveMovieSourceMappingsTxE(tx, buildMovieSourceMappings(infoList, keyToMid)); err != nil {
			return err
		}

		if len(changedInfos) == 0 {
			return nil
		}

		if err := saveMovieDetailInfosTx(tx, buildMovieDetailInfos(id, changedDetails, infoByKey, keyToMid)); err != nil {
			return err
		}
		if err := saveMovieMatchKeysByMidTx(tx, buildMovieMatchKeyMappings(changedDetails, infoByKey, keyToMid)); err != nil {
			return err
		}

		for i := range changedInfos {
			if mid, ok := keyToMid[changedInfos[i].ContentKey]; ok && mid > 0 {
				changedInfos[i].Mid = mid
			}
		}
		out.AffectedMIDs = collectFilmIndexMIDs(changedInfos)
		return RefreshPlayFromSummaryByIndexesTx(tx, changedInfos)
	}); err != nil {
		return CollectWriteResult{}, err
	}
	if len(changedInfos) == 0 {
		return out, nil
	}

	// 更新列表：仅剧集结构变更或新片；用写前集数，避免刚写入的详情被当成 existing
	out.NotifyMIDs = filterPlayStructureNotifyMIDs(changedInfos, detailsByKey, oldDetailsByMid, preWriteCounts)

	// 仅在有实质变更时更新 last_collect_time / 失效缓存。
	if refreshSearchTags {
		if err := repository.TouchCollectSourceStatsTx(db.Mdb, id, time.Now()); err != nil {
			log.Printf("TouchCollectSourceStats Error: %v", err)
		}
		clearFilmIndexCachesByPids(changedInfos)
		BatchHandleSearchTag(changedInfos...)
	} else {
		repository.NoteCollectSourceStats(id)
		NoteCollectCacheInvalidationByIndexes(changedInfos)
	}
	return out, nil
}

// filterPlayStructureNotifyMIDs 从业务写入的影片中筛出应进「更新列表」的 mid：
// 新片，或任一线路分集数量严格大于全库已有最大集数。
// 数量未增加（如已有15集，后更新的源也是15集）不计入更新列表。
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
			existingCounts = append(existingCounts, extractEpisodeCountsFromDetail(oldDetail)...)
		}

		newCounts := extractEpisodeCountsFromDetail(newDetail)
		if isEpisodeCountHigher(newCounts, existingCounts) {
			seen[mid] = struct{}{}
			out = append(out, mid)
		}
	}
	return out
}

// masterPlaylistSignatures 把主站详情转成与附属站一致的线路签名列表（线路名 + 集数内容），
// 复用 playlistLastEpisodeChanged 做「最后一集变化」判定。
func masterPlaylistSignatures(d model.MovieDetail) []playlistSignature {
	n := len(d.PlayList)
	sigs := make([]playlistSignature, 0, n)
	for i := 0; i < n; i++ {
		name := ""
		if i < len(d.PlayFrom) {
			name = strings.TrimSpace(d.PlayFrom[i])
		}
		data, _ := json.Marshal(d.PlayList[i])
		sigs = append(sigs, playlistSignature{GroupIndex: i, GroupName: name, Content: string(data)})
	}
	return sigs
}

// masterLastEpisodeChanged 新详情相对旧详情，任一线路「最后一项分集标签」是否变化（含新增/回退）。
func masterLastEpisodeChanged(oldDetail, newDetail model.MovieDetail) bool {
	return playlistLastEpisodeChanged(masterPlaylistSignatures(oldDetail), masterPlaylistSignatures(newDetail))
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
		rows = append(rows, row{Index: i, Name: name, Last: lastEpisodeLabel(links)})
	}
	data, _ := json.Marshal(rows)
	return string(data)
}

func normalizeEpisodeLabels(in [][]model.MovieUrlInfo) [][]string {
	if len(in) == 0 {
		return [][]string{}
	}
	out := make([][]string, len(in))
	for i, group := range in {
		if len(group) == 0 {
			out[i] = []string{}
			continue
		}
		labels := make([]string, len(group))
		for j, u := range group {
			labels[j] = strings.TrimSpace(u.Episode)
		}
		out[i] = labels
	}
	return out
}

func collectFilmIndexMIDs(infos []model.FilmIndex) []int64 {
	midSet := make(map[int64]struct{}, len(infos))
	mids := make([]int64, 0, len(infos))
	for _, info := range infos {
		if info.Mid <= 0 {
			continue
		}
		if _, ok := midSet[info.Mid]; ok {
			continue
		}
		midSet[info.Mid] = struct{}{}
		mids = append(mids, info.Mid)
	}
	return mids
}

func detailMapByContentKey(details []model.MovieDetail) map[string]model.MovieDetail {
	detailsByKey := make(map[string]model.MovieDetail, len(details))
	for _, detail := range details {
		key := BuildContentKey(detail)
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
		if _, ok := unchangedKeys[BuildContentKey(detail)]; ok {
			continue
		}
		changedDetails = append(changedDetails, detail)
	}
	return changedInfos, changedInfoByKey, changedDetails
}

func filmIndexContentKeys(infos []model.FilmIndex) []string {
	keys := make([]string, 0, len(infos))
	seen := make(map[string]struct{}, len(infos))
	for _, info := range infos {
		key := strings.TrimSpace(info.ContentKey)
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

func SaveDetail(id string, detail model.MovieDetail) error {
	snapshot, err := ConvertFilmIndex(id, detail, support.GetCategoryVersion(), support.GetRuleVersion())
	if err != nil {
		return err
	}
	if strings.TrimSpace(snapshot.Name) == "" {
		return nil
	}

	changed := false
	var savedMid int64
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		infoList := []model.FilmIndex{snapshot}
		unchangedKeys, _, _, err := applyMasterBusinessUpdateStampsTx(tx, infoList, detailMapByContentKey([]model.MovieDetail{detail}))
		if err != nil {
			return err
		}
		writeInfos, infoByKey, writeDetails := filterChangedMasterWrites(infoList, []model.MovieDetail{detail}, unchangedKeys)
		if len(writeInfos) > 0 {
			if err := upsertFilmIndexesTx(tx, writeInfos); err != nil {
				return err
			}
			snapshot = writeInfos[0]
			changed = true
		}

		keyToMid := keyToMidFromIndexes(infoList)
		if len(keyToMid) == 0 {
			return fmt.Errorf("load film index mids failed")
		}
		if err := saveMovieSourceMappingsTxE(tx, buildMovieSourceMappings(infoList, keyToMid)); err != nil {
			return err
		}

		if !changed {
			return nil
		}

		if err := saveMovieDetailInfosTx(tx, buildMovieDetailInfos(id, writeDetails, infoByKey, keyToMid)); err != nil {
			return err
		}
		if err := saveMovieMatchKeysByMidTx(tx, buildMovieMatchKeyMappings(writeDetails, infoByKey, keyToMid)); err != nil {
			return err
		}

		mid, ok := keyToMid[snapshot.ContentKey]
		if !ok || mid <= 0 {
			return nil
		}
		snapshot.Mid = mid
		savedMid = mid
		return RefreshPlayFromSummaryByIndexesTx(tx, []model.FilmIndex{snapshot})
	}); err != nil {
		return err
	}
	if err := repository.TouchCollectSourceStatsTx(db.Mdb, id, time.Now()); err != nil {
		log.Printf("TouchCollectSourceStats Error: %v", err)
	}
	if !changed {
		return nil
	}

	BatchHandleSearchTag(snapshot)
	clearDetailCaches(snapshot.Pid)
	ClearProvideListCache()
	if err := UpsertActiveSnapshotByMid(savedMid); err != nil {
		return err
	}
	if err := RefreshActiveReadModelArtifacts(); err != nil {
		return err
	}
	return nil
}

func reloadFilmIndexesByContentKeys(contentKeys []string) []model.FilmIndex {
	return reloadFilmIndexesByContentKeysTx(db.Mdb, contentKeys)
}

func reloadFilmIndexesByContentKeysTx(tx *gorm.DB, contentKeys []string) []model.FilmIndex {
	if len(contentKeys) == 0 {
		return nil
	}
	var infos []model.FilmIndex
	if err := tx.Where("content_key IN ?", contentKeys).Find(&infos).Error; err != nil {
		return nil
	}
	return infos
}

func filmIndexMIDs(infos []model.FilmIndex) []int64 {
	mids := make([]int64, 0, len(infos))
	seen := make(map[int64]struct{}, len(infos))
	for _, info := range infos {
		if info.Mid <= 0 {
			continue
		}
		if _, ok := seen[info.Mid]; ok {
			continue
		}
		seen[info.Mid] = struct{}{}
		mids = append(mids, info.Mid)
	}
	return mids
}

func reloadFilmIndexesByMidsTx(tx *gorm.DB, mids []int64) []model.FilmIndex {
	if len(mids) == 0 {
		return nil
	}
	var infos []model.FilmIndex
	if err := tx.Where("mid IN ?", mids).Find(&infos).Error; err != nil {
		return nil
	}
	return infos
}

func BatchHandleSearchTag(infos ...model.FilmIndex) {
	if len(infos) == 0 {
		return
	}

	pids := collectSearchTagPidList(infos)
	if err := RefreshSearchTagsByPids(pids...); err != nil {
		log.Printf("RefreshSearchTagsByPids Error: %v", err)
		return
	}

	ClearAllSearchTagsCache()
	ClearAdminFilmSearchCache()
}

func UpdateSearchTagsForVisibleCollect(infos ...model.FilmIndex) {
	if len(infos) == 0 {
		return
	}
	for _, info := range infos {
		if err := handleDynamicSearchTagsTx(db.Mdb, info); err != nil {
			log.Printf("UpdateSearchTagsForVisibleCollect Error: %v", err)
		}
	}
	invalidateSearchTagsVisibleCacheThrottled()
}

func invalidateSearchTagsVisibleCacheThrottled() {
	now := time.Now()
	searchTagsVisibleCacheState.mu.Lock()
	if !searchTagsVisibleCacheState.lastAt.IsZero() && now.Sub(searchTagsVisibleCacheState.lastAt) < searchTagsVisibleCacheInvalidateInterval {
		searchTagsVisibleCacheState.mu.Unlock()
		return
	}
	searchTagsVisibleCacheState.lastAt = now
	searchTagsVisibleCacheState.mu.Unlock()

	ClearAllSearchTagsCache()
	ClearAdminFilmSearchCache()
}

func normalizeOrderedPids(pids []int64) []int64 {
	pidSet := make(map[int64]struct{}, len(pids))
	orderedPids := make([]int64, 0, len(pids))
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		if _, ok := pidSet[pid]; ok {
			continue
		}
		pidSet[pid] = struct{}{}
		orderedPids = append(orderedPids, pid)
	}
	return orderedPids
}

func RefreshSearchTagsByPids(pids ...int64) error {
	orderedPids := normalizeOrderedPids(pids)
	if len(orderedPids) == 0 {
		return nil
	}

	start := time.Now()
	totalFilms := 0
	for idx, pid := range orderedPids {
		pidStart := time.Now()
		films, err := rebuildSearchTagsForPid(pid)
		if err != nil {
			return err
		}
		totalFilms += films
		log.Printf("[SearchTags] 标签重建进度 pid=%d (%d/%d) films=%d cost=%s total=%s",
			pid, idx+1, len(orderedPids), films, time.Since(pidStart), time.Since(start))
	}

	for _, pid := range orderedPids {
		ClearSearchTagsCache(pid)
	}
	log.Printf("[SearchTags] 标签重建完成 pids=%d films=%d cost=%s",
		len(orderedPids), totalFilms, time.Since(start))
	return nil
}

func rebuildSearchTagsForPid(pid int64) (int, error) {
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("pid = ?", pid).Delete(&model.SearchTagItem{}).Error; err != nil {
			return err
		}
		initializedPids.Delete(pid)
		return ensureStaticTagsForPidTx(tx, pid)
	}); err != nil {
		return 0, err
	}

	var infos []model.FilmIndex
	if err := db.Mdb.Where("pid = ?", pid).Find(&infos).Error; err != nil {
		return 0, err
	}
	if len(infos) == 0 {
		return 0, nil
	}

	for offset := 0; offset < len(infos); offset += searchTagsRebuildFilmBatchSize {
		end := offset + searchTagsRebuildFilmBatchSize
		if end > len(infos) {
			end = len(infos)
		}
		batch := infos[offset:end]
		items := aggregateSearchTagItems(collectDynamicSearchTagItemsBatch(batch))
		if len(items) == 0 {
			continue
		}
		if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
			return bulkUpsertSearchTagItemsTx(tx, items)
		}); err != nil {
			return 0, err
		}
	}
	return len(infos), nil
}

func collectDynamicSearchTagItemsBatch(infos []model.FilmIndex) []model.SearchTagItem {
	out := make([]model.SearchTagItem, 0, len(infos)*5)
	for _, info := range infos {
		out = append(out, collectDynamicSearchTagItems(info)...)
	}
	return out
}

func collectDynamicSearchTagItems(info model.FilmIndex) []model.SearchTagItem {
	if info.Pid <= 0 {
		return nil
	}
	items := make([]model.SearchTagItem, 0, 8)

	if info.Cid > 0 {
		catName := support.GetCategoryNameById(info.Cid)
		if catName == "" {
			catName = info.CName
		}
		items = append(items, collectSearchTagItems(catName, "Category", info.Pid, fmt.Sprint(info.Cid))...)
	}

	mainCategoryName := support.GetMainCategoryName(info.Pid)
	cleanPlot := support.CleanPlotTags(info.ClassTag, info.Area, mainCategoryName, info.CName)
	items = append(items, collectSearchTagItems(cleanPlot, "Plot", info.Pid)...)

	items = append(items, collectSearchTagItems(info.Area, "Area", info.Pid)...)
	items = append(items, collectSearchTagItems(info.Language, "Language", info.Pid)...)
	if info.Year > 0 {
		items = append(items, collectSearchTagItems(fmt.Sprint(info.Year), "Year", info.Pid)...)
	}
	return items
}

func collectSearchTagItems(allTags, tagType string, pid int64, customValues ...string) []model.SearchTagItem {
	allTags = reTagCleanup.ReplaceAllString(allTags, "")
	parts := reTagSplit.Split(allTags, -1)
	items := make([]model.SearchTagItem, 0, len(parts))
	for _, t := range parts {
		var customVal []string
		if tagType == "Category" && len(customValues) > 0 {
			customVal = customValues[:1]
		}
		if item, ok := buildSearchTagItem(t, tagType, pid, customVal...); ok {
			items = append(items, item)
		}
	}
	return items
}

func buildSearchTagItem(rawValue, tagType string, pid int64, customVal ...string) (model.SearchTagItem, bool) {
	v := normalizeSearchTagValue(tagType, rawValue)
	if v == "" || v == model.TagOthersValue || v == "其他" || v == "其它" || v == "全部" || v == "完结" || v == "HD" || v == "解说" || v == "剧情" || v == "暂无" {
		return model.SearchTagItem{}, false
	}
	val := v
	if len(customVal) > 0 {
		val = normalizeSearchTagValue(tagType, customVal[0])
		if val == "" {
			return model.SearchTagItem{}, false
		}
	}
	if tagType == "Category" && val == fmt.Sprint(pid) {
		return model.SearchTagItem{}, false
	}
	if tagType == "Year" {
		if y, _ := strconv.Atoi(v); y <= 0 {
			return model.SearchTagItem{}, false
		}
	}
	return model.SearchTagItem{Pid: pid, TagType: tagType, Name: v, Value: val, Score: 1}, true
}

func aggregateSearchTagItems(items []model.SearchTagItem) []model.SearchTagItem {
	if len(items) == 0 {
		return nil
	}
	type key struct {
		pid     int64
		tagType string
		value   string
	}
	agg := make(map[key]int, len(items))
	first := make(map[key]model.SearchTagItem, len(items))
	for _, item := range items {
		k := key{item.Pid, item.TagType, item.Value}
		if _, seen := first[k]; !seen {
			first[k] = item
		}
		agg[k]++
	}
	out := make([]model.SearchTagItem, 0, len(agg))
	for k, count := range agg {
		row := first[k]
		row.Score = int64(count)
		out = append(out, row)
	}
	return out
}

func bulkUpsertSearchTagItemsTx(tx *gorm.DB, items []model.SearchTagItem) error {
	if len(items) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "pid"}, {Name: "tag_type"}, {Name: "value"}},
		DoUpdates: clause.Assignments(map[string]any{
			"score":      gorm.Expr("score + VALUES(score)"),
			"name":       gorm.Expr("VALUES(name)"),
			"deleted_at": nil,
		}),
	}).CreateInBatches(items, upsertBatchSize).Error
}

func RefreshSearchTagsByMids(mids ...int64) error {
	if len(mids) == 0 {
		return nil
	}
	var infos []model.FilmIndex
	if err := db.Mdb.Select("pid").Where("mid IN ?", mids).Find(&infos).Error; err != nil {
		return err
	}
	return RefreshSearchTagsByPids(collectSearchTagPidList(infos)...)
}

func RebuildSearchTagsByPids(pids ...int64) error {
	return RefreshSearchTagsByPids(pids...)
}

func SaveSearchTag(filmIndex model.FilmIndex) {
	BatchHandleSearchTag(filmIndex)
}

func collectSearchTagPids(infos []model.FilmIndex) map[int64]bool {
	pids := make(map[int64]bool)
	for _, info := range infos {
		if info.Pid > 0 {
			pids[info.Pid] = true
		}
	}
	return pids
}

func collectSearchTagPidList(infos []model.FilmIndex) []int64 {
	pidSet := collectSearchTagPids(infos)
	pids := make([]int64, 0, len(pidSet))
	for pid := range pidSet {
		pids = append(pids, pid)
	}
	return pids
}

func handleDynamicSearchTags(info model.FilmIndex) {
	_ = handleDynamicSearchTagsTx(db.Mdb, info)
}

func handleDynamicSearchTagsTx(tx *gorm.DB, info model.FilmIndex) error {
	if info.Pid <= 0 {
		return nil
	}

	if err := handleCategorySearchTagTx(tx, info); err != nil {
		return err
	}
	if err := handlePlotSearchTagTx(tx, info); err != nil {
		return err
	}
	if err := HandleSearchTagsTx(tx, info.Area, "Area", info.Pid); err != nil {
		return err
	}
	if err := HandleSearchTagsTx(tx, info.Language, "Language", info.Pid); err != nil {
		return err
	}
	if info.Year > 0 {
		if err := HandleSearchTagsTx(tx, fmt.Sprint(info.Year), "Year", info.Pid); err != nil {
			return err
		}
	}
	return nil
}

func handleCategorySearchTag(info model.FilmIndex) {
	_ = handleCategorySearchTagTx(db.Mdb, info)
}

func handleCategorySearchTagTx(tx *gorm.DB, info model.FilmIndex) error {
	if info.Cid <= 0 {
		return nil
	}

	catName := support.GetCategoryNameById(info.Cid)
	if catName == "" {
		catName = info.CName
	}
	return HandleSearchTagsTx(tx, catName, "Category", info.Pid, fmt.Sprint(info.Cid))
}

func handlePlotSearchTag(info model.FilmIndex) {
	_ = handlePlotSearchTagTx(db.Mdb, info)
}

func handlePlotSearchTagTx(tx *gorm.DB, info model.FilmIndex) error {
	mainCategoryName := support.GetMainCategoryName(info.Pid)
	cleanPlot := support.CleanPlotTags(info.ClassTag, info.Area, mainCategoryName, info.CName)
	return HandleSearchTagsTx(tx, cleanPlot, "Plot", info.Pid)
}

func ensureStaticTagsForPid(pid int64) {
	_ = ensureStaticTagsForPidTx(db.Mdb, pid)
}

func ensureStaticTagsForPidTx(tx *gorm.DB, pid int64) error {
	if _, ok := initializedPids.Load(pid); ok {
		return nil
	}

	var initialItems []model.SearchTagItem
	for i := 65; i <= 90; i++ {
		v := string(rune(i))
		initialItems = append(initialItems, model.SearchTagItem{Pid: pid, TagType: "Initial", Name: v, Value: v, Score: int64(90 - i)})
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initialItems).Error; err != nil {
		return err
	}
	initializedPids.Store(pid, true)
	return nil
}

var (
	reTagCleanup = regexp.MustCompile(`[\s\n\r]+`)
	reTagSplit   = regexp.MustCompile(`[/,，、\s\.\+\|]`)
)

func normalizeSearchTagValue(tagType string, value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, ":：")

	switch tagType {
	case "Area":
		switch value {
		case "地区", "制片国家", "制片国家地区":
			return ""
		}
	case "Language":
		switch value {
		case "语言", "对白语言":
			return ""
		}
	}

	return value
}

func HandleSearchTags(allTags string, tagType string, pid int64, customValues ...string) {
	_ = HandleSearchTagsTx(db.Mdb, allTags, tagType, pid, customValues...)
}

func HandleSearchTagsTx(tx *gorm.DB, allTags string, tagType string, pid int64, customValues ...string) error {
	allTags = reTagCleanup.ReplaceAllString(allTags, "")
	parts := reTagSplit.Split(allTags, -1)
	var saveErr error

	upsert := func(v string, customVal ...string) {
		v = normalizeSearchTagValue(tagType, v)
		if v == "" || v == model.TagOthersValue || v == "其他" || v == "其它" || v == "全部" || v == "完结" || v == "HD" || v == "解说" || v == "剧情" || v == "暂无" {
			return
		}

		val := v
		if len(customVal) > 0 {
			val = normalizeSearchTagValue(tagType, customVal[0])
			if val == "" {
				return
			}
		}

		if tagType == "Category" && val == fmt.Sprint(pid) {
			return
		}

		if tagType == "Year" {
			if y, _ := strconv.Atoi(v); y <= 0 {
				return
			}
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "pid"}, {Name: "tag_type"}, {Name: "value"}},
			DoUpdates: clause.Assignments(map[string]any{
				"score":      gorm.Expr("score + 1"),
				"name":       v,
				"deleted_at": nil,
			}),
		}).Create(&model.SearchTagItem{Pid: pid, TagType: tagType, Name: v, Value: val, Score: 1}).Error; err != nil {
			saveErr = err
		}
	}

	for _, t := range parts {
		if saveErr != nil {
			return saveErr
		}
		if tagType == "Category" && len(customValues) > 0 {
			upsert(t, customValues[0])
		} else {
			upsert(t)
		}
	}
	return saveErr
}

func resolveLocalCategory(pid int64, cid int64, cName string) resolvedSearchCategory {
	result := resolvedSearchCategory{CName: strings.TrimSpace(cName)}
	if cid > 0 {
		result.Cid = cid
	}
	if result.Cid > 0 {
		result.Pid = support.GetRootId(result.Cid)
	}
	if result.Pid == 0 && pid > 0 {
		result.Pid = pid
	}
	if result.Pid > 0 && result.Cid > 0 && result.CName == "" {
		result.CName = support.GetCategoryNameById(result.Cid)
	}
	if result.Pid > 0 && result.PKey == "" {
		result.PKey = support.GetCategoryStableKeyByID(result.Pid)
	}
	if result.Cid > 0 {
		result.CKey = support.GetCategoryStableKeyByID(result.Cid)
	}
	return result
}

type resolvedSearchCategory struct {
	Pid              int64
	Cid              int64
	CName            string
	OriginalCategory string
	PKey             string
	CKey             string
}

func resolveOriginalCategoryName(sourceId string, sourcePid int64, sourceCid int64, fallback string) string {
	fallback = strings.TrimSpace(fallback)
	if strings.TrimSpace(sourceId) == "manual" {
		return fallback
	}

	if sourcePid > 0 {
		var root model.SourceCategory
		if err := db.Mdb.Select("raw_name").Where("source_id = ? AND source_type_id = ?", sourceId, sourcePid).First(&root).Error; err == nil {
			name := strings.TrimSpace(root.RawName)
			if name != "" {
				return name
			}
		}
	}

	if sourceCid > 0 {
		var row model.SourceCategory
		if err := db.Mdb.Select("raw_name", "parent_source_type_id").Where("source_id = ? AND source_type_id = ?", sourceId, sourceCid).First(&row).Error; err == nil {
			if row.ParentSourceTypeId == 0 {
				name := strings.TrimSpace(row.RawName)
				if name != "" {
					return name
				}
			}
			if row.ParentSourceTypeId > 0 {
				var parent model.SourceCategory
				if err := db.Mdb.Select("raw_name").Where("source_id = ? AND source_type_id = ?", sourceId, row.ParentSourceTypeId).First(&parent).Error; err == nil {
					name := strings.TrimSpace(parent.RawName)
					if name != "" {
						return name
					}
				}
			}
		}
	}

	return fallback
}

func resolveSourceRootTypeID(sourceId string, sourcePid int64, sourceCid int64) int64 {
	if strings.TrimSpace(sourceId) == "" {
		return 0
	}
	if sourcePid > 0 {
		return sourcePid
	}
	if sourceCid <= 0 {
		return 0
	}

	current := sourceCid
	for range [5]int{} {
		var row model.SourceCategory
		if err := db.Mdb.Select("parent_source_type_id").Where("source_id = ? AND source_type_id = ?", sourceId, current).First(&row).Error; err != nil {
			return current
		}
		if row.ParentSourceTypeId <= 0 {
			return current
		}
		current = row.ParentSourceTypeId
	}
	return current
}

type normalizedSearchMeta struct {
	Score       float64
	UpdateStamp int64
	Year        int64
	Area        string
	Language    string
	ClassTag    string
}

func resolveSearchCategory(sourceId string, detail model.MovieDetail) resolvedSearchCategory {
	if strings.TrimSpace(sourceId) == "manual" {
		category := resolveLocalCategory(detail.Pid, detail.Cid, detail.CName)
		category.OriginalCategory = strings.TrimSpace(detail.CName)
		return category
	}

	sourceCid := detail.Cid
	sourcePid := detail.Pid
	if detail.RawCid > 0 {
		sourceCid = detail.RawCid
	}
	if detail.RawPid > 0 {
		sourcePid = detail.RawPid
	}

	result := resolvedSearchCategory{CName: strings.TrimSpace(detail.CName)}
	result.OriginalCategory = resolveOriginalCategoryName(sourceId, sourcePid, sourceCid, detail.CName)
	rootSourceTypeID := resolveSourceRootTypeID(sourceId, sourcePid, sourceCid)
	result.PKey = support.BuildSourceCategoryKey(sourceId, rootSourceTypeID)
	result.CKey = support.BuildSourceCategoryKey(sourceId, sourceCid)
	result.Cid = support.GetLocalCategoryId(sourceId, sourceCid)
	if result.Cid > 0 {
		result.Pid = support.GetRootId(result.Cid)
	}
	if result.Pid == 0 {
		result.Pid = support.GetRootId(support.GetLocalCategoryId(sourceId, sourcePid))
	}
	if result.Pid > 0 && result.Cid == 0 && result.CName != "" {
		var category model.Category
		if err := db.Mdb.Where("pid = ? AND name = ?", result.Pid, result.CName).First(&category).Error; err == nil {
			result.Cid = category.Id
		}
	}
	if result.Pid > 0 && result.CName == "" {
		result.CName = support.GetCategoryNameById(result.Pid)
	}
	if result.PKey == "" && result.Pid > 0 {
		result.PKey = support.GetCategoryStableKeyByID(result.Pid)
	}
	if result.CKey == "" && result.Cid > 0 {
		result.CKey = support.GetCategoryStableKeyByID(result.Cid)
	}
	return result
}

func normalizeSearchMetadata(detail model.MovieDetail, category resolvedSearchCategory) (normalizedSearchMeta, error) {
	score, _ := strconv.ParseFloat(detail.DbScore, 64)
	year, err := strconv.ParseInt(regexp.MustCompile(`[1-9][0-9]{3}`).FindString(detail.ReleaseDate), 10, 64)
	if err != nil {
		year = 0
	}
	updateStamp, err := utils.ParseCollectUpdateTime(detail.UpdateTime)
	if err != nil {
		// 解析失败不阻断入库；用当前时间兜底，避免整条元数据失败
		// 可能影响按 update_stamp 排序的「最新」顺序，属可接受降级
		log.Printf("[FilmWrite] 更新时间解析失败，使用当前时间兜底 name=%s id=%d updateTime=%q err=%v",
			detail.Name, detail.Id, detail.UpdateTime, err)
		updateStamp = time.Now().Unix()
	}

	finalArea := support.NormalizeArea(detail.Area)
	finalLang := support.NormalizeLanguage(detail.Language)
	mainCategoryName := support.GetMainCategoryName(category.Pid)

	return normalizedSearchMeta{
		Score:       score,
		UpdateStamp: updateStamp,
		Year:        year,
		Area:        finalArea,
		Language:    finalLang,
		ClassTag:    support.CleanPlotTags(detail.ClassTag, finalArea, mainCategoryName, category.CName),
	}, nil
}

func buildFilmIndex(sourceId string, detail model.MovieDetail, category resolvedSearchCategory, meta normalizedSearchMeta, categoryVersion string, ruleVersion string) model.FilmIndex {
	return model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid:        detail.Id,
			ContentKey: BuildContentKey(detail),
			SourceId:   sourceId,
			DbId:       detail.DbId,
		},
		FilmIndexCategory: model.FilmIndexCategory{
			Cid:              category.Cid,
			Pid:              category.Pid,
			RootCategoryKey:  category.PKey,
			CategoryKey:      category.CKey,
			OriginalCategory: category.OriginalCategory,
			CName:            category.CName,
		},
		FilmIndexContent: model.FilmIndexContent{
			SeriesKey:    utils.BuildSeriesKey(detail.Name, detail.SubTitle),
			Name:         detail.Name,
			SubTitle:     detail.SubTitle,
			ClassTag:     meta.ClassTag,
			Area:         meta.Area,
			Language:     meta.Language,
			Year:         meta.Year,
			Initial:      detail.Initial,
			Score:        meta.Score,
			UpdateStamp:  meta.UpdateStamp,
			Hits:         detail.Hits,
			State:        detail.State,
			Remarks:      detail.Remarks,
			Picture:      detail.Picture,
			PictureSlide: detail.PictureSlide,
			Actor:        detail.Actor,
			Director:     detail.Director,
			Blurb:        detail.Blurb,
		},
		FilmIndexVersion: model.FilmIndexVersion{
			CollectStamp:    detail.AddTime,
			CategoryVersion: categoryVersion,
			RuleVersion:     ruleVersion,
		},
	}
}

func ConvertFilmIndex(sourceId string, detail model.MovieDetail, categoryVersion string, ruleVersion string) (model.FilmIndex, error) {
	category := resolveSearchCategory(sourceId, detail)
	meta, err := normalizeSearchMetadata(detail, category)
	if err != nil {
		return model.FilmIndex{}, err
	}
	return buildFilmIndex(sourceId, detail, category, meta, categoryVersion, ruleVersion), nil
}
