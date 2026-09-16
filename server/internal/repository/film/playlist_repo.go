package film

import (
	"encoding/json"
	"log"
	"sort"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/support"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SaveSitePlayList 写入附属站播放列表。
// NotifyMIDs：本源追集且集数超过全库其它源（进最近更新）。
// AffectedMIDs：任意 playlist 实质写入（含追平主站、仅链接刷新），详情缓存必须失效并展示最新集。
func SaveSitePlayList(sourceID string, list []model.MovieDetail) (CollectWriteResult, error) {
	if len(list) == 0 {
		return CollectWriteResult{}, nil
	}

	var playlists []model.SlaveMoviePlaylist
	keysByMovieKey := make(map[string]struct{}, len(list)*2)

	detailMids, primaryKeyByMid, err := matchSlaveDetailMids(list)
	if err != nil {
		return CollectWriteResult{}, err
	}
	inheritedKeyByLookup := loadInheritedKeysForUnmatchedDetails(list, detailMids)

	for index, detail := range list {
		if !isPlaylistWritableDetail(detail) {
			continue
		}

		writeKeys := playlistWriteKeys(detail, detailMids[index], primaryKeyByMid, inheritedKeyByLookup)
		for _, movieKey := range writeKeys {
			keysByMovieKey[movieKey] = struct{}{}

			for groupIndex, links := range detail.PlayList {
				if len(links) == 0 {
					continue
				}

				data, _ := json.Marshal(links)
				rawName := ""
				if groupIndex < len(detail.PlayFrom) {
					rawName = strings.TrimSpace(detail.PlayFrom[groupIndex])
				}

				playlists = append(playlists, model.SlaveMoviePlaylist{
					SourceId:   sourceID,
					MovieKey:   movieKey,
					GroupIndex: groupIndex,
					GroupName:  rawName,
					Content:    string(data),
				})
			}
		}
	}

	if len(keysByMovieKey) == 0 {
		return CollectWriteResult{}, nil
	}

	changes, err := saveGroupedPlaylists(sourceID, playlists, keysByMovieKey)
	if err != nil {
		log.Printf("SaveSitePlayList Error: %v", err)
		return CollectWriteResult{}, err
	}
	result, err := scheduleSearchInfoRefreshByPlaylists(sourceID, list, changes)
	if err != nil {
		log.Printf("scheduleSearchInfoRefreshByPlaylists Error: %v", err)
		return CollectWriteResult{}, err
	}
	// 仅在有播放源实质变更时更新 last_collect_time。
	if len(changes) > 0 {
		repository.NoteCollectSourceStats(sourceID)
	}

	return result, nil
}

func isPlaylistWritableDetail(detail model.MovieDetail) bool {
	return len(detail.PlayList) > 0 && !strings.Contains(detail.CName, "解说")
}

// playlistWriteKeys 一条附属站详情落到哪些 movie_key。唯一命中写该片主键；否则写候选键（有大类则不含纯片名）。
func playlistWriteKeys(
	detail model.MovieDetail,
	mid int64,
	primaryKeyByMid map[int64]string,
	inheritedKeyByLookup map[string]string,
) []string {
	if mid > 0 && primaryKeyByMid[mid] != "" {
		return []string{primaryKeyByMid[mid]}
	}
	if ResolveMovieDetailRootPid(detail) == 0 {
		for _, lookupKey := range BuildPlaylistMovieKeys(detail) {
			if inherited := inheritedKeyByLookup[lookupKey]; inherited != "" {
				return []string{inherited}
			}
		}
	}
	return BuildPlaylistCandidateKeys(detail)
}

func filmIndexRootPid(info model.FilmIndex) int64 {
	if info.Pid > 0 {
		if root := support.GetRootId(info.Pid); root > 0 {
			return root
		}
	}
	if info.Cid > 0 {
		if root := support.GetRootId(info.Cid); root > 0 {
			return root
		}
	}
	// 兜底与写入侧 buildMovieMatchKeyMappings 一致：pid/cid 未落到根分类时按分类名解析，
	// 否则这些影片的「片名#大类」键会被当成非规范键删掉。
	return support.ResolveRootCategoryIDByCName(info.CName)
}

func slaveDetailMatchesFilm(detailPid int64, info model.FilmIndex) bool {
	infoPid := filmIndexRootPid(info)
	if detailPid > 0 && infoPid > 0 && infoPid != detailPid {
		return false
	}
	return true
}

// pickUniqueSlaveMid 按键优先级（豆瓣、片名#大类、纯片名）找唯一主站 mid。
// 某键只命中一部就采用，不管副站大类是否和主站一致（源站常把动漫标成电视剧）。
// 某键命中多部时才用大类消歧（两个仙逆）；消歧后仍不唯一则看下一把键。
func pickUniqueSlaveMid(
	keys []string,
	detailPid int64,
	midsByLookupKey map[string][]int64,
	infoByMid map[int64]model.FilmIndex,
) int64 {
	for _, key := range keys {
		cands := uniquePositiveMIDs(midsByLookupKey[key])
		if len(cands) == 0 {
			continue
		}
		if len(cands) == 1 {
			return cands[0]
		}
		filtered := make([]int64, 0, len(cands))
		for _, mid := range cands {
			info, ok := infoByMid[mid]
			if !ok || !slaveDetailMatchesFilm(detailPid, info) {
				continue
			}
			filtered = append(filtered, mid)
		}
		if len(filtered) == 1 {
			return filtered[0]
		}
	}
	return 0
}

// matchSlaveDetailMids 按同一套键给附属站详情找唯一主站 mid。
func matchSlaveDetailMids(list []model.MovieDetail) ([]int64, map[int64]string, error) {
	detailMids := make([]int64, len(list))
	if len(list) == 0 {
		return detailMids, nil, nil
	}

	keysPerDetail := make([][]string, len(list))
	allKeys := make([]string, 0, len(list)*3)
	for i, detail := range list {
		if !isPlaylistWritableDetail(detail) {
			continue
		}
		keys := BuildPlaylistMovieKeys(detail)
		keysPerDetail[i] = keys
		allKeys = append(allKeys, keys...)
	}
	midsByLookupKey := loadMidCandidatesByMatchKeys(allKeys)
	midSet := make(map[int64]struct{})
	for _, mids := range midsByLookupKey {
		for _, mid := range mids {
			if mid > 0 {
				midSet[mid] = struct{}{}
			}
		}
	}
	if len(midSet) == 0 {
		return detailMids, nil, nil
	}
	matchedMids := make([]int64, 0, len(midSet))
	for mid := range midSet {
		matchedMids = append(matchedMids, mid)
	}

	var candidates []model.FilmIndex
	if err := db.Mdb.Where("mid IN ?", matchedMids).Find(&candidates).Error; err != nil {
		return nil, nil, err
	}
	infoByMid := make(map[int64]model.FilmIndex, len(candidates))
	for _, info := range candidates {
		infoByMid[info.Mid] = info
	}
	keysByMid := loadMovieMatchKeysByMids(matchedMids)
	primaryKeyByMid := make(map[int64]string, len(keysByMid))
	for mid, keys := range keysByMid {
		if len(keys) > 0 {
			primaryKeyByMid[mid] = keys[0]
		}
	}

	for i, detail := range list {
		if !isPlaylistWritableDetail(detail) {
			continue
		}
		mid := pickUniqueSlaveMid(keysPerDetail[i], ResolveMovieDetailRootPid(detail), midsByLookupKey, infoByMid)
		if mid > 0 && primaryKeyByMid[mid] != "" {
			detailMids[i] = mid
		}
	}
	return detailMids, primaryKeyByMid, nil
}

func loadInheritedKeysForUnmatchedDetails(list []model.MovieDetail, detailMids []int64) map[string]string {
	uncategorizedKeys := make([]string, 0)
	for i, detail := range list {
		if i < len(detailMids) && detailMids[i] > 0 {
			continue
		}
		if !isPlaylistWritableDetail(detail) {
			continue
		}
		if ResolveMovieDetailRootPid(detail) != 0 {
			continue
		}
		uncategorizedKeys = append(uncategorizedKeys, BuildPlaylistMovieKeys(detail)...)
	}
	if len(uncategorizedKeys) == 0 {
		return nil
	}
	midsByLookupKey := loadMidCandidatesByMatchKeys(uncategorizedKeys)
	candidateMids := make([]int64, 0)
	for _, mids := range midsByLookupKey {
		candidateMids = append(candidateMids, mids...)
	}
	keysByMid := loadMovieMatchKeysByMids(candidateMids)
	inheritedKeyByLookup := make(map[string]string)
	for lookupKey, mids := range midsByLookupKey {
		if inherited := inheritPrimaryMovieKeyIfUnique(mids, keysByMid); inherited != "" {
			inheritedKeyByLookup[lookupKey] = inherited
		}
	}
	return inheritedKeyByLookup
}

// scheduleSearchInfoRefreshByPlaylists 刷新附属站映射/时间戳。
// NotifyMIDs 仅 stamp 资格；AffectedMIDs 含所有 playlist 写入，供详情页展示最新集。
func scheduleSearchInfoRefreshByPlaylists(sourceID string, details []model.MovieDetail, changes []playlistChange) (CollectWriteResult, error) {
	var out CollectWriteResult
	infos, err := loadMatchedSearchInfosByDetails(details)
	if err != nil {
		return out, err
	}
	if err := saveSlaveSourceMappings(sourceID, details, infos); err != nil {
		return out, err
	}
	// 附属站海报同步：若当前源开启了 IsPosterSource，将高清海报同步写入主站影片并加入刷新列表
	posterUpdatedMids, err := SyncSlavePostersIfConfiguredTx(db.Mdb, sourceID, details, infos)
	if err != nil {
		log.Printf("SyncSlavePostersIfConfiguredTx Error: %v", err)
	}

	// 更新列表：本源追集且集数超过全库其它源（后到的同集数源不重进）
	notifyMids, err := touchSlavePlaylistUpdateStamps(sourceID, changes)
	if err != nil {
		return out, err
	}
	out.NotifyMIDs = notifyMids
	refreshMIDs := slavePlaylistAffectedMIDs(changes)
	if len(posterUpdatedMids) > 0 {
		refreshMIDs = append(refreshMIDs, posterUpdatedMids...)
	}
	seen := make(map[int64]struct{}, len(refreshMIDs))
	affected := make([]int64, 0, len(refreshMIDs))
	refreshInfos := make([]model.FilmIndex, 0, len(refreshMIDs))
	for _, mid := range refreshMIDs {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		affected = append(affected, mid)
		refreshInfos = append(refreshInfos, model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: mid},
		})
	}
	if len(refreshInfos) > 0 {
		SchedulePlaySummaryRefresh(refreshInfos...)
	}
	out.AffectedMIDs = affected
	return out, nil
}

// slavePlaylistAffectedMIDs 任意 playlist 写入（含仅链接刷新）涉及的 mid，每个 movie_key 只取一个最优 mid。
func slavePlaylistAffectedMIDs(changes []playlistChange) []int64 {
	if len(changes) == 0 {
		return nil
	}
	keys := make([]string, 0, len(changes))
	for _, c := range changes {
		if k := strings.TrimSpace(c.MovieKey); k != "" {
			keys = append(keys, k)
		}
	}
	midsByKey := loadMidCandidatesByMatchKeys(keys)
	bestByKey := pickBestMidsByKey(midsByKey)
	seen := make(map[int64]struct{})
	out := make([]int64, 0, len(changes))
	for _, c := range changes {
		mid := bestByKey[c.MovieKey]
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		out = append(out, mid)
	}
	return out
}

func loadMatchedSearchInfosByDetails(details []model.MovieDetail) ([]model.FilmIndex, error) {
	type detailLookup struct {
		detail model.MovieDetail
		keys   []string
	}

	lookups := make([]detailLookup, 0, len(details))
	allKeys := make([]string, 0, len(details)*4)

	for _, detail := range details {
		lookupKeys := BuildPlaylistMovieKeys(detail)
		if len(lookupKeys) == 0 {
			continue
		}
		lookups = append(lookups, detailLookup{detail: detail, keys: lookupKeys})
		allKeys = append(allKeys, lookupKeys...)
	}

	if len(lookups) == 0 {
		return nil, nil
	}

	midsByLookupKey := loadMidCandidatesByMatchKeys(allKeys)
	matchedMidSet := make(map[int64]struct{}, len(allKeys))
	for _, mids := range midsByLookupKey {
		for _, mid := range mids {
			matchedMidSet[mid] = struct{}{}
		}
	}
	if len(matchedMidSet) == 0 {
		return nil, nil
	}

	matchedMids := make([]int64, 0, len(matchedMidSet))
	for mid := range matchedMidSet {
		matchedMids = append(matchedMids, mid)
	}

	var candidates []model.FilmIndex
	if err := db.Mdb.Where("mid IN ?", matchedMids).Find(&candidates).Error; err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	infoByMid := make(map[int64]model.FilmIndex, len(candidates))
	for _, info := range candidates {
		infoByMid[info.Mid] = info
	}

	ordered := make([]model.FilmIndex, 0, len(candidates))
	seenMid := make(map[int64]struct{}, len(candidates))
	for _, item := range lookups {
		mid := pickUniqueSlaveMid(item.keys, ResolveMovieDetailRootPid(item.detail), midsByLookupKey, infoByMid)
		if mid <= 0 {
			continue
		}
		info, ok := infoByMid[mid]
		if !ok {
			continue
		}
		if _, seen := seenMid[mid]; seen {
			continue
		}
		seenMid[mid] = struct{}{}
		ordered = append(ordered, info)
	}

	return ordered, nil
}

func loadMatchedSearchInfosByMovieKeys(movieKeys []string) ([]model.FilmIndex, error) {
	midsByLookupKey := loadMidCandidatesByMatchKeys(movieKeys)
	if len(midsByLookupKey) == 0 {
		return nil, nil
	}

	midSet := make(map[int64]struct{}, len(movieKeys))
	for _, mids := range midsByLookupKey {
		for _, mid := range mids {
			if mid > 0 {
				midSet[mid] = struct{}{}
			}
		}
	}
	if len(midSet) == 0 {
		return nil, nil
	}

	mids := make([]int64, 0, len(midSet))
	for mid := range midSet {
		mids = append(mids, mid)
	}

	var infos []model.FilmIndex
	if err := db.Mdb.Where("mid IN ?", mids).Find(&infos).Error; err != nil {
		return nil, err
	}
	return infos, nil
}

// touchSlavePlaylistUpdateStamps 刷新「应进更新列表」影片的 update_stamp，返回这些全局 mid。
// 仅链接刷新的保存已在 saveGroupedPlaylists 完成，不抬 stamp、不进更新列表。
func touchSlavePlaylistUpdateStamps(sourceID string, changes []playlistChange) ([]int64, error) {
	notifyChanges := make([]playlistChange, 0, len(changes))
	for _, c := range changes {
		if c.NotifyWorthy || c.CountIncreased || c.FirstInsert {
			notifyChanges = append(notifyChanges, c)
		}
	}
	updateStampByMid, err := buildSlavePlaylistUpdateStamps(sourceID, notifyChanges)
	if err != nil {
		return nil, err
	}
	if len(updateStampByMid) == 0 {
		return nil, nil
	}
	caseExpr := "CASE mid"
	mids := make([]int64, 0, len(updateStampByMid))
	args := make([]any, 0, len(updateStampByMid)*2)
	for mid, updateStamp := range updateStampByMid {
		caseExpr += " WHEN ? THEN ?"
		args = append(args, mid, updateStamp)
		mids = append(mids, mid)
	}
	caseExpr += " ELSE update_stamp END"
	if err := db.Mdb.Model(&model.FilmIndex{}).
		Where("mid IN ?", mids).
		Update("update_stamp", clause.Expr{SQL: caseExpr, Vars: args}).Error; err != nil {
		return nil, err
	}
	return mids, nil
}

func buildSlavePlaylistUpdateStamps(sourceID string, changes []playlistChange) (map[int64]int64, error) {
	movieKeys := make([]string, 0, len(changes))
	changeByKey := make(map[string]playlistChange, len(changes))
	for _, change := range changes {
		if strings.TrimSpace(change.MovieKey) == "" {
			continue
		}
		movieKeys = append(movieKeys, change.MovieKey)
		changeByKey[change.MovieKey] = change
	}
	midsByLookupKey := loadMidCandidatesByMatchKeys(movieKeys)
	if len(midsByLookupKey) == 0 {
		return nil, nil
	}

	// 每个 movie_key 只绑定一个最优 mid，避免标点重复片（烬九州：第二季 / 烬九州第二季）共享 key 时双双进更新列表
	midByKey := pickBestMidsByKey(midsByLookupKey)
	if len(midByKey) == 0 {
		return nil, nil
	}

	allMIDs := make([]int64, 0, len(midByKey))
	for _, mid := range midByKey {
		allMIDs = append(allMIDs, mid)
	}
	existingCountsMap, err := loadExistingEpisodeCountsByMIDs(db.Mdb, allMIDs, sourceID)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	result := make(map[int64]int64, len(midByKey))
	for movieKey, mid := range midByKey {
		change := changeByKey[movieKey]
		if !slaveShouldBumpStamp(change, existingCountsMap[mid]) {
			continue
		}
		if existing, ok := result[mid]; !ok || now > existing {
			result[mid] = now
		}
	}
	return result, nil
}

// slaveShouldBumpStamp 附属站是否应顶「最近更新」并记入每日更新。
// 本源须有追集，且 incoming 最大集数严格大于写前全库最大（其它源 + 本源旧集数）。
// playlist 已先落库，otherCounts 不含本源；用 PrevMaxCount 补回写前自己的集数。
func slaveShouldBumpStamp(change playlistChange, otherCounts []int) bool {
	if !change.FirstInsert && !change.NotifyWorthy && !change.CountIncreased {
		return false
	}
	global := make([]int, 0, len(otherCounts)+1)
	global = append(global, otherCounts...)
	if change.PrevMaxCount > 0 {
		global = append(global, change.PrevMaxCount)
	}
	return isEpisodeCountHigher(extractEpisodeCountsFromPlaylistSignatures(change.Signatures), global)
}

// pickBestMidForMatchKey 同一 match_key 命中多个 mid 时只保留一个（update_stamp 新者优先，其次 mid 大）。
// 源站常并存「烬九州：第二季」与「烬九州第二季」两个 vod_id，归一化后共享 match_key。
func pickBestMidForMatchKey(mids []int64) int64 {
	return pickBestMidsByKey(map[string][]int64{"_": mids})["_"]
}

func uniquePositiveMIDs(mids []int64) []int64 {
	uniq := make([]int64, 0, len(mids))
	seen := make(map[int64]struct{}, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		uniq = append(uniq, mid)
	}
	return uniq
}

func pickBestMidsByKey(midsByKey map[string][]int64) map[string]int64 {
	out := make(map[string]int64, len(midsByKey))
	if len(midsByKey) == 0 {
		return out
	}
	needQuery := make([]int64, 0)
	seenQuery := make(map[int64]struct{})
	multiKeys := make([]string, 0)
	for key, mids := range midsByKey {
		uniq := uniquePositiveMIDs(mids)
		if len(uniq) == 0 {
			continue
		}
		if len(uniq) == 1 {
			out[key] = uniq[0]
			continue
		}
		multiKeys = append(multiKeys, key)
		for _, mid := range uniq {
			if _, ok := seenQuery[mid]; ok {
				continue
			}
			seenQuery[mid] = struct{}{}
			needQuery = append(needQuery, mid)
		}
	}
	if len(multiKeys) == 0 {
		return out
	}
	rank := make(map[int64]int, len(needQuery))
	if db.Mdb != nil && len(needQuery) > 0 {
		var rows []model.FilmIndex
		if err := db.Mdb.Select("mid", "update_stamp").Where("mid IN ?", needQuery).Order("update_stamp DESC, mid DESC").Find(&rows).Error; err == nil {
			for i, row := range rows {
				rank[row.Mid] = i
			}
		}
	}
	fallbackRank := len(needQuery) + 1
	for _, key := range multiKeys {
		best := int64(0)
		bestRank := fallbackRank + 1
		for _, mid := range uniquePositiveMIDs(midsByKey[key]) {
			r, ok := rank[mid]
			if !ok {
				r = fallbackRank
			}
			if best == 0 || r < bestRank || (r == bestRank && mid > best) {
				best = mid
				bestRank = r
			}
		}
		if best > 0 {
			out[key] = best
		}
	}
	return out
}

func saveGroupedPlaylists(sourceID string, playlists []model.SlaveMoviePlaylist, keysByMovieKey map[string]struct{}) ([]playlistChange, error) {
	movieKeys := make([]string, 0, len(keysByMovieKey))
	for movieKey := range keysByMovieKey {
		if strings.TrimSpace(movieKey) == "" {
			continue
		}
		movieKeys = append(movieKeys, movieKey)
	}
	sort.Strings(movieKeys)

	if len(playlists) > 0 {
		sort.Slice(playlists, func(i, j int) bool {
			if playlists[i].MovieKey == playlists[j].MovieKey {
				return playlists[i].GroupIndex < playlists[j].GroupIndex
			}
			return playlists[i].MovieKey < playlists[j].MovieKey
		})
		// 同一 (movie_key, group_index) 只保留最后一行：同一影片在源站常有多个条目
		// （如「XXX英语」「XXX国语」共享豆瓣匹配键），都会写入同一 (key, group) 槽位，
		// 落库按唯一键后写覆盖。签名必须与落库语义对齐，否则每次采集都会把
		// 「多条目并存」误判为结构变化，更新列表反复刷同一 mid。
		playlists = dedupePlaylistRows(playlists)
	}

	// 1. 无事务只读比对签名与变更
	existing, err := loadPlaylistSignaturesTx(db.Mdb, sourceID, movieKeys)
	if err != nil {
		return nil, err
	}
	incoming := buildPlaylistSignatures(playlists)
	changes := diffPlaylistMovieKeys(existing, incoming, movieKeys)

	// 2. 无变更短路（No-op Short-circuit）：无任何改动直接返回，避免无意义开启事务
	if len(changes) == 0 {
		return changes, nil
	}

	// 3. 变更行原地 upsert；只按 (source_id, movie_key, group_index) 删除消失的线路。
	// 禁止按 movie_key 整组 DELETE：多源同时写同一主键时会打到 idx_slave_movie_key 上互相堵住。
	changedKeysMap := make(map[string]struct{}, len(changes))
	changedKeys := make([]string, 0, len(changes))
	for _, c := range changes {
		if c.MovieKey != "" {
			if _, ok := changedKeysMap[c.MovieKey]; !ok {
				changedKeysMap[c.MovieKey] = struct{}{}
				changedKeys = append(changedKeys, c.MovieKey)
			}
		}
	}
	sort.Strings(changedKeys)

	changedPlaylists := make([]model.SlaveMoviePlaylist, 0, len(playlists))
	for _, p := range playlists {
		if _, ok := changedKeysMap[p.MovieKey]; ok {
			changedPlaylists = append(changedPlaylists, p)
		}
	}
	vanished := vanishedPlaylistSlots(sourceID, existing, incoming, changedKeys)

	err = db.Mdb.Transaction(func(tx *gorm.DB) error {
		if len(changedPlaylists) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "source_id"}, {Name: "movie_key"}, {Name: "group_index"}},
				DoUpdates: clause.AssignmentColumns([]string{"group_name", "content", "updated_at"}),
			}).CreateInBatches(&changedPlaylists, 500).Error; err != nil {
				return err
			}
		}
		for _, slot := range vanished {
			if err := tx.Unscoped().
				Where("source_id = ? AND movie_key = ? AND group_index = ?", slot.sourceID, slot.movieKey, slot.groupIndex).
				Delete(&model.SlaveMoviePlaylist{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changes, nil
}

type playlistSlot struct {
	sourceID   string
	movieKey   string
	groupIndex int
}

func vanishedPlaylistSlots(sourceID string, existing, incoming map[string][]playlistSignature, changedKeys []string) []playlistSlot {
	if len(changedKeys) == 0 {
		return nil
	}
	out := make([]playlistSlot, 0)
	for _, movieKey := range changedKeys {
		keep := make(map[int]struct{}, len(incoming[movieKey]))
		for _, sig := range incoming[movieKey] {
			keep[sig.GroupIndex] = struct{}{}
		}
		for _, sig := range existing[movieKey] {
			if _, ok := keep[sig.GroupIndex]; ok {
				continue
			}
			out = append(out, playlistSlot{sourceID: sourceID, movieKey: movieKey, groupIndex: sig.GroupIndex})
		}
	}
	return out
}

type playlistChange struct {
	MovieKey       string
	FirstInsert    bool
	NotifyWorthy   bool // 任一线路「最后一项分集标签」有变化（含新增/回退/顺序变化）或首次写入
	CountIncreased bool // 相对本源上次 playlist，最大集数变多（含中间插集、最后一项仍是「完结」）
	PrevMaxCount   int  // 本源写前最大集数；与其它源合计成写前全库最大
	Signatures     []playlistSignature
}

type playlistSignature struct {
	GroupIndex int
	GroupName  string
	Content    string
}

func loadPlaylistSignaturesTx(tx *gorm.DB, sourceID string, movieKeys []string) (map[string][]playlistSignature, error) {
	result := make(map[string][]playlistSignature, len(movieKeys))
	if len(movieKeys) == 0 {
		return result, nil
	}
	var rows []model.SlaveMoviePlaylist
	if err := tx.Where("source_id = ? AND movie_key IN ?", sourceID, movieKeys).
		Order("movie_key ASC, group_index ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.MovieKey] = append(result[row.MovieKey], playlistSignature{
			GroupIndex: row.GroupIndex,
			GroupName:  row.GroupName,
			Content:    row.Content,
		})
	}
	return result, nil
}

func buildPlaylistSignatures(playlists []model.SlaveMoviePlaylist) map[string][]playlistSignature {
	result := make(map[string][]playlistSignature, len(playlists))
	for _, playlist := range playlists {
		result[playlist.MovieKey] = append(result[playlist.MovieKey], playlistSignature{
			GroupIndex: playlist.GroupIndex,
			GroupName:  playlist.GroupName,
			Content:    playlist.Content,
		})
	}
	return result
}

func diffPlaylistMovieKeys(existing map[string][]playlistSignature, incoming map[string][]playlistSignature, movieKeys []string) []playlistChange {
	changed := make([]playlistChange, 0, len(movieKeys))
	for _, movieKey := range movieKeys {
		left, right := existing[movieKey], incoming[movieKey]
		if samePlaylistSignatures(left, right) {
			continue
		}
		first := len(left) == 0
		notifyWorthy := false
		countIncreased := false
		prevMax := maxEpisodeCount(extractEpisodeCountsFromPlaylistSignatures(left))
		if len(right) > 0 {
			countIncreased = isEpisodeCountHigher(
				extractEpisodeCountsFromPlaylistSignatures(right),
				extractEpisodeCountsFromPlaylistSignatures(left),
			)
		}
		switch {
		case len(right) == 0:
			// right 为空 = 该 key 本次未出现（源站改名/条目消失后的残留或陈旧 key）→ 不是内容更新，
			// 不进更新列表，否则改名/条目切换会让同一 mid 每批反复上报。
		case first:
			// 首次写入：确为新增内容；是否顶最近更新见 slaveShouldBumpStamp（还要比主站集数）
			notifyWorthy = true
		default:
			// 任一线路「最后一项分集标签」与库中不同（含新增/回退/顺序变化）→ 进更新列表；
			// 最后一项相同但集数变多（中间插集）也进；仅链接变化不进。
			notifyWorthy = playlistLastEpisodeChanged(left, right)
		}
		changed = append(changed, playlistChange{
			MovieKey:       movieKey,
			FirstInsert:    first,
			NotifyWorthy:   notifyWorthy,
			CountIncreased: countIncreased,
			PrevMaxCount:   prevMax,
			Signatures:     right,
		})
	}
	return changed
}

// playlistGroupKey 线路分组标识（分组序号 + 线路名）。
type playlistGroupKey struct {
	GroupIndex int
	GroupName  string
}

// lastEpisodeLabel 线路的「最后一集」标签：取源站返回顺序的最后一个非空 Episode 原文。
// macCMS 源站按集数/日期顺序返回（剧「第01集…第N集」、综艺「第20240107期…」），
// 最后一项即最新一集；不解析数字，HD/正片等无数字标签同样适用。
func lastEpisodeLabel(links []model.MovieUrlInfo) string {
	last := ""
	for _, u := range links {
		if label := strings.TrimSpace(u.Episode); label != "" {
			last = label
		}
	}
	return last
}

// lastEpisodeByGroup 把线路签名转为「线路 → 最后一集标签」。
func lastEpisodeByGroup(sigs []playlistSignature) map[playlistGroupKey]string {
	out := make(map[playlistGroupKey]string, len(sigs))
	for _, s := range sigs {
		key := playlistGroupKey{GroupIndex: s.GroupIndex, GroupName: strings.TrimSpace(s.GroupName)}
		var links []model.MovieUrlInfo
		if err := json.Unmarshal([]byte(s.Content), &links); err != nil {
			out[key] = ""
			continue
		}
		out[key] = lastEpisodeLabel(links)
	}
	return out
}

// playlistLastEpisodeChanged 任一线路（按 GroupIndex+GroupName 对齐）的「最后一项分集标签」变化 → true。
// 线路新增/消失、任一线路最后一项标签不同（含集数回退/顺序变化）都算变化；仅链接/中间集变化不算。
func playlistLastEpisodeChanged(left, right []playlistSignature) bool {
	leftByGroup := lastEpisodeByGroup(left)
	rightByGroup := lastEpisodeByGroup(right)
	if len(leftByGroup) != len(rightByGroup) {
		return true
	}
	for key, label := range rightByGroup {
		if oldLabel, ok := leftByGroup[key]; !ok || oldLabel != label {
			return true
		}
	}
	return false
}

// dedupePlaylistRows 按 (movie_key, group_index) 去重，保留最后一行。
// 入参需已按 movie_key ASC, group_index ASC 排序；与落库 OnConflict 后写覆盖语义一致。
func dedupePlaylistRows(rows []model.SlaveMoviePlaylist) []model.SlaveMoviePlaylist {
	if len(rows) < 2 {
		return rows
	}
	out := rows[:0]
	for i := 0; i < len(rows); {
		j := i + 1
		for j < len(rows) && rows[j].MovieKey == rows[i].MovieKey && rows[j].GroupIndex == rows[i].GroupIndex {
			j++
		}
		out = append(out, rows[j-1])
		i = j
	}
	return out
}

func samePlaylistSignatures(left []playlistSignature, right []playlistSignature) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].GroupIndex != right[index].GroupIndex {
			return false
		}
		if left[index].GroupName != right[index].GroupName {
			return false
		}
		if normalizePlaylistCompareContent(left[index].Content) != normalizePlaylistCompareContent(right[index].Content) {
			return false
		}
	}
	return true
}

// normalizePlaylistCompareContent 归一化播放列表内容用于「是否需要写库」对比：
// trim 集数、去掉链接 query。仅用于对比，不影响实际保存的播放数据。
func normalizePlaylistCompareContent(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	var links []model.MovieUrlInfo
	if err := json.Unmarshal([]byte(raw), &links); err != nil {
		return raw
	}
	out := make([]model.MovieUrlInfo, len(links))
	for i, u := range links {
		out[i] = model.MovieUrlInfo{
			Episode: strings.TrimSpace(u.Episode),
			Link:    stripURLQuery(strings.TrimSpace(u.Link)),
		}
	}
	data, _ := json.Marshal(out)
	return string(data)
}

// playlistEpisodeLabelSignature 集数标签序列（仅诊断展示用，非更新列表判定）。
func playlistEpisodeLabelSignature(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "[]"
	}
	var links []model.MovieUrlInfo
	if err := json.Unmarshal([]byte(raw), &links); err != nil {
		return raw
	}
	labels := make([]string, len(links))
	for i, u := range links {
		labels[i] = strings.TrimSpace(u.Episode)
	}
	data, _ := json.Marshal(labels)
	return string(data)
}

func DeletePlaylistBySourceId(sourceID string) error {
	return DeletePlaylistBySourceIdTx(db.Mdb, sourceID)
}

func DeletePlaylistBySourceIdTx(tx *gorm.DB, sourceID string) error {
	return tx.Unscoped().Where("source_id = ?", sourceID).Delete(&model.SlaveMoviePlaylist{}).Error
}

// saveSlaveSourceMappings 为附属站播放列表补充 source_mid -> global_mid 映射，
// 让后台单片更新时能够按全局 mid 精确找到每个附属站自己的原始影片 ID。
func saveSlaveSourceMappings(sourceID string, details []model.MovieDetail, infos []model.FilmIndex) error {
	if len(details) == 0 || len(infos) == 0 {
		return nil
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}

	mids := make([]int64, 0, len(infos))
	for _, info := range infos {
		if info.Mid > 0 {
			mids = append(mids, info.Mid)
		}
	}
	if len(mids) == 0 {
		return nil
	}

	globalMidByKey := make(map[string]int64, len(mids)*2)
	keysByMid := loadMovieMatchKeysByMids(mids)
	sortedMids := make([]int64, 0, len(keysByMid))
	for mid := range keysByMid {
		sortedMids = append(sortedMids, mid)
	}
	sort.Slice(sortedMids, func(i, j int) bool {
		return sortedMids[i] > sortedMids[j]
	})
	for _, mid := range sortedMids {
		for _, key := range keysByMid[mid] {
			if strings.TrimSpace(key) == "" {
				continue
			}
			if _, exists := globalMidByKey[key]; !exists {
				globalMidByKey[key] = mid
			}
		}
	}
	if len(globalMidByKey) == 0 {
		return nil
	}

	mappings := make([]model.MovieSourceMapping, 0, len(details))
	for _, detail := range details {
		if detail.Id <= 0 {
			continue
		}
		globalMid, ok := resolveSlaveGlobalMid(detail, globalMidByKey)
		if !ok || globalMid <= 0 {
			continue
		}
		mappings = append(mappings, model.MovieSourceMapping{
			SourceId:  sourceID,
			SourceMid: detail.Id,
			GlobalMid: globalMid,
		})
	}

	return saveMovieSourceMappingsTxE(db.Mdb, mappings)
}

func resolveSlaveGlobalMid(detail model.MovieDetail, globalMidByKey map[string]int64) (int64, bool) {
	for _, key := range BuildPlaylistMovieKeys(detail) {
		globalMid, ok := globalMidByKey[key]
		if ok {
			return globalMid, true
		}
	}
	return 0, false
}

func GetMultiplePlayGroupsByKeys(siteID, siteName string, keys []string) []model.PlayLinkVo {
	return getMultiplePlayGroupsByKeysTx(db.Mdb, siteID, siteName, keys)
}

func GetMultiplePlayGroupsBySourcesAndKeys(sources []model.FilmSource, keys []string) map[string][]model.PlayLinkVo {
	orderedKeys := UniqueKeys(keys)
	if len(sources) == 0 || len(orderedKeys) == 0 {
		return nil
	}

	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		if strings.TrimSpace(source.Id) != "" {
			sourceIDs = append(sourceIDs, source.Id)
		}
	}
	if len(sourceIDs) == 0 {
		return nil
	}

	playlistsBySourceKey, err := loadPlaylistsBySourceAndKeysTx(db.Mdb, sourceIDs, orderedKeys)
	if err != nil {
		return nil
	}
	if len(playlistsBySourceKey) == 0 {
		return nil
	}

	result := make(map[string][]model.PlayLinkVo, len(sources))
	for _, source := range sources {
		groups := buildPlayGroupsFromLoadedPlaylists(source.Id, source.Name, orderedKeys, playlistsBySourceKey)
		if len(groups) > 0 {
			result[source.Id] = groups
		}
	}
	return result
}

func getMultiplePlayGroupsByKeysTx(tx *gorm.DB, siteID, siteName string, keys []string) []model.PlayLinkVo {
	orderedKeys := UniqueKeys(keys)
	if siteID == "" || len(orderedKeys) == 0 {
		return nil
	}

	var playlists []model.SlaveMoviePlaylist
	if err := tx.Where("source_id = ? AND movie_key IN ?", siteID, orderedKeys).
		Order("movie_key ASC").
		Order("group_index ASC").
		Find(&playlists).Error; err != nil {
		return nil
	}
	if len(playlists) == 0 {
		return nil
	}

	playlistByKey := make(map[string][]model.SlaveMoviePlaylist, len(playlists))
	for _, playlist := range playlists {
		playlistByKey[playlist.MovieKey] = append(playlistByKey[playlist.MovieKey], playlist)
	}

	return selectBestPlayGroups(siteID, siteName, orderedKeys, playlistByKey)
}

func loadPlaylistGroupsByInfos(infos []model.FilmIndex) (map[int64]map[string][]model.PlayLinkVo, error) {
	return loadPlaylistGroupsByInfosTx(db.Mdb, infos)
}

func loadPlaylistGroupsByInfosTx(tx *gorm.DB, infos []model.FilmIndex) (map[int64]map[string][]model.PlayLinkVo, error) {
	result := make(map[int64]map[string][]model.PlayLinkVo, len(infos))
	mids := make([]int64, 0, len(infos))
	for _, info := range infos {
		if info.Mid > 0 {
			mids = append(mids, info.Mid)
		}
	}

	keysByMid := loadMovieMatchKeysByMidsTx(tx, mids)
	allKeys := make([]string, 0, len(infos)*4)
	for _, keys := range keysByMid {
		allKeys = append(allKeys, keys...)
	}
	allKeys = UniqueKeys(allKeys)

	sources := make([]model.FilmSource, 0)
	sourceIDs := make([]string, 0)
	for _, source := range support.GetCollectSourceList() {
		if source.Grade != model.SlaveCollect || !source.State {
			continue
		}
		sources = append(sources, source)
		sourceIDs = append(sourceIDs, source.Id)
	}

	playlistsBySourceKey, err := loadPlaylistsBySourceAndKeysTx(tx, sourceIDs, allKeys)
	if err != nil {
		return nil, err
	}
	for _, info := range infos {
		groupsBySource := make(map[string][]model.PlayLinkVo)
		lookupKeys := keysByMid[info.Mid]
		if len(lookupKeys) == 0 || len(playlistsBySourceKey) == 0 {
			result[info.Mid] = groupsBySource
			continue
		}

		for _, source := range sources {
			groups := buildPlayGroupsFromLoadedPlaylists(source.Id, source.Name, lookupKeys, playlistsBySourceKey)
			if len(groups) == 0 {
				continue
			}
			groupsBySource[source.Id] = groups
		}
		result[info.Mid] = groupsBySource
	}
	return result, nil
}

func loadPlaylistsBySourceAndKeysTx(tx *gorm.DB, sourceIDs []string, keys []string) (map[string]map[string][]model.SlaveMoviePlaylist, error) {
	if len(sourceIDs) == 0 || len(keys) == 0 {
		return nil, nil
	}

	var playlists []model.SlaveMoviePlaylist
	if err := tx.Where("source_id IN ? AND movie_key IN ?", sourceIDs, keys).
		Order("source_id ASC").
		Order("movie_key ASC").
		Order("group_index ASC").
		Find(&playlists).Error; err != nil {
		return nil, err
	}

	result := make(map[string]map[string][]model.SlaveMoviePlaylist)
	for _, playlist := range playlists {
		byKey := result[playlist.SourceId]
		if byKey == nil {
			byKey = make(map[string][]model.SlaveMoviePlaylist)
			result[playlist.SourceId] = byKey
		}
		byKey[playlist.MovieKey] = append(byKey[playlist.MovieKey], playlist)
	}
	return result, nil
}

func buildPlayGroupsFromLoadedPlaylists(
	siteID string,
	siteName string,
	keys []string,
	playlistsBySourceKey map[string]map[string][]model.SlaveMoviePlaylist,
) []model.PlayLinkVo {
	byKey := playlistsBySourceKey[siteID]
	if len(byKey) == 0 {
		return nil
	}
	return selectBestPlayGroups(siteID, siteName, keys, byKey)
}

func selectBestPlayGroups(siteID, siteName string, keys []string, byKey map[string][]model.SlaveMoviePlaylist) []model.PlayLinkVo {
	var best []model.PlayLinkVo
	bestCount := -1
	for _, key := range UniqueKeys(keys) {
		groups := playGroupsFromPlaylistRows(siteID, siteName, byKey[key])
		if len(groups) == 0 {
			continue
		}
		count := 0
		for _, group := range groups {
			if n := episodeCount(group.LinkList); n > count {
				count = n
			}
		}
		if count > bestCount {
			best = groups
			bestCount = count
		}
	}
	return best
}

func playGroupsFromPlaylistRows(siteID, siteName string, matched []model.SlaveMoviePlaylist) []model.PlayLinkVo {
	if len(matched) == 0 {
		return nil
	}
	groups := make([]model.PlayLinkVo, 0, len(matched))
	for _, playlist := range matched {
		var links []model.MovieUrlInfo
		if err := json.Unmarshal([]byte(playlist.Content), &links); err != nil || len(links) == 0 {
			continue
		}
		displayName := BuildDisplaySourceName(siteName, playlist.GroupName, playlist.GroupIndex, len(matched))
		groupID := BuildPlayGroupID(siteID, playlist.GroupName, playlist.GroupIndex, len(matched))
		groups = append(groups, model.PlayLinkVo{
			Id:       groupID,
			SourceId: siteID,
			Name:     displayName,
			LinkList: links,
		})
	}
	return groups
}

// LoadSourceMidByGlobalMid 通过全局影片 ID 获取指定站点的原始影片 ID。
// 单片更新全部站点时，主站和附属站都会先经过这里做一次 ID 翻译。
func LoadSourceMidByGlobalMid(globalMid int64, sourceID string) int64 {
	if globalMid <= 0 || strings.TrimSpace(sourceID) == "" {
		return 0
	}

	var mapping model.MovieSourceMapping
	if err := db.Mdb.Where("global_mid = ? AND source_id = ?", globalMid, sourceID).First(&mapping).Error; err != nil {
		return 0
	}
	return mapping.SourceMid
}
