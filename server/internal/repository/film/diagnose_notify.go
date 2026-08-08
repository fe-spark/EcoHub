package film

import (
	"encoding/json"
	"fmt"
	"strings"

	"server/internal/model"
)

// MasterPlayStructureSig 主站播放结构指纹（进更新列表用的那套：线路名 + 集数标签）。
func MasterPlayStructureSig(detail model.MovieDetail) string {
	return playStructureSignature(detail)
}

// SameMasterPlayStructure 主站播放结构是否一致（忽略链接）。
func SameMasterPlayStructure(a, b model.MovieDetail) bool {
	return samePlayStructure(a, b)
}

// SameMasterBusiness 主站业务指纹是否一致（写库判定；含元数据与归一化链接）。
func SameMasterBusiness(a, b model.MovieDetail) bool {
	return sameStoredMasterDetail(a, b)
}

// SlavePlaylistDiff 附属站 playlist 对比结果（与生产 diffPlaylistMovieKeys / NotifyWorthy 一致）。
type SlavePlaylistDiff struct {
	MovieKey     string
	WouldWrite   bool // 内容有差异（含仅链接变化）
	NotifyWorthy bool // 会进更新列表
	FirstInsert  bool
	Reason       string
	ExistingSig  string
	IncomingSig  string
}

// DiffSlavePlaylistGroups 对比同一 movie_key 下的 playlist 组。
func DiffSlavePlaylistGroups(movieKey string, existing, incoming []model.MoviePlaylist) SlavePlaylistDiff {
	left := playlistsToSignatures(existing)
	right := playlistsToSignatures(incoming)
	diff := SlavePlaylistDiff{
		MovieKey:    movieKey,
		ExistingSig: formatPlaylistStructure(left),
		IncomingSig: formatPlaylistStructure(right),
	}
	if samePlaylistSignatures(left, right) {
		diff.Reason = "完全一致（含归一化后的链接 path）"
		return diff
	}
	diff.WouldWrite = true
	first := len(left) == 0
	diff.FirstInsert = first
	if first {
		diff.NotifyWorthy = true
		diff.Reason = "首次写入（库中无该 movie_key 的 playlist）"
		return diff
	}
	if !samePlaylistEpisodeStructure(left, right) {
		diff.NotifyWorthy = true
		diff.Reason = "集数/线路结构变化（会进更新列表）"
		return diff
	}
	diff.NotifyWorthy = false
	diff.Reason = "仅链接等非结构变化（写库但不进更新列表）"
	return diff
}

// MasterNotifyExplain 主站「若用 incoming 覆盖 old」时的写库/通知判定说明。
type MasterNotifyExplain struct {
	BusinessChanged  bool
	StructureChanged bool
	WouldWrite       bool // business 变或 content_key 懒升等；此处仅业务对比
	WouldNotify      bool // 结构变或无旧详情
	Reason           string
	OldStructure     string
	NewStructure     string
}

// ExplainMasterNotify 对照生产 filterPlayStructureNotifyMIDs / sameStoredMasterDetail。
// hasOld=false 表示库中无详情（新片）。
func ExplainMasterNotify(old model.MovieDetail, hasOld bool, incoming model.MovieDetail) MasterNotifyExplain {
	ex := MasterNotifyExplain{
		OldStructure: MasterPlayStructureSig(old),
		NewStructure: MasterPlayStructureSig(incoming),
	}
	if !hasOld {
		ex.WouldWrite = true
		ex.WouldNotify = true
		ex.StructureChanged = true
		ex.Reason = "库中无旧详情 → 视为新片，写库且进更新列表"
		return ex
	}
	ex.BusinessChanged = !SameMasterBusiness(old, incoming)
	ex.StructureChanged = !SameMasterPlayStructure(old, incoming)
	ex.WouldWrite = ex.BusinessChanged
	ex.WouldNotify = ex.StructureChanged // 仅当会写库时才走到 filter；结构变才 notify
	// 生产路径：只有 business 变更才写库，再在 changed 里按结构筛 Notify。
	// 若业务不变则不会写、也不会 notify。
	if !ex.BusinessChanged {
		ex.WouldNotify = false
		ex.Reason = "业务指纹一致 → 不写库，不进更新列表"
		return ex
	}
	if ex.StructureChanged {
		ex.WouldNotify = true
		ex.Reason = "业务有变更且播放结构变化 → 写库且进更新列表"
		return ex
	}
	ex.WouldNotify = false
	ex.Reason = "业务有变更但播放结构未变（元数据/链接噪声）→ 写库但不进更新列表"
	return ex
}

// FormatPlayStructureHuman 人类可读的线路/集数摘要。
func FormatPlayStructureHuman(detail model.MovieDetail) string {
	from := normalizeStringSlice(detail.PlayFrom)
	eps := normalizeEpisodeLabels(detail.PlayList)
	if len(from) == 0 && len(eps) == 0 {
		return "(无播放源)"
	}
	var b strings.Builder
	n := len(eps)
	if len(from) > n {
		n = len(from)
	}
	for i := 0; i < n; i++ {
		name := ""
		if i < len(from) {
			name = from[i]
		}
		labels := []string{}
		if i < len(eps) {
			labels = eps[i]
		}
		fmt.Fprintf(&b, "  [%d] %q episodes=%v (count=%d)\n", i, name, labels, len(labels))
	}
	return b.String()
}

func playlistsToSignatures(rows []model.MoviePlaylist) []playlistSignature {
	out := make([]playlistSignature, 0, len(rows))
	for _, r := range rows {
		out = append(out, playlistSignature{
			GroupIndex: r.GroupIndex,
			GroupName:  r.GroupName,
			Content:    r.Content,
		})
	}
	return out
}

func formatPlaylistStructure(sigs []playlistSignature) string {
	type row struct {
		Index  int      `json:"i"`
		Name   string   `json:"name"`
		Labels []string `json:"labels"`
	}
	rows := make([]row, 0, len(sigs))
	for _, s := range sigs {
		var labels []string
		raw := playlistEpisodeLabelSignature(s.Content)
		_ = json.Unmarshal([]byte(raw), &labels)
		if labels == nil {
			labels = []string{}
		}
		rows = append(rows, row{Index: s.GroupIndex, Name: s.GroupName, Labels: labels})
	}
	data, _ := json.Marshal(rows)
	return string(data)
}

// BuildIncomingSlavePlaylists 用 API 详情构造将写入的 MoviePlaylist 行（对齐 SaveSitePlayList）。
func BuildIncomingSlavePlaylists(sourceID string, detail model.MovieDetail) []model.MoviePlaylist {
	if len(detail.PlayList) == 0 || strings.Contains(detail.CName, "解说") {
		return nil
	}
	var playlists []model.MoviePlaylist
	for _, movieKey := range BuildPlaylistMovieKeys(detail) {
		for index, links := range detail.PlayList {
			if len(links) == 0 {
				continue
			}
			data, _ := json.Marshal(links)
			rawName := ""
			if index < len(detail.PlayFrom) {
				rawName = strings.TrimSpace(detail.PlayFrom[index])
			}
			playlists = append(playlists, model.MoviePlaylist{
				SourceId:   sourceID,
				MovieKey:   movieKey,
				GroupIndex: index,
				GroupName:  rawName,
				Content:    string(data),
			})
		}
	}
	return playlists
}
