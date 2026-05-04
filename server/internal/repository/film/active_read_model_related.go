package film

import (
	"sort"
	"strings"

	"server/internal/model"
	"server/internal/model/dto"
)

func ListRelatedSnapshotsReadModel(version string, snapshot model.FilmListSnapshot, page *dto.Page) []model.FilmListSnapshot {
	page = ensurePage(page)
	readModel := requireActiveFilmReadModel(version)
	tags := splitClassTags(snapshot.ClassTag)
	if len(tags) == 0 {
		return []model.FilmListSnapshot{}
	}

	candidateSet := make(map[int64]struct{})
	baseSet := midsToSet(readModel.baseMIDs(snapshot.Pid))
	for _, tag := range tags {
		for _, mid := range readModel.ByTag[readModelTagKey("Plot", tag)] {
			if mid == snapshot.Mid {
				continue
			}
			if _, ok := baseSet[mid]; ok {
				candidateSet[mid] = struct{}{}
			}
		}
	}

	snapshots := make([]model.FilmListSnapshot, 0, len(candidateSet))
	for mid := range candidateSet {
		candidate, ok := readModel.ByMid[mid]
		if !ok {
			continue
		}
		if snapshot.Cid > 0 && candidate.Cid != snapshot.Cid {
			continue
		}
		snapshots = append(snapshots, candidate)
	}
	sortRelatedSnapshots(snapshots, tags)
	page.Total = len(snapshots)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}
	return pageSnapshots(snapshots, page)
}

func sortRelatedSnapshots(snapshots []model.FilmListSnapshot, tags []string) {
	tagSet := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tagSet[strings.TrimSpace(tag)] = struct{}{}
	}
	scoreOf := func(snapshot model.FilmListSnapshot) int {
		score := 0
		for _, tag := range splitClassTags(snapshot.ClassTag) {
			if _, ok := tagSet[tag]; ok {
				score++
			}
		}
		return score
	}
	sort.SliceStable(snapshots, func(i, j int) bool {
		leftScore := scoreOf(snapshots[i])
		rightScore := scoreOf(snapshots[j])
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if snapshots[i].UpdateStamp != snapshots[j].UpdateStamp {
			return snapshots[i].UpdateStamp > snapshots[j].UpdateStamp
		}
		return snapshots[i].Mid > snapshots[j].Mid
	})
}
