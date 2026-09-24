package service

import (
	"strings"

	"server/internal/model"
	filmshared "server/internal/repository/film/shared"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/support"
)

var loadCMSSearchIdentitySnaps = loadCMSSearchIdentitySnapsByMatchKeys

func resolveCMSLocalCards(source *model.FilmSource, sourceMids []int64) (map[int64]int64, map[int64]model.FilmListSnapshot) {
	out := make(map[int64]int64, len(sourceMids))
	snaps := make(map[int64]model.FilmListSnapshot, len(sourceMids))
	if source == nil || len(sourceMids) == 0 {
		return out, snaps
	}
	version := filmsnapshot.GetActiveReadModelVersion()
	lookup := sourceMids
	mapped := map[int64]int64{}
	if source.Grade == model.MasterCollect {
		for _, id := range sourceMids {
			if id > 0 {
				mapped[id] = id
			}
		}
	} else {
		mapped = filmshared.LoadGlobalMidsBySourceMids(source.Id, sourceMids)
		lookup = make([]int64, 0, len(mapped))
		for _, mid := range mapped {
			lookup = append(lookup, mid)
		}
	}
	if len(lookup) == 0 {
		return out, snaps
	}
	for _, snap := range filmsnapshot.GetSnapshotsByMidsOrdered(version, lookup) {
		if snap.Mid > 0 {
			snaps[snap.Mid] = snap
		}
	}
	for sourceMid, mid := range mapped {
		if _, ok := snaps[mid]; ok {
			out[sourceMid] = mid
		}
	}
	return out, snaps
}

func loadCMSSearchIdentitySnapsByMatchKeys(cards []model.MovieBasicInfo, detailsByID map[int64]model.MovieDetail) map[int64]model.FilmListSnapshot {
	out := make(map[int64]model.FilmListSnapshot)
	if len(cards) == 0 {
		return out
	}
	allKeys := make([]string, 0, len(cards)*3)
	for i := range cards {
		allKeys = append(allKeys, cmsSearchMatchKeys(cards[i], detailsByID[cards[i].SourceMid])...)
	}
	midsByKey := filmshared.LoadMidCandidatesByMatchKeys(allKeys)
	mids := make([]int64, 0)
	seen := make(map[int64]struct{})
	for _, hits := range midsByKey {
		for _, mid := range hits {
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
	if len(mids) == 0 {
		return out
	}
	version := filmsnapshot.GetActiveReadModelVersion()
	for _, snap := range filmsnapshot.GetSnapshotsByMidsOrdered(version, mids) {
		if snap.Mid > 0 {
			out[snap.Mid] = snap
		}
	}
	return out
}

func cmsSearchMatchKeys(card model.MovieBasicInfo, detail model.MovieDetail) []string {
	slave := identityFromCMSSearchCard(card, detail)
	return filmshared.BuildMovieMatchKeysWithCategory(slave.DbID, slave.Name, slave.RootPid)
}

func assignCMSSearchLocalIDs(
	cards []model.MovieBasicInfo,
	localBySourceMid map[int64]int64,
	snaps map[int64]model.FilmListSnapshot,
	detailsByID map[int64]model.MovieDetail,
	identitySnaps map[int64]model.FilmListSnapshot,
) {
	if len(cards) == 0 {
		return
	}
	hits := make([]int64, len(cards))
	mappingClaimed := make(map[int64]int, len(cards))
	for i := range cards {
		detail := detailsByID[cards[i].SourceMid]
		mid := localBySourceMid[cards[i].SourceMid]
		snap, ok := snaps[mid]
		if mid > 0 && ok && cmsSearchCardMatchesLocal(cards[i], detail, snap) {
			hits[i] = mid
			mappingClaimed[mid]++
		}
	}
	reserved := make(map[int64]struct{}, len(mappingClaimed))
	for mid, n := range mappingClaimed {
		if n == 1 {
			reserved[mid] = struct{}{}
		}
	}
	for i := range cards {
		if mid := hits[i]; mid > 0 && mappingClaimed[mid] != 1 {
			hits[i] = 0
		}
	}

	fallbackClaimed := make(map[int64]int, len(cards))
	for i := range cards {
		if hits[i] > 0 {
			continue
		}
		rejected := localBySourceMid[cards[i].SourceMid]
		pool := cmsSearchFallbackPool(identitySnaps, reserved, rejected)
		picked := filmshared.PickUniqueIdentityMid(
			identityFromCMSSearchCard(cards[i], detailsByID[cards[i].SourceMid]),
			pool,
		)
		if picked <= 0 {
			continue
		}
		if _, taken := reserved[picked]; taken {
			continue
		}
		hits[i] = picked
		fallbackClaimed[picked]++
	}

	for i := range cards {
		mid := hits[i]
		switch {
		case mid <= 0:
			cards[i].Id = 0
		case mappingClaimed[mid] == 1:
			cards[i].Id = mid
		case fallbackClaimed[mid] == 1:
			cards[i].Id = mid
		default:
			cards[i].Id = 0
		}
	}
}

func cmsSearchFallbackPool(
	identitySnaps map[int64]model.FilmListSnapshot,
	reserved map[int64]struct{},
	rejected int64,
) map[int64]filmshared.IdentityProfile {
	pool := make(map[int64]filmshared.IdentityProfile, len(identitySnaps))
	for mid, snap := range identitySnaps {
		if mid <= 0 || mid == rejected {
			continue
		}
		if _, taken := reserved[mid]; taken {
			continue
		}
		pool[mid] = filmshared.IdentityFromSnapshot(snap)
	}
	return pool
}

func cmsSearchCardMatchesLocal(card model.MovieBasicInfo, detail model.MovieDetail, snap model.FilmListSnapshot) bool {
	slave := identityFromCMSSearchCard(card, detail)
	master := filmshared.IdentityFromSnapshot(snap)
	if !filmshared.CompatibleIdentity(master, slave) || !filmshared.CompatibleWorkShape(master, slave) {
		return false
	}
	if slave.RootPid > 0 && master.RootPid > 0 && slave.RootPid != master.RootPid {
		return false
	}
	return true
}

func identityFromCMSSearchCard(card model.MovieBasicInfo, detail model.MovieDetail) filmshared.IdentityProfile {
	slave := filmshared.IdentityFromMovieDetail(detail)
	if name := strings.TrimSpace(card.Name); name != "" {
		slave.Name = name
	}
	if cname := strings.TrimSpace(card.CName); cname != "" {
		slave.CName = cname
		if root := support.ResolveRootCategoryIDByCName(cname); root > 0 {
			slave.RootPid = root
		}
	}
	if remarks := strings.TrimSpace(card.Remarks); remarks != "" {
		slave.Remarks = remarks
	}
	if year := filmshared.ParseIdentityYear(card.Year); year > 0 {
		slave.Year = year
	}
	if director := strings.TrimSpace(card.Director); director != "" {
		slave.Director = director
	}
	if classTag := strings.TrimSpace(card.ClassTag); classTag != "" {
		slave.ClassTag = classTag
	}
	return slave
}
