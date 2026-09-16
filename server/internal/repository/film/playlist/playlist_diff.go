package playlist

import (
	"encoding/json"
	"sort"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/film/shared"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type playlistSlot struct {
	sourceID   string
	movieKey   string
	groupIndex int
}

type playlistChange struct {
	MovieKey       string
	FirstInsert    bool
	NotifyWorthy   bool // 任一线路「最后一项分集标签」有变化（含新增/回退/顺序变化）或首次写入
	CountIncreased bool // 相对本源上次 playlist，最大集数变多（含中间插集、最后一项仍是「完结」）
	PrevMaxCount   int  // 本源写前最大集数；与其它源合计成写前全库最大
	Signatures     []PlaylistSignature
}

type PlaylistSignature struct {
	GroupIndex int
	GroupName  string
	Content    string
}

func PlaylistSignatureContents(sigs []PlaylistSignature) []string {
	contents := make([]string, 0, len(sigs))
	for _, sig := range sigs {
		contents = append(contents, sig.Content)
	}
	return contents
}

// playlistGroupKey 线路分组标识（分组序号 + 线路名）。
type playlistGroupKey struct {
	GroupIndex int
	GroupName  string
}

func saveGroupedPlaylists(sourceID string, playlists []model.SlaveMoviePlaylist, keysByMovieKey map[string]struct{}) ([]playlistChange, error) {
	movieKeys := make([]string, 0, len(keysByMovieKey))
	for movieKey := range keysByMovieKey {
		if strings.TrimSpace(movieKey) == "" {
			continue
		}
		movieKeys = append(movieKeys, movieKey)
	}
	sort.Strings(movieKeys)

	if len(playlists) > 0 {
		sort.Slice(playlists, func(i, j int) bool {
			if playlists[i].MovieKey == playlists[j].MovieKey {
				return playlists[i].GroupIndex < playlists[j].GroupIndex
			}
			return playlists[i].MovieKey < playlists[j].MovieKey
		})
		// 同一 (movie_key, group_index) 只保留最后一行：同一影片在源站常有多个条目
		// （如「XXX英语」「XXX国语」共享豆瓣匹配键），都会写入同一 (key, group) 槽位，
		// 落库按唯一键后写覆盖。签名必须与落库语义对齐，否则每次采集都会把
		// 「多条目并存」误判为结构变化，更新列表反复刷同一 mid。
		playlists = DedupePlaylistRows(playlists)
	}

	// 1. 无事务只读比对签名与变更
	existing, err := loadPlaylistSignaturesTx(db.Mdb, sourceID, movieKeys)
	if err != nil {
		return nil, err
	}
	incoming := buildPlaylistSignatures(playlists)
	changes := diffPlaylistMovieKeys(existing, incoming, movieKeys)

	// 2. 无变更短路（No-op Short-circuit）：无任何改动直接返回，避免无意义开启事务
	if len(changes) == 0 {
		return changes, nil
	}

	// 3. 变更行原地 upsert；只按 (source_id, movie_key, group_index) 删除消失的线路。
	// 禁止按 movie_key 整组 DELETE：多源同时写同一主键时会打到 idx_slave_movie_key 上互相堵住。
	changedKeysMap := make(map[string]struct{}, len(changes))
	changedKeys := make([]string, 0, len(changes))
	for _, c := range changes {
		if c.MovieKey != "" {
			if _, ok := changedKeysMap[c.MovieKey]; !ok {
				changedKeysMap[c.MovieKey] = struct{}{}
				changedKeys = append(changedKeys, c.MovieKey)
			}
		}
	}
	sort.Strings(changedKeys)

	changedPlaylists := make([]model.SlaveMoviePlaylist, 0, len(playlists))
	for _, p := range playlists {
		if _, ok := changedKeysMap[p.MovieKey]; ok {
			changedPlaylists = append(changedPlaylists, p)
		}
	}
	vanished := vanishedPlaylistSlots(sourceID, existing, incoming, changedKeys)

	err = db.Mdb.Transaction(func(tx *gorm.DB) error {
		if len(changedPlaylists) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "source_id"}, {Name: "movie_key"}, {Name: "group_index"}},
				DoUpdates: clause.AssignmentColumns([]string{"group_name", "content", "updated_at"}),
			}).CreateInBatches(&changedPlaylists, 500).Error; err != nil {
				return err
			}
		}
		for _, slot := range vanished {
			if err := tx.Unscoped().
				Where("source_id = ? AND movie_key = ? AND group_index = ?", slot.sourceID, slot.movieKey, slot.groupIndex).
				Delete(&model.SlaveMoviePlaylist{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changes, nil
}

func vanishedPlaylistSlots(sourceID string, existing, incoming map[string][]PlaylistSignature, changedKeys []string) []playlistSlot {
	if len(changedKeys) == 0 {
		return nil
	}
	out := make([]playlistSlot, 0)
	for _, movieKey := range changedKeys {
		keep := make(map[int]struct{}, len(incoming[movieKey]))
		for _, sig := range incoming[movieKey] {
			keep[sig.GroupIndex] = struct{}{}
		}
		for _, sig := range existing[movieKey] {
			if _, ok := keep[sig.GroupIndex]; ok {
				continue
			}
			out = append(out, playlistSlot{sourceID: sourceID, movieKey: movieKey, groupIndex: sig.GroupIndex})
		}
	}
	return out
}

func loadPlaylistSignaturesTx(tx *gorm.DB, sourceID string, movieKeys []string) (map[string][]PlaylistSignature, error) {
	result := make(map[string][]PlaylistSignature, len(movieKeys))
	if len(movieKeys) == 0 {
		return result, nil
	}
	var rows []model.SlaveMoviePlaylist
	if err := tx.Where("source_id = ? AND movie_key IN ?", sourceID, movieKeys).
		Order("movie_key ASC, group_index ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.MovieKey] = append(result[row.MovieKey], PlaylistSignature{
			GroupIndex: row.GroupIndex,
			GroupName:  row.GroupName,
			Content:    row.Content,
		})
	}
	return result, nil
}

func buildPlaylistSignatures(playlists []model.SlaveMoviePlaylist) map[string][]PlaylistSignature {
	result := make(map[string][]PlaylistSignature, len(playlists))
	for _, playlist := range playlists {
		result[playlist.MovieKey] = append(result[playlist.MovieKey], PlaylistSignature{
			GroupIndex: playlist.GroupIndex,
			GroupName:  playlist.GroupName,
			Content:    playlist.Content,
		})
	}
	return result
}

func diffPlaylistMovieKeys(existing map[string][]PlaylistSignature, incoming map[string][]PlaylistSignature, movieKeys []string) []playlistChange {
	changed := make([]playlistChange, 0, len(movieKeys))
	for _, movieKey := range movieKeys {
		left, right := existing[movieKey], incoming[movieKey]
		if SamePlaylistSignatures(left, right) {
			continue
		}
		first := len(left) == 0
		notifyWorthy := false
		countIncreased := false
		prevMax := shared.MaxEpisodeCount(shared.ExtractEpisodeCountsFromContents(PlaylistSignatureContents(left)))
		if len(right) > 0 {
			countIncreased = shared.IsEpisodeCountHigher(
				shared.ExtractEpisodeCountsFromContents(PlaylistSignatureContents(right)),
				shared.ExtractEpisodeCountsFromContents(PlaylistSignatureContents(left)),
			)
		}
		switch {
		case len(right) == 0:
			// right 为空 = 该 key 本次未出现（源站改名/条目消失后的残留或陈旧 key）→ 不是内容更新，
			// 不进更新列表，否则改名/条目切换会让同一 mid 每批反复上报。
		case first:
			// 首次写入：确为新增内容；是否顶最近更新见 slaveShouldBumpStamp（还要比主站集数）
			notifyWorthy = true
		default:
			// 任一线路「最后一项分集标签」与库中不同（含新增/回退/顺序变化）→ 进更新列表；
			// 最后一项相同但集数变多（中间插集）也进；仅链接变化不进。
			notifyWorthy = PlaylistLastEpisodeChanged(left, right)
		}
		changed = append(changed, playlistChange{
			MovieKey:       movieKey,
			FirstInsert:    first,
			NotifyWorthy:   notifyWorthy,
			CountIncreased: countIncreased,
			PrevMaxCount:   prevMax,
			Signatures:     right,
		})
	}
	return changed
}

// LastEpisodeLabel 线路的「最后一集」标签：取源站返回顺序的最后一个非空 Episode 原文。
// macCMS 源站按集数/日期顺序返回（剧「第01集…第N集」、综艺「第20240107期…」），
// 最后一项即最新一集；不解析数字，HD/正片等无数字标签同样适用。
func LastEpisodeLabel(links []model.MovieUrlInfo) string {
	last := ""
	for _, u := range links {
		if label := strings.TrimSpace(u.Episode); label != "" {
			last = label
		}
	}
	return last
}

// lastEpisodeByGroup 把线路签名转为「线路 → 最后一集标签」。
func lastEpisodeByGroup(sigs []PlaylistSignature) map[playlistGroupKey]string {
	out := make(map[playlistGroupKey]string, len(sigs))
	for _, s := range sigs {
		key := playlistGroupKey{GroupIndex: s.GroupIndex, GroupName: strings.TrimSpace(s.GroupName)}
		var links []model.MovieUrlInfo
		if err := json.Unmarshal([]byte(s.Content), &links); err != nil {
			out[key] = ""
			continue
		}
		out[key] = LastEpisodeLabel(links)
	}
	return out
}

// PlaylistLastEpisodeChanged 任一线路（按 GroupIndex+GroupName 对齐）的「最后一项分集标签」变化 → true。
// 线路新增/消失、任一线路最后一项标签不同（含集数回退/顺序变化）都算变化；仅链接/中间集变化不算。
func PlaylistLastEpisodeChanged(left, right []PlaylistSignature) bool {
	leftByGroup := lastEpisodeByGroup(left)
	rightByGroup := lastEpisodeByGroup(right)
	if len(leftByGroup) != len(rightByGroup) {
		return true
	}
	for key, label := range rightByGroup {
		if oldLabel, ok := leftByGroup[key]; !ok || oldLabel != label {
			return true
		}
	}
	return false
}

// DedupePlaylistRows 按 (movie_key, group_index) 去重，保留最后一行。
// 入参需已按 movie_key ASC, group_index ASC 排序；与落库 OnConflict 后写覆盖语义一致。
func DedupePlaylistRows(rows []model.SlaveMoviePlaylist) []model.SlaveMoviePlaylist {
	if len(rows) < 2 {
		return rows
	}
	out := rows[:0]
	for i := 0; i < len(rows); {
		j := i + 1
		for j < len(rows) && rows[j].MovieKey == rows[i].MovieKey && rows[j].GroupIndex == rows[i].GroupIndex {
			j++
		}
		out = append(out, rows[j-1])
		i = j
	}
	return out
}

func SamePlaylistSignatures(left []PlaylistSignature, right []PlaylistSignature) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].GroupIndex != right[index].GroupIndex {
			return false
		}
		if left[index].GroupName != right[index].GroupName {
			return false
		}
		if normalizePlaylistCompareContent(left[index].Content) != normalizePlaylistCompareContent(right[index].Content) {
			return false
		}
	}
	return true
}

// normalizePlaylistCompareContent 归一化播放列表内容用于「是否需要写库」对比：
// trim 集数、去掉链接 query。仅用于对比，不影响实际保存的播放数据。
func normalizePlaylistCompareContent(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	var links []model.MovieUrlInfo
	if err := json.Unmarshal([]byte(raw), &links); err != nil {
		return raw
	}
	out := make([]model.MovieUrlInfo, len(links))
	for i, u := range links {
		out[i] = model.MovieUrlInfo{
			Episode: strings.TrimSpace(u.Episode),
			Link:    shared.StripURLQuery(strings.TrimSpace(u.Link)),
		}
	}
	data, _ := json.Marshal(out)
	return string(data)
}
