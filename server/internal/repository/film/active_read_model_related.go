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
	snapshot = projectSnapshotCategory(snapshot)
	if !isVisibleProjectedSnapshot(snapshot) {
		return []model.FilmListSnapshot{}
	}
	tags := splitClassTags(snapshot.ClassTag)
	if len(tags) == 0 {
		return readModel.relatedByCategory(snapshot, page)
	}

	candidateSet := make(map[int64]struct{})
	for _, candidate := range readModel.projectedSnapshotsByPid(snapshot.Pid) {
		if candidate.Mid == snapshot.Mid {
			continue
		}
		if snapshot.Cid > 0 && candidate.Cid != snapshot.Cid {
			continue
		}
		for _, tag := range tags {
			for _, candidateTag := range splitClassTags(candidate.ClassTag) {
				if candidateTag == tag {
					candidateSet[candidate.Mid] = struct{}{}
				}
			}
		}
	}

	snapshots := make([]model.FilmListSnapshot, 0, len(candidateSet))
	for mid := range candidateSet {
		candidate, ok := readModel.projectedSnapshotByMID(mid)
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

func (m *FilmReadModel) relatedByCategory(snapshot model.FilmListSnapshot, page *dto.Page) []model.FilmListSnapshot {
	projected := ensureProjectedFilmReadModel(m)
	mids := projected.ByPid[snapshot.Pid]
	if snapshot.Pid <= 0 {
		mids = projected.AllMIDs
	}
	snapshots := make([]model.FilmListSnapshot, 0, len(mids))
	for _, mid := range mids {
		if mid == snapshot.Mid {
			continue
		}
		candidate, ok := projected.ByMid[mid]
		if !ok {
			continue
		}
		if snapshot.Cid > 0 && candidate.Cid != snapshot.Cid {
			continue
		}
		snapshots = append(snapshots, candidate)
	}
	sortSnapshotsBySearchTag(snapshots, "update_stamp")
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
