package film

import (
	"encoding/json"
	"fmt"
	"sort"
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
	NotifyWorthy bool // 可能进入 stamp 路径；生产还要过全库最大集数才真正上榜
	FirstInsert  bool
	Reason       string
	ExistingSig  string
	IncomingSig  string
}

// DiffSlavePlaylistGroups 对比同一 movie_key 下的 playlist 组。
// 与生产 saveGroupedPlaylists 对齐：先按 (movie_key, group_index) 排序去重
// （同一影片多条目共享匹配键时，落库后写覆盖，仅最后一行生效），再对比。
// 可能进 stamp 路径：首次写入、最后一集标签变化、或自己集数变多。
// 生产最终还要过全库最大集数；仅链接变化不进。
func DiffSlavePlaylistGroups(movieKey string, existing, incoming []model.MoviePlaylist) SlavePlaylistDiff {
	left := playlistsToSignatures(sortDedupePlaylists(existing))
	right := playlistsToSignatures(sortDedupePlaylists(incoming))
	diff := SlavePlaylistDiff{
		MovieKey:    movieKey,
		ExistingSig: formatPlaylistStructure(left),
		IncomingSig: formatPlaylistStructure(right),
	}
	if samePlaylistSignatures(left, right) {
		diff.Reason = "完全一致（含归一化后的链接 path）"
		return diff
	}
	first := len(left) == 0
	diff.FirstInsert = first
	if first {
		diff.WouldWrite = true
		diff.NotifyWorthy = true
		diff.Reason = "首次写入（库中无该 movie_key 的 playlist）；生产还要过全库最大集数才上榜"
		return diff
	}
	if len(right) == 0 {
		diff.Reason = "该 key 本次无内容（源站改名/条目消失的残留，不进更新列表）"
		return diff
	}
	diff.WouldWrite = true
	if playlistLastEpisodeChanged(left, right) {
		diff.NotifyWorthy = true
		diff.Reason = "任一线路最后一集有变化（含新增/回退）；生产还要过全库最大集数才上榜"
	} else if isEpisodeCountHigher(extractEpisodeCountsFromPlaylistSignatures(right), extractEpisodeCountsFromPlaylistSignatures(left)) {
		diff.NotifyWorthy = true
		diff.Reason = "最后一集标签相同但自己集数变多（中间插集）；若全库已有相同或更多集数则不上榜"
	} else {
		diff.Reason = "各线路最后一项分集标签相同且集数未增加（仅链接变化，写库但不进更新列表）"
	}
	return diff
}

// sortDedupePlaylists 排序并按 (movie_key, group_index) 去重（保留最后一行）。
func sortDedupePlaylists(rows []model.MoviePlaylist) []model.MoviePlaylist {
	if len(rows) < 2 {
		return rows
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].MovieKey == rows[j].MovieKey {
			return rows[i].GroupIndex < rows[j].GroupIndex
		}
		return rows[i].MovieKey < rows[j].MovieKey
	})
	return dedupePlaylistRows(rows)
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
	// 生产路径：业务变更才写库；进更新列表还须本源集数大于全库最大（此处无其它源，只比旧详情）。
	if !ex.BusinessChanged {
		ex.WouldNotify = false
		ex.Reason = "业务指纹一致 → 不写库，不进更新列表"
		return ex
	}
	if isEpisodeCountHigher(extractEpisodeCountsFromDetail(incoming), extractEpisodeCountsFromDetail(old)) {
		ex.WouldNotify = true
		ex.Reason = "业务有变更且自己集数变多 → 写库；若其它源已有相同或更多集数则生产路径仍不进最近更新"
		return ex
	}
	ex.WouldNotify = false
	ex.Reason = "业务有变更但集数未超过已有最大（元数据/链接噪声或后到的同集数源）→ 写库但不进更新列表"
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
