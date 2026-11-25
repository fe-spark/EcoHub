package shared

import (
	"strings"

	"errors"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/utils"

	"gorm.io/gorm"
)

type peerPick struct {
	mid  int64
	rank int
}

// PeerCategoryVisibleFunc 判断指定采集站的影片所属分类是否对外可见。
type PeerCategoryVisibleFunc func(sourceID, categoryKey, rootCategoryKey string, cid, pid int64) bool

// LoadActivePeerPlaylists 使用当前数据库连接加载其他站的播放线路。
func LoadActivePeerPlaylists(infos []model.FilmIndex) (map[int64][]model.FilmSourcePlaylist, error) {
	return LoadActivePeerPlaylistsWithFilter(infos, nil)
}

// LoadActivePeerPlaylistsWithFilter 使用当前数据库连接加载其他站的播放线路，并按分类可见性过滤。
func LoadActivePeerPlaylistsWithFilter(infos []model.FilmIndex, visible PeerCategoryVisibleFunc) (map[int64][]model.FilmSourcePlaylist, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	return LoadPeerPlaylistsWithFilter(db.Mdb, infos, visible)
}

// LoadPeerPlaylists 按匹配键找出其他已启用站的播放线路，挂到这些档案上。
// 返回的线路 Mid 是正在看的档案，SourceId 仍是线路所属站。每站只保留匹配最强的一部。
func LoadPeerPlaylists(tx *gorm.DB, infos []model.FilmIndex) (map[int64][]model.FilmSourcePlaylist, error) {
	return LoadPeerPlaylistsWithFilter(tx, infos, nil)
}

// LoadPeerPlaylistsWithFilter 按匹配键找出其他已启用且分类可见站的播放线路，挂到这些档案上。
func LoadPeerPlaylistsWithFilter(tx *gorm.DB, infos []model.FilmIndex, visible PeerCategoryVisibleFunc) (map[int64][]model.FilmSourcePlaylist, error) {
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
	catNames := loadPeerCategoryNamesTx(tx, infos, ownerOf)
	chosen := choosePeerFilms(infos, ranks, keyRows, ownerOf, enabled, catNames, visible)
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
	for _, source := range repobase.GetCollectSourceList() {
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

type filmOwnerMeta struct {
	sourceID         string
	cid              int64
	pid              int64
	dbID             int64
	categoryKey      string
	rootCategoryKey  string
	originalCategory string
	cName            string
}

func filmOwnersTx(tx *gorm.DB, rows []model.MovieMatchKey) (map[int64]filmOwnerMeta, error) {
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
	ownerOf := make(map[int64]filmOwnerMeta, len(mids))
	if len(mids) == 0 {
		return ownerOf, nil
	}
	var films []model.FilmIndex
	if err := tx.Select("mid", "first_source_id", "cid", "pid", "db_id", "category_key", "root_category_key", "original_category", "c_name").Where("mid IN ?", mids).Find(&films).Error; err != nil {
		return nil, err
	}
	for _, film := range films {
		ownerOf[film.Mid] = filmOwnerMeta{
			sourceID:         strings.TrimSpace(film.FirstSourceId),
			cid:              film.Cid,
			pid:              film.Pid,
			dbID:             film.DbId,
			categoryKey:      strings.TrimSpace(film.CategoryKey),
			rootCategoryKey:  strings.TrimSpace(film.RootCategoryKey),
			originalCategory: strings.TrimSpace(film.OriginalCategory),
			cName:            strings.TrimSpace(film.CName),
		}
	}
	return ownerOf, nil
}

func loadPeerCategoryNamesTx(tx *gorm.DB, infos []model.FilmIndex, ownerOf map[int64]filmOwnerMeta) map[int64]string {
	names := make(map[int64]string)
	if tx == nil {
		return names
	}
	seen := make(map[int64]struct{})
	pids := make([]int64, 0)
	addPid := func(pid int64) {
		if pid <= 0 {
			return
		}
		if _, ok := seen[pid]; ok {
			return
		}
		seen[pid] = struct{}{}
		pids = append(pids, pid)
	}
	for _, info := range infos {
		addPid(info.Pid)
	}
	for _, owner := range ownerOf {
		addPid(owner.pid)
	}
	if len(pids) == 0 {
		return names
	}
	var cats []model.Category
	if err := tx.Select("id", "name").Where("id IN ?", pids).Find(&cats).Error; err != nil {
		return names
	}
	for _, cat := range cats {
		if trimmed := strings.TrimSpace(cat.Name); trimmed != "" {
			names[cat.Id] = trimmed
		}
	}
	return names
}

func peerCategoryCompatible(originFilm model.FilmIndex, owner filmOwnerMeta, catNames map[int64]string) bool {
	if originFilm.DbId > 0 && owner.dbID > 0 && originFilm.DbId == owner.dbID {
		return true
	}
	originCat := resolvePeerBigCategory(originFilm.FirstSourceId, originFilm.Pid, originFilm.OriginalCategory, originFilm.CName, catNames)
	ownerCat := resolvePeerBigCategory(owner.sourceID, owner.pid, owner.originalCategory, owner.cName, catNames)
	// 分类名缺失时不靠本地 Pid 猜。各站 Pid 各自建档，数字相同也不代表同一大类。
	if originCat == "" || ownerCat == "" {
		return false
	}
	return originCat == ownerCat
}

func resolvePeerBigCategory(sourceID string, pid int64, origCat, cName string, catNames map[int64]string) string {
	raw := ""
	if pid > 0 {
		raw = strings.TrimSpace(catNames[pid])
	}
	if raw == "" {
		raw = strings.TrimSpace(origCat)
	}
	if raw == "" {
		raw = strings.TrimSpace(cName)
	}
	if raw == "" {
		return ""
	}
	if sid := strings.TrimSpace(sourceID); sid != "" {
		if mapped := strings.TrimSpace(repobase.NormalizeRootCategoryName(sid, raw)); mapped != "" {
			raw = mapped
		}
	}
	return normalizeCanonicalBigCategory(raw)
}

func normalizeCanonicalBigCategory(raw string) string {
	s := strings.ToLower(utils.TraditionalToSimplified(strings.TrimSpace(raw)))
	if s == "" {
		return ""
	}
	switch {
	case strings.Contains(s, "短剧") || strings.Contains(s, "爽剧"):
		return model.BigCategoryShortFilm
	case strings.Contains(s, "纪录") || strings.Contains(s, "记录片") || s == "记录":
		return model.BigCategoryDocumentary
	case strings.Contains(s, "动漫") || strings.Contains(s, "动画") || strings.Contains(s, "番剧") || s == "国漫" || s == "日漫" || s == "美漫":
		return model.BigCategoryAnimation
	case strings.Contains(s, "综艺") || strings.Contains(s, "真人秀") || strings.Contains(s, "脱口秀"):
		return model.BigCategoryVariety
	case s == "电视剧" || s == "连续剧" || s == "剧集" || s == "剧场" ||
		strings.Contains(s, "连续剧") || strings.Contains(s, "电视剧"):
		return model.BigCategoryTV
	case strings.Contains(s, "电影") || s == "大片" || s == "院线":
		return model.BigCategoryMovie
	default:
		return s
	}
}

func choosePeerFilms(infos []model.FilmIndex, ranks map[int64]map[string]int, rows []model.MovieMatchKey, ownerOf map[int64]filmOwnerMeta, enabled map[string]struct{}, catNames map[int64]string, visible PeerCategoryVisibleFunc) map[int64]map[string]peerPick {
	originInfo := make(map[int64]model.FilmIndex, len(infos))
	for _, info := range infos {
		originInfo[info.Mid] = info
	}
	chosen := make(map[int64]map[string]peerPick)
	for _, row := range rows {
		owner := ownerOf[row.Mid]
		if owner.sourceID == "" {
			continue
		}
		if _, ok := enabled[owner.sourceID]; !ok {
			continue
		}
		if visible != nil && !visible(owner.sourceID, owner.categoryKey, owner.rootCategoryKey, owner.cid, owner.pid) {
			continue
		}
		for origin, keyRank := range ranks {
			originFilm := originInfo[origin]
			if row.Mid == origin || owner.sourceID == strings.TrimSpace(originFilm.FirstSourceId) {
				continue
			}
			// 跨分类防混拼：无相同有效豆瓣 ID 时只比归一化后的大类名，不比各站本地 Pid
			if !peerCategoryCompatible(originFilm, owner, catNames) {
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
			current, exists := bySource[owner.sourceID]
			if !exists || rank < current.rank || (rank == current.rank && row.Mid < current.mid) {
				bySource[owner.sourceID] = peerPick{mid: row.Mid, rank: rank}
			}
		}
	}
	return chosen
}

func peerPlayLinesTx(tx *gorm.DB, chosen map[int64]map[string]peerPick, ownerOf map[int64]filmOwnerMeta) (map[int64][]model.FilmSourcePlaylist, error) {
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
		if ownerOf[line.Mid].sourceID != line.SourceId {
			continue
		}
		linesByMid[line.Mid] = append(linesByMid[line.Mid], line)
	}
	return linesByMid, nil
}
