package spider

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	filmrepo "server/internal/repository/film"
)

// FinalizeWebDAVCollectRun 执行 WebDAV 线路变更后的标准收尾：刷新源摘要、发布读模型快照、清理详情缓存
func FinalizeWebDAVCollectRun(s model.FilmSource, mids []int64) {
	if len(mids) == 0 {
		return
	}
	_, _, _ = finalizeCollectRun([]model.FilmSource{s}, mids, nil)

	if db.Rdb != nil {
		for _, mid := range mids {
			db.Rdb.Del(db.Cxt, fmt.Sprintf("%s:%d", config.FilmPlayInfoKey, mid))
		}
	}
}

// SyncWebDAVPlaylists 依据当前数据库中的 activeItems 与 WebdavMediaGroup，重新合成并挂载播放列表
func SyncWebDAVPlaylists(s model.FilmSource, cfg model.WebdavConfig) ([]int64, int, error) {
	if db.Mdb == nil {
		return nil, 0, errors.New("数据库未连接")
	}

	var groups []model.WebdavMediaGroup
	db.Mdb.Where("source_id = ? AND global_mid > 0", s.Id).Find(&groups)
	groupByKey := make(map[string]model.WebdavMediaGroup, len(groups))
	for _, g := range groups {
		groupByKey[g.GroupKey] = g
	}

	var activeItems []model.WebdavScanItem
	db.Mdb.Where("source_id = ? AND status IN ('scraped', 'bound')", s.Id).Find(&activeItems)
	itemsByMid := make(map[int64][]model.WebdavScanItem)
	for _, it := range activeItems {
		if g, ok := groupByKey[it.GroupKey]; ok && g.GlobalMid > 0 {
			itemsByMid[g.GlobalMid] = append(itemsByMid[g.GlobalMid], it)
		}
	}

	playFromName := s.Name
	if strings.TrimSpace(cfg.PlayFromName) != "" {
		playFromName = strings.TrimSpace(cfg.PlayFromName)
	}

	var detailsToSave []model.MovieDetail
	for mid, items := range itemsByMid {
		var master model.FilmIndex
		if err := db.Mdb.Where("mid = ?", mid).First(&master).Error; err != nil {
			continue
		}

		if cfg.MediaType == "tv" {
			sort.Slice(items, func(i, j int) bool {
				if items[i].Season != items[j].Season {
					return items[i].Season < items[j].Season
				}
				return items[i].Episode < items[j].Episode
			})
		} else {
			sort.Slice(items, func(i, j int) bool {
				return items[i].RelPath < items[j].RelPath
			})
		}

		hasMultipleSeasons := false
		firstSeason := -1
		for _, it := range items {
			if cfg.MediaType == "tv" {
				if firstSeason == -1 {
					firstSeason = it.Season
				} else if it.Season != firstSeason {
					hasMultipleSeasons = true
					break
				}
			}
		}

		var links []model.MovieUrlInfo
		for _, it := range items {
			epStr := ""
			if cfg.MediaType == "tv" {
				if hasMultipleSeasons || it.Season > 1 {
					epStr = fmt.Sprintf("S%02dE%02d", it.Season, it.Episode)
				} else {
					epStr = fmt.Sprintf("第%d集", it.Episode)
				}
			} else {
				epStr = "正片"
			}
			encodedPath := base64.RawURLEncoding.EncodeToString([]byte(it.RelPath))
			link := fmt.Sprintf("wdv://%s/%s", s.Id, encodedPath)
			links = append(links, model.MovieUrlInfo{
				Episode: epStr,
				Link:    link,
			})
		}

		if len(links) > 0 {
			detail := model.MovieDetail{
				Id:       master.Mid,
				Name:     master.Name,
				Pid:      master.Pid,
				Cid:      master.Cid,
				PlayFrom: []string{playFromName},
				PlayList: [][]model.MovieUrlInfo{links},
				MovieDescriptor: model.MovieDescriptor{
					DbId:  master.DbId,
					CName: master.CName,
					Year:  strconv.FormatInt(master.Year, 10),
				},
			}
			detailsToSave = append(detailsToSave, detail)
		}
	}

	var affectedMids []int64
	if len(detailsToSave) > 0 {
		changedMids, err := filmrepo.SaveSitePlayList(s.Id, detailsToSave)
		if err != nil {
			log.Printf("[WebDAV Sync] 站点 %s SaveSitePlayList 失败: %v\n", s.Name, err)
			return nil, 0, err
		}
		affectedMids = append(affectedMids, changedMids...)
	}

	FinalizeWebDAVCollectRun(s, affectedMids)
	return uniqueInt64s(affectedMids), len(detailsToSave), nil
}
