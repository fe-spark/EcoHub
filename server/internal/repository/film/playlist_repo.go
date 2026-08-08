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
	"server/internal/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SaveSitePlayList 写入附属站播放列表，返回「播放源实质变更」对应的全局 mid。
func SaveSitePlayList(sourceID string, list []model.MovieDetail) ([]int64, error) {
	if len(list) == 0 {
		return nil, nil
	}

	var playlists []model.MoviePlaylist
	keysByMovieKey := make(map[string]struct{}, len(list)*2)

	for _, detail := range list {
		if len(detail.PlayList) == 0 || strings.Contains(detail.CName, "解说") {
			continue
		}

		keys := BuildPlaylistMovieKeys(detail)
		for _, movieKey := range keys {
			keysByMovieKey[movieKey] = struct{}{}
		}

		for _, movieKey := range keys {
			for index, links := range detail.PlayList {
				if len(links) == 0 {
					continue
				}

				data, _ := json.Marshal(links)
				rawName := ""
				if index < len(detail.PlayFrom) {
					rawName = strings.TrimSpace(detail.PlayFrom[index])
				}

				playlists = append(playlists, model.MoviePlaylist{
					SourceId:   sourceID,
					MovieKey:   movieKey,
					GroupIndex: index,
					GroupName:  rawName,
					Content:    string(data),
				})
			}
		}
	}

	if len(keysByMovieKey) == 0 {
		return nil, nil
	}

	// 写库前记录「该站是否已为 mid 写过 playlist」，供 FirstInsert 通知降噪
	// （若写库后再查，本批新 key 已被写入，会把真·首次也误判为 key 扩展）
	preHasPlaylist := preflightSourcePlaylistPresence(sourceID, keysByMovieKey)

	changes, err := saveGroupedPlaylists(sourceID, playlists, keysByMovieKey)
	if err != nil {
		log.Printf("SaveSitePlayList Error: %v", err)
		return nil, err
	}
	changedMids, err := scheduleSearchInfoRefreshByPlaylists(sourceID, list, changes, preHasPlaylist)
	if err != nil {
		log.Printf("scheduleSearchInfoRefreshByPlaylists Error: %v", err)
		return nil, err
	}
	// 仅在有播放源实质变更时更新 last_collect_time。
	if len(changes) > 0 {
		repository.NoteCollectSourceStats(sourceID)
	}

	return changedMids, nil
}

// preflightSourcePlaylistPresence 在写入前，按本批 movie_key 可能命中的 mid，查询该附属站是否已有 playlist。
func preflightSourcePlaylistPresence(sourceID string, keysByMovieKey map[string]struct{}) map[int64]bool {
	keys := make([]string, 0, len(keysByMovieKey))
	for k := range keysByMovieKey {
		keys = append(keys, k)
	}
	midsByKey := loadMidCandidatesByMatchKeys(keys)
	mids := make([]int64, 0, len(midsByKey))
	seen := make(map[int64]struct{})
	for _, list := range midsByKey {
		for _, mid := range list {
			if mid <= 0 {
				continue
			}
			if _, ok := seen[mid]; ok {
				continue
			}
			seen[mid] = struct{}{}
			mids = append(mids, mid)
		}
	}
	return sourceHasPlaylistForMIDs(sourceID, mids)
}

// scheduleSearchInfoRefreshByPlaylists 刷新附属站映射/时间戳，并返回有变更的全局 mid。
func scheduleSearchInfoRefreshByPlaylists(sourceID string, details []model.MovieDetail, changes []playlistChange, preHasPlaylist map[int64]bool) ([]int64, error) {
	infos, err := loadMatchedSearchInfosByDetails(details)
	if err != nil {
		return nil, err
	}
	if err := saveSlaveSourceMappings(sourceID, details, infos); err != nil {
		return nil, err
	}
	// 更新列表 mid：仅结构变更 / 真·首次匹配（一对一 mid，见 buildSlavePlaylistUpdateStamps）
	changedMids, err := touchSlavePlaylistUpdateStamps(sourceID, changes, preHasPlaylist)
	if err != nil {
		return nil, err
	}
	// 播放源摘要：仅对本批有 playlist 写入的 mid 刷新，避免每次把页内全部匹配片（数十上百）刷进 finalizer
	if refreshMIDs := slavePlaylistAffectedMIDs(changes); len(refreshMIDs) > 0 {
		refreshInfos := filterFilmIndexesByMIDs(infos, refreshMIDs)
		if len(refreshInfos) == 0 {
			// infos 可能因匹配策略未包含某 mid：按 mid 回表
			var rows []model.FilmIndex
			if err := db.Mdb.Where("mid IN ?", refreshMIDs).Find(&rows).Error; err == nil {
				refreshInfos = rows
			}
		}
		SchedulePlaySummaryRefresh(refreshInfos...)
	}
	return changedMids, nil
}

func filterFilmIndexesByMIDs(infos []model.FilmIndex, mids []int64) []model.FilmIndex {
	if len(infos) == 0 || len(mids) == 0 {
		return nil
	}
	want := make(map[int64]struct{}, len(mids))
	for _, mid := range mids {
		if mid > 0 {
			want[mid] = struct{}{}
		}
	}
	out := make([]model.FilmIndex, 0, len(want))
	for _, info := range infos {
		if _, ok := want[info.Mid]; ok {
			out = append(out, info)
		}
	}
	return out
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
	seen := make(map[int64]struct{})
	out := make([]int64, 0, len(changes))
	for _, c := range changes {
		mid := pickBestMidForMatchKey(midsByKey[c.MovieKey])
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
		matched := make(map[int64]struct{}, 2)
		for _, key := range item.keys {
			candidateMids := midsByLookupKey[key]
			if len(candidateMids) == 0 {
				continue
			}
			for _, mid := range candidateMids {
				matched[mid] = struct{}{}
			}
			break
		}

		for mid := range matched {
			if _, ok := seenMid[mid]; ok {
				continue
			}
			seenMid[mid] = struct{}{}
			ordered = append(ordered, infoByMid[mid])
		}
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

// touchSlavePlaylistUpdateStamps 刷新「剧集结构变更」影片的 update_stamp，返回这些全局 mid。
// 仅链接签名变化的保存已在 saveGroupedPlaylists 完成，不抬 stamp、不进更新列表。
// preHasPlaylist 必须在写库前计算：同一附属站已有 playlist 的 mid，其 FirstInsert 仅表示新 key 扩展。
func touchSlavePlaylistUpdateStamps(sourceID string, changes []playlistChange, preHasPlaylist map[int64]bool) ([]int64, error) {
	_ = sourceID
	notifyChanges := make([]playlistChange, 0, len(changes))
	for _, c := range changes {
		if c.NotifyWorthy {
			notifyChanges = append(notifyChanges, c)
		}
	}
	updateStampByMid, err := buildSlavePlaylistUpdateStamps(notifyChanges, preHasPlaylist)
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

func buildSlavePlaylistUpdateStamps(changes []playlistChange, preHasPlaylist map[int64]bool) (map[int64]int64, error) {
	if preHasPlaylist == nil {
		preHasPlaylist = map[int64]bool{}
	}
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
	midByKey := make(map[string]int64, len(midsByLookupKey))
	for key, mids := range midsByLookupKey {
		mid := pickBestMidForMatchKey(mids)
		if mid <= 0 {
			continue
		}
		midByKey[key] = mid
	}
	if len(midByKey) == 0 {
		return nil, nil
	}

	firstInsertMIDs := make([]int64, 0, len(changes))
	for movieKey, mid := range midByKey {
		change := changeByKey[movieKey]
		if !change.FirstInsert {
			continue
		}
		// 写库前已有该站 playlist：只是多写了一个 match key，不按「首次上线」通知
		if preHasPlaylist[mid] {
			continue
		}
		firstInsertMIDs = append(firstInsertMIDs, mid)
	}
	masterUpdateStampByMid, err := loadMasterUpdateStampsByMids(firstInsertMIDs)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	result := make(map[int64]int64, len(midByKey))
	for movieKey, mid := range midByKey {
		change := changeByKey[movieKey]
		// FirstInsert 但 mid 写库前已有 playlist：不进更新列表（不写 stamp）
		if change.FirstInsert && preHasPlaylist[mid] {
			continue
		}
		// 非 FirstInsert 的 NotifyWorthy 已是结构变更；FirstInsert 真首次则抬 stamp
		updateStamp := now
		if change.FirstInsert {
			updateStamp = masterUpdateStampByMid[mid]
			if updateStamp <= 0 {
				continue
			}
		}
		if existing, ok := result[mid]; !ok || updateStamp > existing {
			result[mid] = updateStamp
		}
	}
	return result, nil
}

// pickBestMidForMatchKey 同一 match_key 命中多个 mid 时只保留一个（update_stamp 新者优先，其次 mid 大）。
// 源站常并存「烬九州：第二季」与「烬九州第二季」两个 vod_id，归一化后共享 match_key。
func pickBestMidForMatchKey(mids []int64) int64 {
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
	if len(uniq) == 0 {
		return 0
	}
	if len(uniq) == 1 {
		return uniq[0]
	}
	var rows []model.FilmIndex
	if err := db.Mdb.Select("mid", "update_stamp").Where("mid IN ?", uniq).Find(&rows).Error; err != nil || len(rows) == 0 {
		// 回退：取最大 mid（通常更新）
		best := uniq[0]
		for _, mid := range uniq[1:] {
			if mid > best {
				best = mid
			}
		}
		return best
	}
	best := rows[0]
	for _, row := range rows[1:] {
		if row.UpdateStamp > best.UpdateStamp || (row.UpdateStamp == best.UpdateStamp && row.Mid > best.Mid) {
			best = row
		}
	}
	return best.Mid
}

// sourceHasPlaylistForMIDs 判断附属站是否已通过任一 match_key 为这些 mid 写过 playlist。
func sourceHasPlaylistForMIDs(sourceID string, mids []int64) map[int64]bool {
	out := make(map[int64]bool, len(mids))
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || len(mids) == 0 || db.Mdb == nil {
		return out
	}
	keysByMid := loadMovieMatchKeysByMids(mids)
	allKeys := make([]string, 0, len(mids)*2)
	keyToMids := make(map[string][]int64)
	for mid, keys := range keysByMid {
		for _, k := range keys {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			allKeys = append(allKeys, k)
			keyToMids[k] = append(keyToMids[k], mid)
		}
	}
	allKeys = UniqueKeys(allKeys)
	if len(allKeys) == 0 {
		return out
	}
	var existingKeys []string
	if err := db.Mdb.Model(&model.MoviePlaylist{}).
		Where("source_id = ? AND movie_key IN ?", sourceID, allKeys).
		Distinct().
		Pluck("movie_key", &existingKeys).Error; err != nil {
		return out
	}
	for _, k := range existingKeys {
		for _, mid := range keyToMids[k] {
			out[mid] = true
		}
	}
	return out
}

func loadMasterUpdateStampsByMids(mids []int64) (map[int64]int64, error) {
	result := make(map[int64]int64, len(mids))
	detailsByMid, err := loadMovieDetailsByMidsTx(db.Mdb, mids)
	if err != nil {
		return nil, err
	}
	for mid, detail := range detailsByMid {
		updateStamp, err := utils.ParseCollectUpdateTime(detail.UpdateTime)
		if err != nil {
			return nil, err
		}
		result[mid] = updateStamp
	}
	return result, nil
}

func saveGroupedPlaylists(sourceID string, playlists []model.MoviePlaylist, keysByMovieKey map[string]struct{}) ([]playlistChange, error) {
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

	var changes []playlistChange
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		existing, err := loadPlaylistSignaturesTx(tx, sourceID, movieKeys)
		if err != nil {
			return err
		}
		incoming := buildPlaylistSignatures(playlists)
		changes = diffPlaylistMovieKeys(existing, incoming, movieKeys)

		// 集数回退的 key 不写库（保护已存最大集数，避免源站抖动反复刷新）：
		// 正在更新的连载剧源站/CDN 常在不同请求返回不同集数（16↔18），
		// 若每次都按「最新抓到」覆盖，DB 会在两个版本间震荡，更新列表反复刷同一 mid。
		skipWrite := make(map[string]bool, len(changes))
		for _, c := range changes {
			if c.SkipWrite {
				skipWrite[c.MovieKey] = true
			}
		}
		writeKeys := movieKeys[:0]
		for _, k := range movieKeys {
			if !skipWrite[k] {
				writeKeys = append(writeKeys, k)
			}
		}

		if len(writeKeys) > 0 {
			if err := tx.Unscoped().
				Where("source_id = ? AND movie_key IN ?", sourceID, writeKeys).
				Delete(&model.MoviePlaylist{}).Error; err != nil {
				return err
			}
		}

		if len(playlists) > 0 {
			writePlaylists := playlists[:0]
			for _, p := range playlists {
				if !skipWrite[p.MovieKey] {
					writePlaylists = append(writePlaylists, p)
				}
			}
			if len(writePlaylists) > 0 {
				if err := tx.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "source_id"}, {Name: "movie_key"}, {Name: "group_index"}},
					DoUpdates: clause.AssignmentColumns([]string{"group_name", "content", "updated_at", "deleted_at"}),
				}).Create(&writePlaylists).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changes, nil
}

type playlistChange struct {
	MovieKey     string
	FirstInsert  bool
	NotifyWorthy bool // 新增集数/新线路/首次写入才进更新列表；仅链接、顺序、集数回退为 false
	SkipWrite    bool // 集数回退（源站抖动/下架）：不覆盖已存内容，避免 DB 震荡导致反复上报
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
	var rows []model.MoviePlaylist
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

func buildPlaylistSignatures(playlists []model.MoviePlaylist) map[string][]playlistSignature {
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
		skipWrite := false
		switch {
		case len(right) == 0:
			// right 为空 = 该 key 本次未出现（源站改名/条目消失后的残留或陈旧 key）→ 不是内容更新，
			// 不进更新列表，否则改名/条目切换会让同一 mid 每批反复上报。
		case first:
			// 首次写入：确为新增内容
			notifyWorthy = true
		default:
			// 仅「新增集数/新增线路」进更新列表：
			// 集数相同（仅链接/顺序变化）不通知；集数减少（源站抖动或下架）不通知且不写库，
			// 保护已存最大集数，避免 DB 在两个版本间震荡、更新列表反复刷同一 mid。
			grew := playlistStructureGrew(left, right)
			if grew {
				notifyWorthy = true
			} else if !playlistStructureSameEpisodes(left, right) {
				skipWrite = true
			}
		}
		changed = append(changed, playlistChange{
			MovieKey:     movieKey,
			FirstInsert:  first,
			NotifyWorthy: notifyWorthy,
			SkipWrite:    skipWrite,
		})
	}
	return changed
}

// playlistGroupKey 线路分组标识（分组序号 + 线路名）。
type playlistGroupKey struct {
	GroupIndex int
	GroupName  string
}

// playlistStructureGrew 判断 incoming 相对 existing 是否「新增集数/新增线路」：
// 按 (GroupIndex, GroupName) 对齐，集数标签做无序多重集比较。
// 任一组新增集数或出现新线路 → true；任一组集数减少、线路消失或内容不同 → false（视为抖动/回退）。
func playlistStructureGrew(left, right []playlistSignature) bool {
	leftByGroup, rightByGroup := groupPlaylistSignatures(left), groupPlaylistSignatures(right)
	grew := false
	for key, rightLabels := range rightByGroup {
		leftLabels, ok := leftByGroup[key]
		if !ok {
			grew = true // 新增线路
			continue
		}
		lm, rm := episodeLabelMultiset(leftLabels), episodeLabelMultiset(rightLabels)
		if !multisetSuperset(rm, lm) {
			return false // 该组集数减少/内容不同 → 回退，不视为新增
		}
		if len(rm) > len(lm) {
			grew = true
		}
	}
	for key := range leftByGroup {
		if _, ok := rightByGroup[key]; !ok {
			return false // 线路消失 → 回退
		}
	}
	return grew
}

// playlistStructureSameEpisodes 判断两侧集数多重集是否完全一致（忽略顺序与链接）。
func playlistStructureSameEpisodes(left, right []playlistSignature) bool {
	leftByGroup, rightByGroup := groupPlaylistSignatures(left), groupPlaylistSignatures(right)
	if len(leftByGroup) != len(rightByGroup) {
		return false
	}
	for key, leftLabels := range leftByGroup {
		rightLabels, ok := rightByGroup[key]
		if !ok {
			return false
		}
		lm, rm := episodeLabelMultiset(leftLabels), episodeLabelMultiset(rightLabels)
		if len(lm) != len(rm) || !multisetSuperset(lm, rm) {
			return false
		}
	}
	return true
}

func groupPlaylistSignatures(sigs []playlistSignature) map[playlistGroupKey][]string {
	out := make(map[playlistGroupKey][]string, len(sigs))
	for _, s := range sigs {
		key := playlistGroupKey{GroupIndex: s.GroupIndex, GroupName: strings.TrimSpace(s.GroupName)}
		var links []model.MovieUrlInfo
		if err := json.Unmarshal([]byte(s.Content), &links); err != nil {
			continue
		}
		for _, u := range links {
			if label := strings.TrimSpace(u.Episode); label != "" {
				out[key] = append(out[key], label)
			}
		}
	}
	return out
}

// episodeLabelMultiset 集数标签多重集（无序、保留重复）。
func episodeLabelMultiset(labels []string) map[string]int {
	m := make(map[string]int, len(labels))
	for _, l := range labels {
		m[l]++
	}
	return m
}

// multisetSuperset a ⊇ b：a 中每个标签出现次数 ≥ b。
func multisetSuperset(a, b map[string]int) bool {
	for label, n := range b {
		if a[label] < n {
			return false
		}
	}
	return true
}

// dedupePlaylistRows 按 (movie_key, group_index) 去重，保留最后一行。
// 入参需已按 movie_key ASC, group_index ASC 排序；与落库 OnConflict 后写覆盖语义一致。
func dedupePlaylistRows(rows []model.MoviePlaylist) []model.MoviePlaylist {
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

// samePlaylistEpisodeStructure 仅比线路与集数标签，忽略播放链接（防盗链/CDN 签名噪声）。
func samePlaylistEpisodeStructure(left []playlistSignature, right []playlistSignature) bool {
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
		if playlistEpisodeLabelSignature(left[index].Content) != playlistEpisodeLabelSignature(right[index].Content) {
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

// playlistEpisodeLabelSignature 仅集数标签序列，用于更新列表判定。
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
	return tx.Where("source_id = ?", sourceID).Delete(&model.MoviePlaylist{}).Error
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
	for mid, keys := range keysByMid {
		for _, key := range keys {
			if strings.TrimSpace(key) == "" {
				continue
			}
			globalMidByKey[key] = mid
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

	var playlists []model.MoviePlaylist
	if err := tx.Where("source_id = ? AND movie_key IN ?", siteID, orderedKeys).
		Order("movie_key ASC").
		Order("group_index ASC").
		Find(&playlists).Error; err != nil {
		return nil
	}
	if len(playlists) == 0 {
		return nil
	}

	playlistByKey := make(map[string][]model.MoviePlaylist, len(playlists))
	for _, playlist := range playlists {
		playlistByKey[playlist.MovieKey] = append(playlistByKey[playlist.MovieKey], playlist)
	}

	for _, key := range orderedKeys {
		matched, ok := playlistByKey[key]
		if !ok {
			continue
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
		if len(groups) > 0 {
			return groups
		}
	}

	return nil
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

func loadPlaylistsBySourceAndKeysTx(tx *gorm.DB, sourceIDs []string, keys []string) (map[string]map[string][]model.MoviePlaylist, error) {
	if len(sourceIDs) == 0 || len(keys) == 0 {
		return nil, nil
	}

	var playlists []model.MoviePlaylist
	if err := tx.Where("source_id IN ? AND movie_key IN ?", sourceIDs, keys).
		Order("source_id ASC").
		Order("movie_key ASC").
		Order("group_index ASC").
		Find(&playlists).Error; err != nil {
		return nil, err
	}

	result := make(map[string]map[string][]model.MoviePlaylist)
	for _, playlist := range playlists {
		byKey := result[playlist.SourceId]
		if byKey == nil {
			byKey = make(map[string][]model.MoviePlaylist)
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
	playlistsBySourceKey map[string]map[string][]model.MoviePlaylist,
) []model.PlayLinkVo {
	byKey := playlistsBySourceKey[siteID]
	if len(byKey) == 0 {
		return nil
	}
	for _, key := range UniqueKeys(keys) {
		matched := byKey[key]
		if len(matched) == 0 {
			continue
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
		if len(groups) > 0 {
			return groups
		}
	}
	return nil
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
