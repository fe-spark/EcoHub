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

	candidates := make([]relatedSnapshotScore, 0)
	for _, candidate := range readModel.projectedSnapshotsByPid(snapshot.Pid) {
		if candidate.Mid == snapshot.Mid {
			continue
		}
		candidate = projectSnapshotCategory(candidate)
		if !isVisibleProjectedSnapshot(candidate) {
			continue
		}
		score := scoreRelatedSnapshot(snapshot, candidate)
		if score < relatedSnapshotMinScore {
			continue
		}
		candidates = append(candidates, relatedSnapshotScore{snapshot: candidate, score: score})
	}
	sortRelatedSnapshots(candidates)
	snapshots := relatedScoresToSnapshots(candidates)
	snapshots = appendTopScoredCategoryFallbacks(readModel, snapshot, snapshots)
	page.Total = len(snapshots)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}
	return pageSnapshots(snapshots, page)
}

const relatedSnapshotMinScore = 24

type relatedSnapshotScore struct {
	snapshot model.FilmListSnapshot
	score    int
}

func scoreRelatedSnapshot(current model.FilmListSnapshot, candidate model.FilmListSnapshot) int {
	relationScore := 0
	if current.SeriesKey != "" && current.SeriesKey == candidate.SeriesKey {
		relationScore += 100
	}
	relationScore += titleRelatedScore(current, candidate)
	relationScore += tagRelatedScore(splitClassTags(current.ClassTag), splitClassTags(candidate.ClassTag))
	relationScore += peopleRelatedScore(current.Director, candidate.Director, 24)
	relationScore += peopleRelatedScore(current.Actor, candidate.Actor, 18)
	if relationScore == 0 {
		return 0
	}

	score := relationScore
	if current.Cid > 0 && current.Cid == candidate.Cid {
		score += 18
	}
	score += snapshotMetaRelatedScore(current, candidate)
	return score
}

func titleRelatedScore(current model.FilmListSnapshot, candidate model.FilmListSnapshot) int {
	coreToken := extractCoreSearchToken(current.Name)
	if coreToken == "" {
		return 0
	}
	candidateCoreToken := extractCoreSearchToken(candidate.Name)
	name := strings.TrimSpace(candidate.Name)
	subTitle := strings.TrimSpace(candidate.SubTitle)
	switch {
	case candidateCoreToken != "" && candidateCoreToken == coreToken:
		return 45
	case name == coreToken:
		return 35
	case strings.HasPrefix(name, coreToken):
		return 25
	case strings.Contains(name, coreToken):
		return 18
	case subTitle != "" && strings.Contains(subTitle, coreToken):
		return 10
	default:
		return 0
	}
}

func tagRelatedScore(currentTags []string, candidateTags []string) int {
	if len(currentTags) == 0 || len(candidateTags) == 0 {
		return 0
	}
	currentSet := buildTagSet(currentTags)
	score := 0
	for _, tag := range candidateTags {
		if _, ok := currentSet[tag]; ok {
			score += 12
			if score >= 36 {
				return 36
			}
		}
	}
	return score
}

func peopleRelatedScore(current string, candidate string, maxScore int) int {
	currentSet := splitPeopleSet(current)
	if len(currentSet) == 0 {
		return 0
	}
	score := 0
	for _, name := range splitPeople(candidate) {
		if _, ok := currentSet[name]; ok {
			score += 8
			if score >= maxScore {
				return maxScore
			}
		}
	}
	return score
}

func splitPeopleSet(raw string) map[string]struct{} {
	people := splitPeople(raw)
	set := make(map[string]struct{}, len(people))
	for _, name := range people {
		set[name] = struct{}{}
	}
	return set
}

func splitPeople(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := []string{raw}
	for _, sep := range []string{",", "，", "/", "|", "、", " "} {
		next := make([]string, 0, len(parts))
		for _, part := range parts {
			for item := range strings.SplitSeq(part, sep) {
				item = strings.TrimSpace(item)
				if item != "" {
					next = append(next, item)
				}
			}
		}
		parts = next
	}
	return parts
}

func snapshotMetaRelatedScore(current model.FilmListSnapshot, candidate model.FilmListSnapshot) int {
	score := 0
	if current.Year > 0 && candidate.Year > 0 {
		diff := current.Year - candidate.Year
		if diff < 0 {
			diff = -diff
		}
		if diff == 0 {
			score += 8
		} else if diff == 1 {
			score += 4
		}
	}
	if current.Area != "" && current.Area == candidate.Area {
		score += 5
	}
	if current.Language != "" && current.Language == candidate.Language {
		score += 3
	}
	return score
}

func sortRelatedSnapshots(scores []relatedSnapshotScore) {
	sort.SliceStable(scores, func(i, j int) bool {
		left := scores[i]
		right := scores[j]
		if left.score != right.score {
			return left.score > right.score
		}
		if left.snapshot.UpdateStamp != right.snapshot.UpdateStamp {
			return left.snapshot.UpdateStamp > right.snapshot.UpdateStamp
		}
		return left.snapshot.Mid > right.snapshot.Mid
	})
}

func relatedScoresToSnapshots(scores []relatedSnapshotScore) []model.FilmListSnapshot {
	snapshots := make([]model.FilmListSnapshot, 0, len(scores))
	for _, item := range scores {
		snapshots = append(snapshots, item.snapshot)
	}
	return snapshots
}

func appendTopScoredCategoryFallbacks(readModel *FilmReadModel, current model.FilmListSnapshot, snapshots []model.FilmListSnapshot) []model.FilmListSnapshot {
	seen := make(map[int64]struct{}, len(snapshots)+1)
	seen[current.Mid] = struct{}{}
	for _, snapshot := range snapshots {
		seen[snapshot.Mid] = struct{}{}
	}
	fallbacks := make([]model.FilmListSnapshot, 0)
	for _, candidate := range readModel.projectedSnapshotsByPid(current.Pid) {
		if _, ok := seen[candidate.Mid]; ok {
			continue
		}
		candidate = projectSnapshotCategory(candidate)
		if !isVisibleProjectedSnapshot(candidate) {
			continue
		}
		if current.Cid > 0 && candidate.Cid != current.Cid {
			continue
		}
		fallbacks = append(fallbacks, candidate)
	}
	sortTopScoredFallbackSnapshots(fallbacks)
	return append(snapshots, fallbacks...)
}

func sortTopScoredFallbackSnapshots(snapshots []model.FilmListSnapshot) {
	sort.SliceStable(snapshots, func(i, j int) bool {
		left := snapshots[i]
		right := snapshots[j]
		if left.Score != right.Score {
			return left.Score > right.Score
		}
		if left.Hits != right.Hits {
			return left.Hits > right.Hits
		}
		if left.UpdateStamp != right.UpdateStamp {
			return left.UpdateStamp > right.UpdateStamp
		}
		return left.Mid > right.Mid
	})
}
