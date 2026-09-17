package playlist

import (
	"log"
	"sort"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	poster "server/internal/repository/film/poster"
	"server/internal/repository/film/shared"
	"server/internal/repository/film/snapshot"

	"gorm.io/gorm/clause"
)

// scheduleSearchInfoRefreshByPlaylists 刷新附属站映射/时间戳。
// NotifyMIDs 仅 stamp 资格；AffectedMIDs 含所有 playlist 写入，供详情页展示最新集。
func scheduleSearchInfoRefreshByPlaylists(
	sourceID string,
	details []model.MovieDetail,
	changes []playlistChange,
	infos []model.FilmIndex,
	keysByMid map[int64][]string,
) (shared.CollectWriteResult, error) {
	var out shared.CollectWriteResult
	if len(infos) == 0 {
		var err error
		infos, err = loadMatchedSearchInfosByDetails(details)
		if err != nil {
			return out, err
		}
	}
	if err := saveSlaveSourceMappingsWithKeys(sourceID, details, infos, keysByMid); err != nil {
		return out, err
	}
	// 附属站海报同步：若当前源开启了 IsPosterSource，将高清海报同步写入主站影片并加入刷新列表
	posterUpdatedMids, err := poster.SyncSlavePostersIfConfiguredTx(db.Mdb, sourceID, details, infos)
	if err != nil {
		log.Printf("poster.SyncSlavePostersIfConfiguredTx Error: %v", err)
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
		snapshot.SchedulePlaySummaryRefresh(refreshInfos...)
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
	midsByKey := shared.LoadMidCandidatesByMatchKeys(keys)
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
	midsByLookupKey := shared.LoadMidCandidatesByMatchKeys(movieKeys)
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
	existingCountsMap, err := shared.LoadExistingEpisodeCountsByMIDs(db.Mdb, allMIDs, sourceID)
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
	return shared.IsEpisodeCountHigher(shared.ExtractEpisodeCountsFromContents(PlaylistSignatureContents(change.Signatures)), global)
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

func saveSlaveSourceMappingsWithKeys(sourceID string, details []model.MovieDetail, infos []model.FilmIndex, keysByMid map[int64][]string) error {
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
	if keysByMid == nil {
		keysByMid = shared.LoadMovieMatchKeysByMids(mids)
	}
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
		globalMid, ok := shared.ResolveSlaveGlobalMid(detail, globalMidByKey)
		if !ok || globalMid <= 0 {
			continue
		}
		mappings = append(mappings, model.MovieSourceMapping{
			SourceId:  sourceID,
			SourceMid: detail.Id,
			GlobalMid: globalMid,
		})
	}
	if len(mappings) == 0 {
		return nil
	}

	return shared.SaveMovieSourceMappingsTxE(db.Mdb, mappings)
}
