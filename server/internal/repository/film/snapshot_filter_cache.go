package film

import (
	"sort"
	"strings"

	"server/internal/model"
	"server/internal/model/dto"
)

func SearchSnapshotsByKeywordFast(version string, keyword string, page *dto.Page) []model.FilmListSnapshot {
	return SearchSnapshotsByKeywordReadModel(version, keyword, page)
}

func ListProvideSnapshotsFast(version string, st model.SearchTagsVO, keyword string, recentHours int, page *dto.Page) []model.FilmListSnapshot {
	return ListProvideSnapshotsReadModel(version, st, keyword, recentHours, page)
}

func ListFilmSnapshotsByTagsFast(version string, st model.SearchTagsVO, page *dto.Page) []model.FilmListSnapshot {
	return ListFilmSnapshotsByTagsReadModel(version, st, page)
}

func sortSnapshotsBySearchTag(snapshots []model.FilmListSnapshot, sortValue string) {
	if strings.TrimSpace(sortValue) == "" {
		sortValue = "update_stamp"
	}
	sort.SliceStable(snapshots, func(i, j int) bool {
		left := snapshots[i]
		right := snapshots[j]
		switch sortValue {
		case "hits":
			if left.Hits != right.Hits {
				return left.Hits > right.Hits
			}
		case "score":
			if left.Score != right.Score {
				return left.Score > right.Score
			}
		case "year":
			if left.Year != right.Year {
				return left.Year > right.Year
			}
		}
		if left.UpdateStamp != right.UpdateStamp {
			return left.UpdateStamp > right.UpdateStamp
		}
		return left.Mid > right.Mid
	})
}
