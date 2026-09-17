package playlist

import (
	"server/internal/model"
	"server/internal/repository/film/shared"
	"server/internal/utils"
)

// BuildPlaylistCandidateKeys 尚未唯一命中主站影片时的落库键；有大类时不含纯片名，避免同名跨类共用槽。
func BuildPlaylistCandidateKeys(detail model.MovieDetail) []string {
	pid := shared.ResolveMovieDetailRootPid(detail)
	keys := shared.BuildMovieMatchKeysWithCategory(detail.DbId, detail.Name, pid)
	if pid <= 0 || len(keys) == 0 {
		return keys
	}
	plainTitle := utils.NormalizeCollectionTitle(detail.Name)
	if plainTitle == "" {
		return keys
	}
	plainKey := utils.GenerateHashKey(plainTitle)
	candidates := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == plainKey {
			continue
		}
		candidates = append(candidates, key)
	}
	return candidates
}

// inheritPrimaryMovieKeyIfUnique 未识别大类时，仅当片名只命中一部主站影片才沿用该片主键。
func inheritPrimaryMovieKeyIfUnique(candidateMids []int64, keysByMid map[int64][]string) string {
	seen := make(map[int64]struct{}, len(candidateMids))
	uniq := make([]int64, 0, 1)
	for _, mid := range candidateMids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		uniq = append(uniq, mid)
	}
	if len(uniq) != 1 {
		return ""
	}
	keys := keysByMid[uniq[0]]
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}
