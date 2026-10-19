package shared

import (
	"strings"

	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

type peerPick struct {
	mid  int64
	rank int
}

// LoadPeerPlaylists 按匹配键找出其他已启用站的播放线路，挂到这些档案上。
// 返回的线路 Mid 是正在看的档案，SourceId 仍是线路所属站。每站只保留匹配最强的一部。
func LoadPeerPlaylists(tx *gorm.DB, infos []model.FilmIndex) (map[int64][]model.FilmSourcePlaylist, error) {
	out := make(map[int64][]model.FilmSourcePlaylist)
	if tx == nil || len(infos) == 0 {
		return out, nil
	}
	enabled := enabledSourceIDs()
	if len(enabled) == 0 {
		return out, nil
	}

	ranks, keys := peerKeyRanks(infos)
	if len(keys) == 0 {
		return out, nil
	}
	var keyRows []model.MovieMatchKey
	if err := tx.Where("match_key IN ?", keys).Find(&keyRows).Error; err != nil {
		return nil, err
	}
	if len(keyRows) == 0 {
		return out, nil
	}
	ownerOf, err := filmOwnersTx(tx, keyRows)
	if err != nil {
		return nil, err
	}
	chosen := choosePeerFilms(infos, ranks, keyRows, ownerOf, enabled)
	if len(chosen) == 0 {
		return out, nil
	}
	linesByMid, err := peerPlayLinesTx(tx, chosen, ownerOf)
	if err != nil {
		return nil, err
	}
	for origin, bySource := range chosen {
		for sourceID, pick := range bySource {
			for _, line := range linesByMid[pick.mid] {
				if line.SourceId != sourceID {
					continue
				}
				line.Mid = origin
				out[origin] = append(out[origin], line)
			}
		}
	}
	return out, nil
}

func enabledSourceIDs() map[string]struct{} {
	out := make(map[string]struct{})
	for _, source := range support.GetCollectSourceList() {
		id := strings.TrimSpace(source.Id)
		if source.State && id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

func peerKeyRanks(infos []model.FilmIndex) (map[int64]map[string]int, []string) {
	ranks := make(map[int64]map[string]int, len(infos))
	seen := make(map[string]struct{})
	keys := make([]string, 0)
	for _, info := range infos {
		if info.Mid <= 0 {
			continue
		}
		built := BuildMovieMatchKeysWithCategory(info.DbId, info.Name, info.Pid)
		if len(built) == 0 {
			continue
		}
		rank := make(map[string]int, len(built))
		for i, key := range built {
			if key == "" {
				continue
			}
			rank[key] = i
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
		if len(rank) > 0 {
			ranks[info.Mid] = rank
		}
	}
	return ranks, keys
}

func filmOwnersTx(tx *gorm.DB, rows []model.MovieMatchKey) (map[int64]string, error) {
	seen := make(map[int64]struct{}, len(rows))
	mids := make([]int64, 0, len(rows))
	for _, row := range rows {
		if row.Mid <= 0 {
			continue
		}
		if _, ok := seen[row.Mid]; ok {
			continue
		}
		seen[row.Mid] = struct{}{}
		mids = append(mids, row.Mid)
	}
	ownerOf := make(map[int64]string, len(mids))
	if len(mids) == 0 {
		return ownerOf, nil
	}
	var films []model.FilmIndex
	if err := tx.Select("mid", "first_source_id").Where("mid IN ?", mids).Find(&films).Error; err != nil {
		return nil, err
	}
	for _, film := range films {
		ownerOf[film.Mid] = strings.TrimSpace(film.FirstSourceId)
	}
	return ownerOf, nil
}

func choosePeerFilms(infos []model.FilmIndex, ranks map[int64]map[string]int, rows []model.MovieMatchKey, ownerOf map[int64]string, enabled map[string]struct{}) map[int64]map[string]peerPick {
	originSource := make(map[int64]string, len(infos))
	for _, info := range infos {
		originSource[info.Mid] = strings.TrimSpace(info.FirstSourceId)
	}
	chosen := make(map[int64]map[string]peerPick)
	for _, row := range rows {
		owner := ownerOf[row.Mid]
		if owner == "" {
			continue
		}
		if _, ok := enabled[owner]; !ok {
			continue
		}
		for origin, keyRank := range ranks {
			if row.Mid == origin || owner == originSource[origin] {
				continue
			}
			rank, ok := keyRank[row.MatchKey]
			if !ok {
				continue
			}
			bySource := chosen[origin]
			if bySource == nil {
				bySource = make(map[string]peerPick)
				chosen[origin] = bySource
			}
			current, exists := bySource[owner]
			if !exists || rank < current.rank || (rank == current.rank && row.Mid < current.mid) {
				bySource[owner] = peerPick{mid: row.Mid, rank: rank}
			}
		}
	}
	return chosen
}

func peerPlayLinesTx(tx *gorm.DB, chosen map[int64]map[string]peerPick, ownerOf map[int64]string) (map[int64][]model.FilmSourcePlaylist, error) {
	seen := make(map[int64]struct{})
	mids := make([]int64, 0)
	for _, bySource := range chosen {
		for _, pick := range bySource {
			if _, ok := seen[pick.mid]; ok {
				continue
			}
			seen[pick.mid] = struct{}{}
			mids = append(mids, pick.mid)
		}
	}
	linesByMid := make(map[int64][]model.FilmSourcePlaylist)
	if len(mids) == 0 {
		return linesByMid, nil
	}
	var lines []model.FilmSourcePlaylist
	if err := tx.Where("mid IN ? AND line_kind = ?", mids, "play").Order("group_index ASC").Find(&lines).Error; err != nil {
		return nil, err
	}
	for _, line := range lines {
		if ownerOf[line.Mid] != line.SourceId {
			continue
		}
		linesByMid[line.Mid] = append(linesByMid[line.Mid], line)
	}
	return linesByMid, nil
}
