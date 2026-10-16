package writer

import (
	"fmt"

	"server/internal/model"
	"server/internal/repository/film/shared"
	"server/internal/utils"

	"gorm.io/gorm"
)

type matchKeyRank struct {
	key  string
	rank int
}

// rankedPeerMatchKeys 匹配键强度：0 豆瓣，1 片名加大类，2 纯片名。
func rankedPeerMatchKeys(source *model.FilmSource, detail model.MovieDetail) []matchKeyRank {
	return rankMovieMatchKeys(detail.DbId, detail.Name, resolveMatchCategoryPid(source, detail))
}

func peerMatchKeyRanks(source *model.FilmSource, detail model.MovieDetail) map[string]int {
	ranked := rankedPeerMatchKeys(source, detail)
	out := make(map[string]int, len(ranked))
	for _, item := range ranked {
		out[item.key] = item.rank
	}
	return out
}

func rankMovieMatchKeys(dbID int64, name string, pid int64) []matchKeyRank {
	keys := shared.BuildMovieMatchKeysWithCategory(dbID, name, pid)
	dbKey := ""
	if identity := utils.BuildCollectionDbIdentity(dbID, name); identity != "" {
		dbKey = utils.GenerateHashKey(identity)
	}
	catKey := ""
	if title := utils.NormalizeIdentityTitle(name); title != "" && pid > 0 {
		catKey = utils.GenerateHashKey(fmt.Sprintf("%s#cat_%d", title, pid))
	}
	out := make([]matchKeyRank, 0, len(keys))
	for _, key := range keys {
		rank := 2
		switch {
		case dbKey != "" && key == dbKey:
			rank = 0
		case catKey != "" && key == catKey:
			rank = 1
		}
		out = append(out, matchKeyRank{key: key, rank: rank})
	}
	return out
}

// bestMidForKeys 优先用更强的匹配键认领。同一强度再取更小的 mid。
func bestMidForKeys(keys []string, ranks map[string]int, rows []matchKeyMid) int64 {
	allowed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key != "" {
			allowed[key] = struct{}{}
		}
	}
	found := false
	bestRank := 0
	var bestMid int64
	for _, row := range rows {
		if row.Mid <= 0 || row.MatchKey == "" {
			continue
		}
		if _, ok := allowed[row.MatchKey]; !ok {
			continue
		}
		rank := 2
		if ranks != nil {
			if value, ok := ranks[row.MatchKey]; ok {
				rank = value
			}
		}
		if !found || rank < bestRank || (rank == bestRank && row.Mid < bestMid) {
			found = true
			bestRank = rank
			bestMid = row.Mid
		}
	}
	if !found {
		return 0
	}
	return bestMid
}

func midsFromMatchRows(rows []matchKeyMid) []int64 {
	seen := make(map[int64]struct{}, len(rows))
	out := make([]int64, 0, len(rows))
	for _, row := range rows {
		if row.Mid <= 0 {
			continue
		}
		if _, ok := seen[row.Mid]; ok {
			continue
		}
		seen[row.Mid] = struct{}{}
		out = append(out, row.Mid)
	}
	return out
}

// claimPeerFilmByMatchKeysTx 按最强匹配键认领已有影片。
// 只把不比命中键更强的键补到该 mid 上，避免豆瓣键和分类键被抄到仅片名相同的更小 mid。
func claimPeerFilmByMatchKeysTx(tx *gorm.DB, source *model.FilmSource, detail model.MovieDetail) (int64, *model.FilmIndex, error) {
	ranked := rankedPeerMatchKeys(source, detail)
	if len(ranked) == 0 {
		return 0, nil, nil
	}
	keys := make([]string, 0, len(ranked))
	ranks := make(map[string]int, len(ranked))
	for _, item := range ranked {
		keys = append(keys, item.key)
		ranks[item.key] = item.rank
	}
	var rows []model.MovieMatchKey
	if err := tx.Where("match_key IN ?", keys).Find(&rows).Error; err != nil {
		return 0, nil, err
	}
	midRows := make([]matchKeyMid, 0, len(rows))
	for _, row := range rows {
		midRows = append(midRows, matchKeyMid{MatchKey: row.MatchKey, Mid: row.Mid})
	}
	mid := bestMidForKeys(keys, ranks, midRows)
	if mid <= 0 {
		return 0, nil, nil
	}
	var film model.FilmIndex
	if err := tx.Where("mid = ?", mid).First(&film).Error; err != nil {
		return 0, nil, nil
	}
	if film.Mid <= 0 {
		return 0, nil, nil
	}
	winRank := 2
	for _, row := range midRows {
		if row.Mid != film.Mid {
			continue
		}
		if rank, ok := ranks[row.MatchKey]; ok && rank < winRank {
			winRank = rank
		}
	}
	if err := fillWeakerMatchKeysTx(tx, film.Mid, ranked, winRank); err != nil {
		return 0, nil, err
	}
	return film.Mid, &film, nil
}

func fillWeakerMatchKeysTx(tx *gorm.DB, mid int64, ranked []matchKeyRank, winRank int) error {
	for _, item := range ranked {
		if item.key == "" || item.rank < winRank {
			continue
		}
		var count int64
		if err := tx.Model(&model.MovieMatchKey{}).Where("mid = ? AND match_key = ?", mid, item.key).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		if err := tx.Create(&model.MovieMatchKey{Mid: mid, MatchKey: item.key}).Error; err != nil {
			return err
		}
	}
	return nil
}
