package writer

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"server/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func computePlaylistContentHash(lineKind string, groupIndex int, groupName string, content string) string {
	raw := lineKind + "#" + strconv.Itoa(groupIndex) + "#" + groupName + "#" + content
	h := md5.Sum([]byte(raw))
	return hex.EncodeToString(h[:])
}

func playlistIdentity(line model.FilmSourcePlaylist) string {
	return line.LineKind + "#" + strconv.Itoa(line.GroupIndex)
}

func isPlaylistSignatureIdentical(oldLines, newLines []model.FilmSourcePlaylist) bool {
	if len(oldLines) != len(newLines) {
		return false
	}
	oldByKey := make(map[string]model.FilmSourcePlaylist, len(oldLines))
	for _, line := range oldLines {
		oldByKey[playlistIdentity(line)] = line
	}
	for _, line := range newLines {
		old, ok := oldByKey[playlistIdentity(line)]
		if !ok || old.ContentHash != line.ContentHash || old.GroupName != line.GroupName {
			return false
		}
	}
	return true
}

func playlistMetaQuery(tx *gorm.DB) *gorm.DB {
	return tx.Select("mid", "source_id", "line_kind", "group_index", "group_name", "episode_count", "last_episode", "content_hash")
}

func playlistUpsertClause() clause.OnConflict {
	return clause.OnConflict{
		Columns: []clause.Column{
			{Name: "mid"},
			{Name: "source_id"},
			{Name: "line_kind"},
			{Name: "group_index"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"group_name", "episode_count", "last_episode", "content_hash", "content", "updated_at",
		}),
	}
}

func playHashesChanged(oldLines, newLines []model.FilmSourcePlaylist) bool {
	oldPlayMap := make(map[int]string)
	for _, line := range oldLines {
		if line.LineKind == "play" {
			oldPlayMap[line.GroupIndex] = line.ContentHash
		}
	}
	newPlayMap := make(map[int]string)
	for _, line := range newLines {
		if line.LineKind == "play" {
			newPlayMap[line.GroupIndex] = line.ContentHash
		}
	}
	if len(oldPlayMap) != len(newPlayMap) {
		return true
	}
	for idx, hash := range newPlayMap {
		if oldPlayMap[idx] != hash {
			return true
		}
	}
	return false
}

func vanishedPlaylists(oldLines, newLines []model.FilmSourcePlaylist) []model.FilmSourcePlaylist {
	newByKey := make(map[string]struct{}, len(newLines))
	for _, line := range newLines {
		newByKey[playlistIdentity(line)] = struct{}{}
	}
	vanished := make([]model.FilmSourcePlaylist, 0)
	for _, old := range oldLines {
		if _, ok := newByKey[playlistIdentity(old)]; ok {
			continue
		}
		vanished = append(vanished, old)
	}
	sortPlaylists(vanished)
	return vanished
}

func sortPlaylists(lines []model.FilmSourcePlaylist) {
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].Mid != lines[j].Mid {
			return lines[i].Mid < lines[j].Mid
		}
		if lines[i].SourceId != lines[j].SourceId {
			return lines[i].SourceId < lines[j].SourceId
		}
		if lines[i].LineKind != lines[j].LineKind {
			return lines[i].LineKind < lines[j].LineKind
		}
		return lines[i].GroupIndex < lines[j].GroupIndex
	})
}

func deletePlaylistTx(tx *gorm.DB, line model.FilmSourcePlaylist) error {
	return tx.Where(
		"mid = ? AND source_id = ? AND line_kind = ? AND group_index = ?",
		line.Mid, line.SourceId, line.LineKind, line.GroupIndex,
	).Delete(&model.FilmSourcePlaylist{}).Error
}

func saveStationPlaylistsTx(tx *gorm.DB, mid int64, sourceId string, newLines []model.FilmSourcePlaylist) (bool, bool, error) {
	var oldLines []model.FilmSourcePlaylist
	if err := playlistMetaQuery(tx).Where("mid = ? AND source_id = ?", mid, sourceId).Find(&oldLines).Error; err != nil {
		return false, false, err
	}
	if isPlaylistSignatureIdentical(oldLines, newLines) {
		return false, false, nil
	}
	playChanged := playHashesChanged(oldLines, newLines)
	// 只删本站消失的线路，按主键等值删除。范围删除会挡住其它片子插入。
	for _, old := range vanishedPlaylists(oldLines, newLines) {
		if err := deletePlaylistTx(tx, old); err != nil {
			return false, false, err
		}
	}
	if len(newLines) == 0 {
		return true, playChanged, nil
	}
	lines := append([]model.FilmSourcePlaylist(nil), newLines...)
	sortPlaylists(lines)
	err := tx.Clauses(playlistUpsertClause()).Create(&lines).Error
	return true, playChanged, err
}

func buildPlaylistsFromDetail(mid int64, sourceID string, detail model.MovieDetail) []model.FilmSourcePlaylist {
	lines := make([]model.FilmSourcePlaylist, 0, len(detail.PlayList)+len(detail.DownloadList))

	for i, group := range detail.PlayList {
		if len(group) == 0 {
			continue
		}
		rawName := ""
		if i < len(detail.PlayFrom) {
			rawName = strings.TrimSpace(detail.PlayFrom[i])
		}
		contentBytes, _ := json.Marshal(group)
		contentStr := string(contentBytes)
		lastEp := ""
		if len(group) > 0 {
			lastEp = strings.TrimSpace(group[len(group)-1].Episode)
		}
		lines = append(lines, model.FilmSourcePlaylist{
			Mid:          mid,
			SourceId:     sourceID,
			LineKind:     "play",
			GroupIndex:   i,
			GroupName:    rawName,
			EpisodeCount: len(group),
			LastEpisode:  lastEp,
			Content:      contentStr,
			ContentHash:  computePlaylistContentHash("play", i, rawName, contentStr),
		})
	}

	for i, group := range detail.DownloadList {
		if len(group) == 0 {
			continue
		}
		rawName := strings.TrimSpace(detail.DownFrom)
		contentBytes, _ := json.Marshal(group)
		contentStr := string(contentBytes)
		lastEp := ""
		if len(group) > 0 {
			lastEp = strings.TrimSpace(group[len(group)-1].Episode)
		}
		lines = append(lines, model.FilmSourcePlaylist{
			Mid:          mid,
			SourceId:     sourceID,
			LineKind:     "download",
			GroupIndex:   i,
			GroupName:    rawName,
			EpisodeCount: len(group),
			LastEpisode:  lastEp,
			Content:      contentStr,
			ContentHash:  computePlaylistContentHash("download", i, rawName, contentStr),
		})
	}

	return lines
}
