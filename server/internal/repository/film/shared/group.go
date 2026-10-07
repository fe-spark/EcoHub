package shared

import (
	"encoding/json"
	"fmt"
	"strings"

	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

// BuildDisplaySourceName 统一站点播放源展示名。
// 单线路仅展示站点名，多线路优先使用“站点名-原始线路名”，缺失时降级为“站点名-线路N”。
func BuildDisplaySourceName(siteName, rawName string, index, total int) string {
	siteName = strings.TrimSpace(siteName)
	rawName = strings.TrimSpace(rawName)

	if siteName == "" {
		if rawName != "" {
			return rawName
		}
		if total > 1 {
			return fmt.Sprintf("播放源%d", index+1)
		}
		return "默认源"
	}

	if total <= 1 {
		return siteName
	}
	if rawName != "" {
		return fmt.Sprintf("%s-%s", siteName, rawName)
	}
	return fmt.Sprintf("%s-线路%d", siteName, index+1)
}

// BuildPlayGroupID 为单个可播放分组构建稳定 ID。
// 单线路时直接复用站点 ID，多线路时追加线路标识，确保同站不同线路可区分。
func BuildPlayGroupID(sourceID, rawName string, index, total int) string {
	sourceID = strings.TrimSpace(sourceID)
	rawName = strings.TrimSpace(rawName)
	if sourceID == "" {
		if rawName != "" {
			return rawName
		}
		return fmt.Sprintf("group_%d", index)
	}
	if total <= 1 {
		return sourceID
	}
	if rawName != "" {
		return fmt.Sprintf("%s::%s", sourceID, rawName)
	}
	return fmt.Sprintf("%s::group_%d", sourceID, index)
}

func LoadPlaylistGroupsByInfosTx(tx *gorm.DB, infos []model.FilmIndex) (map[int64]map[string][]model.PlayLinkVo, error) {
	result := make(map[int64]map[string][]model.PlayLinkVo, len(infos))
	if len(infos) == 0 {
		return result, nil
	}
	mids := make([]int64, 0, len(infos))
	for _, info := range infos {
		if info.Mid > 0 {
			mids = append(mids, info.Mid)
			result[info.Mid] = make(map[string][]model.PlayLinkVo)
		}
	}
	if len(mids) == 0 {
		return result, nil
	}

	var playlists []model.FilmSourcePlaylist
	if err := tx.Where("mid IN ? AND line_kind = ?", mids, "play").
		Order("group_index ASC").Find(&playlists).Error; err != nil {
		return nil, err
	}

	rowsByMidSource := make(map[int64]map[string][]model.FilmSourcePlaylist)
	for _, pl := range playlists {
		if rowsByMidSource[pl.Mid] == nil {
			rowsByMidSource[pl.Mid] = make(map[string][]model.FilmSourcePlaylist)
		}
		rowsByMidSource[pl.Mid][pl.SourceId] = append(rowsByMidSource[pl.Mid][pl.SourceId], pl)
	}

	sourceMap := make(map[string]model.FilmSource)
	for _, s := range support.GetCollectSourceList() {
		sourceMap[s.Id] = s
	}

	for mid, bySource := range rowsByMidSource {
		for sourceID, rows := range bySource {
			sName := sourceID
			if s, ok := sourceMap[sourceID]; ok && s.Name != "" {
				sName = s.Name
			}
			groups := PlayGroupsFromPlaylistRows(sourceID, sName, rows)
			if len(groups) > 0 {
				result[mid][sourceID] = groups
			}
		}
	}
	return result, nil
}

func PlayGroupsFromPlaylistRows(siteID, siteName string, matched []model.FilmSourcePlaylist) []model.PlayLinkVo {
	if len(matched) == 0 {
		return nil
	}
	groups := make([]model.PlayLinkVo, 0, len(matched))
	for _, playlist := range matched {
		var links []model.MovieUrlInfo
		if err := json.Unmarshal([]byte(playlist.Content), &links); err != nil || len(links) == 0 {
			continue
		}
		displayName := BuildDisplaySourceName(siteName, playlist.GroupName, playlist.GroupIndex, len(matched))
		groupID := BuildPlayGroupID(siteID, playlist.GroupName, playlist.GroupIndex, len(matched))
		groups = append(groups, model.PlayLinkVo{
			Id:       groupID,
			SourceId: siteID,
			Name:     displayName,
			LinkList: links,
		})
	}
	return groups
}
