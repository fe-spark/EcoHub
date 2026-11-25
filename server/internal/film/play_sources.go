package film

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"server/internal/film/shared"
	"server/internal/film/snapshot"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/utils"
)

func skipBadPlaylist(row model.FilmSourcePlaylist, err error) {
	log.Printf("[Play] 播放线路解析失败，已跳过 mid=%d source=%s group=%d err=%v", row.Mid, row.SourceId, row.GroupIndex, err)
}

func playlistsAllBroken(mid int64, rows, failed int) error {
	if rows == 0 || failed != rows {
		return nil
	}
	log.Printf("[Play] 播放线路全部解析失败 mid=%d", mid)
	return fmt.Errorf("播放线路全部解析失败 mid=%d", mid)
}

func brokenPlaylistMids(seen, failed map[int64]int) map[int64]struct{} {
	var broken map[int64]struct{}
	for mid, n := range seen {
		if n == 0 || failed[mid] != n {
			continue
		}
		if broken == nil {
			broken = make(map[int64]struct{})
		}
		broken[mid] = struct{}{}
		log.Printf("[Play] 播放线路全部解析失败，已跳过影片 mid=%d", mid)
	}
	return broken
}

// PlayGroupsFromPlayLinkVos 将播放线路视图拆分为分组链接切片和线路名称切片。
func PlayGroupsFromPlayLinkVos(sources []model.PlayLinkVo) ([][]model.MovieUrlInfo, []string) {
	groups := make([][]model.MovieUrlInfo, 0, len(sources))
	names := make([]string, 0, len(sources))
	for _, s := range sources {
		groups = append(groups, s.LinkList)
		names = append(names, s.Name)
	}
	return groups, names
}

func peerSourceCategoryVisible() shared.PeerCategoryVisibleFunc {
	cache := make(map[string]func(categoryKey, rootKey string, cid, pid int64) bool)
	return func(sourceID, categoryKey, rootCategoryKey string, cid, pid int64) bool {
		sourceID = strings.TrimSpace(sourceID)
		fn, ok := cache[sourceID]
		if !ok {
			fn = snapshot.SourceCategoryVisibility(sourceID)
			cache[sourceID] = fn
		}
		return fn(categoryKey, rootCategoryKey, cid, pid)
	}
}

func appendPeerPlayRows(mids []int64, rows []model.FilmSourcePlaylist) ([]model.FilmSourcePlaylist, error) {
	if len(mids) == 0 {
		return rows, nil
	}
	films, err := ListFilmIndexBrief(mids)
	if err != nil {
		return nil, err
	}
	if len(films) == 0 {
		return rows, nil
	}
	peers, err := shared.LoadActivePeerPlaylistsWithFilter(films, peerSourceCategoryVisible())
	if err != nil {
		return nil, err
	}
	if len(peers) == 0 {
		return rows, nil
	}
	own := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.LineKind == model.LineKindPlay {
			own[fmt.Sprintf("%d\x00%s", row.Mid, row.SourceId)] = struct{}{}
		}
	}
	for origin, lines := range peers {
		for _, line := range lines {
			if _, ok := own[fmt.Sprintf("%d\x00%s", origin, line.SourceId)]; ok {
				continue
			}
			rows = append(rows, line)
		}
	}
	return rows, nil
}

// LoadPlayAndDownloadSourcesByMid 加载并按站点权重聚合指定影片的播放与下载线路。
func LoadPlayAndDownloadSourcesByMid(mid int64) ([]model.PlayLinkVo, [][]model.MovieUrlInfo, error) {
	if mid <= 0 {
		return nil, nil, nil
	}
	rows, err := ListPlaylistsByMid(mid)
	if err != nil {
		return nil, nil, err
	}
	rows, err = appendPeerPlayRows([]int64{mid}, rows)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, nil
	}

	sources, err := repobase.GetEnabledCollectSourceList()
	if err != nil {
		return nil, nil, err
	}
	sourcesByID := make(map[string]model.FilmSource, len(sources))
	sourceOrderMap := make(map[string]int, len(sources))
	for idx, s := range sources {
		sourcesByID[s.Id] = s
		sourceOrderMap[s.Id] = idx
	}

	sourcePlayGroupCounts := make(map[string]int)
	for _, r := range rows {
		if r.LineKind == model.LineKindPlay {
			sourcePlayGroupCounts[r.SourceId]++
		}
	}

	var playList []model.PlayLinkVo
	var downloadList [][]model.MovieUrlInfo
	failed := 0
	for _, r := range rows {
		source, hasSource := sourcesByID[r.SourceId]
		var rules []utils.DomainReplaceRule
		if hasSource && source.DomainReplaceRules != "" {
			rules = utils.ParseDomainReplaceRules(source.DomainReplaceRules)
		}

		var links []model.MovieUrlInfo
		if err := json.Unmarshal([]byte(r.Content), &links); err != nil {
			skipBadPlaylist(r, err)
			failed++
			continue
		}
		if len(rules) > 0 {
			links = rewriteURLGroup(links, rules)
		}

		if r.SourceId == "manual" {
			if r.LineKind == model.LineKindPlay {
				siteName := strings.TrimSpace(r.GroupName)
				if siteName == "" {
					siteName = "自定义线路"
				}
				groupID := fmt.Sprintf("manual#%d", r.GroupIndex)
				playList = append(playList, model.PlayLinkVo{
					Id:       groupID,
					SourceId: "manual",
					Name:     siteName,
					IsManual: true,
					LinkList: links,
				})
			} else if r.LineKind == model.LineKindDownload {
				downloadList = append(downloadList, links)
			}
			continue
		}

		if !hasSource {
			continue
		}
		if r.LineKind == model.LineKindPlay {
			siteName := source.Name
			if siteName == "" {
				siteName = r.SourceId
			}
			displayName := shared.BuildDisplaySourceName(siteName, r.GroupName, r.GroupIndex, sourcePlayGroupCounts[r.SourceId])
			groupID := fmt.Sprintf("%s#%d", r.SourceId, r.GroupIndex)
			playList = append(playList, model.PlayLinkVo{
				Id:       groupID,
				SourceId: r.SourceId,
				Name:     displayName,
				LinkList: links,
			})
		} else if r.LineKind == model.LineKindDownload {
			downloadList = append(downloadList, links)
		}
	}

	if err := playlistsAllBroken(mid, len(rows), failed); err != nil {
		return nil, nil, err
	}
	sortPlayListSources(playList, sourceOrderMap)

	return playList, downloadList, nil
}

// BatchGetPlayPlaylistsByMids 批量加载影片播放列表（TVBox 批量组装，无 N+1）。
func BatchGetPlayPlaylistsByMids(mids []int64) (map[int64][]model.PlayLinkVo, map[int64]struct{}, error) {
	result := make(map[int64][]model.PlayLinkVo, len(mids))
	if len(mids) == 0 {
		return result, nil, nil
	}
	rows, err := ListPlayPlaylistsByMids(mids)
	if err != nil {
		return nil, nil, err
	}
	rows, err = appendPeerPlayRows(mids, rows)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return result, nil, nil
	}

	sources, err := repobase.GetEnabledCollectSourceList()
	if err != nil {
		return nil, nil, err
	}
	sourcesByID := make(map[string]model.FilmSource, len(sources))
	sourceOrderMap := make(map[string]int, len(sources))
	for idx, s := range sources {
		sourcesByID[s.Id] = s
		sourceOrderMap[s.Id] = idx
	}

	sourcePlayGroupCounts := make(map[string]int)
	seen := make(map[int64]int)
	failedN := make(map[int64]int)
	for _, r := range rows {
		key := fmt.Sprintf("%d#%s", r.Mid, r.SourceId)
		sourcePlayGroupCounts[key]++
	}

	for _, r := range rows {
		source, hasSource := sourcesByID[r.SourceId]
		var rules []utils.DomainReplaceRule
		if hasSource && source.DomainReplaceRules != "" {
			rules = utils.ParseDomainReplaceRules(source.DomainReplaceRules)
		}

		seen[r.Mid]++
		var links []model.MovieUrlInfo
		if err := json.Unmarshal([]byte(r.Content), &links); err != nil {
			skipBadPlaylist(r, err)
			failedN[r.Mid]++
			continue
		}
		if len(rules) > 0 {
			links = rewriteURLGroup(links, rules)
		}

		if r.SourceId == "manual" {
			siteName := strings.TrimSpace(r.GroupName)
			if siteName == "" {
				siteName = "自定义线路"
			}
			groupID := fmt.Sprintf("manual#%d", r.GroupIndex)
			result[r.Mid] = append(result[r.Mid], model.PlayLinkVo{
				Id:       groupID,
				SourceId: "manual",
				Name:     siteName,
				IsManual: true,
				LinkList: links,
			})
			continue
		}

		if !hasSource {
			continue
		}
		siteName := source.Name
		if siteName == "" {
			siteName = r.SourceId
		}
		key := fmt.Sprintf("%d#%s", r.Mid, r.SourceId)
		displayName := shared.BuildDisplaySourceName(siteName, r.GroupName, r.GroupIndex, sourcePlayGroupCounts[key])
		groupID := fmt.Sprintf("%s#%d", r.SourceId, r.GroupIndex)

		result[r.Mid] = append(result[r.Mid], model.PlayLinkVo{
			Id:       groupID,
			SourceId: r.SourceId,
			Name:     displayName,
			LinkList: links,
		})
	}

	for mid := range result {
		lines := result[mid]
		sortPlayListSources(lines, sourceOrderMap)
		result[mid] = lines
	}

	return result, brokenPlaylistMids(seen, failedN), nil
}

func sortPlayListSources(lines []model.PlayLinkVo, orderMap map[string]int) {
	getOrder := func(s string) int {
		if s == "manual" {
			return -1
		}
		if idx, ok := orderMap[s]; ok {
			return idx
		}
		return 999999
	}
	sort.SliceStable(lines, func(i, j int) bool {
		if oi, oj := getOrder(lines[i].SourceId), getOrder(lines[j].SourceId); oi != oj {
			return oi < oj
		}
		return lines[i].Id < lines[j].Id
	})
}

func rewriteURLGroup(links []model.MovieUrlInfo, rules []utils.DomainReplaceRule) []model.MovieUrlInfo {
	if len(rules) == 0 || links == nil {
		return links
	}
	out := make([]model.MovieUrlInfo, len(links))
	for i, link := range links {
		out[i] = model.MovieUrlInfo{
			Episode:    link.Episode,
			Link:       utils.ApplyDomainReplaceRules(link.Link, rules),
			SourceId:   link.SourceId,
			SourceName: link.SourceName,
		}
	}
	return out
}
