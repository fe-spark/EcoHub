package playlist

import (
	"server/internal/infra/db"
	"server/internal/model"
	shared "server/internal/repository/film/shared"
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
	return support.ResolveRootCategoryIDByCName(info.CName)
}

func collectUniqueMidsFromKeys(keys []string, midsByLookupKey map[string][]int64) []int64 {
	out := make([]int64, 0)
	seen := make(map[int64]struct{})
	for _, key := range keys {
		for _, mid := range uniquePositiveMIDs(midsByLookupKey[key]) {
			if _, ok := seen[mid]; ok {
				continue
			}
			seen[mid] = struct{}{}
			out = append(out, mid)
		}
	}
	return out
}

// pickUniqueSlaveMid 用匹配键召回候选，再用身份打分决定绑定哪部主站影片。
// 只有一部时保持宽松绑定（副站分类标错也能挂上）。
// 同名多部时按豆瓣/名称/类别/标签/年份/备注形态打分，分差不够则不绑定。
func pickUniqueSlaveMid(
	detail model.MovieDetail,
	keys []string,
	midsByLookupKey map[string][]int64,
	infoByMid map[int64]model.FilmIndex,
) int64 {
	cands := collectUniqueMidsFromKeys(keys, midsByLookupKey)
	if len(cands) == 0 {
		return 0
	}
	profiles := make(map[int64]shared.IdentityProfile, len(cands))
	for _, mid := range cands {
		info, ok := infoByMid[mid]
		if !ok {
			continue
		}
		profiles[mid] = shared.IdentityFromFilmIndex(info)
	}
	return shared.PickUniqueIdentityMid(shared.IdentityFromMovieDetail(detail), profiles)
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
		keys := shared.BuildPlaylistMovieKeys(detail)
		keysPerDetail[i] = keys
		allKeys = append(allKeys, keys...)
	}
	midsByLookupKey := shared.LoadMidCandidatesByMatchKeys(allKeys)
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
	keysByMid := shared.LoadMovieMatchKeysByMids(matchedMids)
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
		mid := pickUniqueSlaveMid(detail, keysPerDetail[i], midsByLookupKey, infoByMid)
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
		if shared.ResolveMovieDetailRootPid(detail) != 0 {
			continue
		}
		uncategorizedKeys = append(uncategorizedKeys, shared.BuildPlaylistMovieKeys(detail)...)
	}
	if len(uncategorizedKeys) == 0 {
		return nil
	}
	midsByLookupKey := shared.LoadMidCandidatesByMatchKeys(uncategorizedKeys)
	candidateMids := make([]int64, 0)
	for _, mids := range midsByLookupKey {
		candidateMids = append(candidateMids, mids...)
	}
	keysByMid := shared.LoadMovieMatchKeysByMids(candidateMids)
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
		lookupKeys := shared.BuildPlaylistMovieKeys(detail)
		if len(lookupKeys) == 0 {
			continue
		}
		lookups = append(lookups, detailLookup{detail: detail, keys: lookupKeys})
		allKeys = append(allKeys, lookupKeys...)
	}

	if len(lookups) == 0 {
		return nil, nil
	}

	midsByLookupKey := shared.LoadMidCandidatesByMatchKeys(allKeys)
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
		mid := pickUniqueSlaveMid(item.detail, item.keys, midsByLookupKey, infoByMid)
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
