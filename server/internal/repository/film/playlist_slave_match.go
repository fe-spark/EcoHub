package film

import (
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
)

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

// matchSlaveDetailMids 按同一套键给附属站详情找唯一主站 mid，并顺带输出匹配到的主站影片与键映射。
func matchSlaveDetailMids(list []model.MovieDetail) ([]int64, map[int64]string, []model.FilmIndex, map[int64][]string, error) {
	detailMids := make([]int64, len(list))
	if len(list) == 0 {
		return detailMids, nil, nil, nil, nil
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
		return detailMids, nil, nil, nil, nil
	}
	matchedMids := make([]int64, 0, len(midSet))
	for mid := range midSet {
		matchedMids = append(matchedMids, mid)
	}

	var candidates []model.FilmIndex
	if err := db.Mdb.Where("mid IN ?", matchedMids).Find(&candidates).Error; err != nil {
		return nil, nil, nil, nil, err
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

	matchedInfos := make([]model.FilmIndex, 0, len(candidates))
	seenMid := make(map[int64]struct{}, len(candidates))
	for i, detail := range list {
		if !isPlaylistWritableDetail(detail) {
			continue
		}
		mid := pickUniqueSlaveMid(keysPerDetail[i], ResolveMovieDetailRootPid(detail), midsByLookupKey, infoByMid)
		if mid > 0 && primaryKeyByMid[mid] != "" {
			detailMids[i] = mid
			if _, seen := seenMid[mid]; !seen {
				seenMid[mid] = struct{}{}
				if info, ok := infoByMid[mid]; ok {
					matchedInfos = append(matchedInfos, info)
				}
			}
		}
	}
	return detailMids, primaryKeyByMid, matchedInfos, keysByMid, nil
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
