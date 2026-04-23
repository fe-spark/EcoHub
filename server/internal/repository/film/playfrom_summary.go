package film

import (
	"encoding/json"
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

func BuildPlayFromSummary(search model.SearchInfo, detail *model.MovieDetail, groupsBySource map[string][]model.PlayLinkVo) string {
	playNames := make([]string, 0)
	seen := make(map[string]struct{})
	appendName := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		playNames = append(playNames, name)
	}

	if detail != nil {
		siteName := ""
		if search.SourceId != "" {
			siteName = findCollectSourceName(search.SourceId)
		}
		for index, links := range detail.PlayList {
			if len(links) == 0 {
				continue
			}
			rawName := ""
			if index >= 0 && index < len(detail.PlayFrom) {
				rawName = detail.PlayFrom[index]
			}
			appendName(BuildDisplaySourceName(siteName, rawName, index, len(detail.PlayList)))
		}
	}

	if len(groupsBySource) > 0 {
		for _, source := range support.GetCollectSourceList() {
			if source.Grade != model.SlaveCollect || !source.State {
				continue
			}
			groups := groupsBySource[source.Id]
			for _, group := range groups {
				appendName(group.Name)
			}
		}
	}

	if len(playNames) == 0 {
		return config.PlayFormCloud
	}
	return strings.Join(playNames, "$$$")
}

func RefreshPlayFromSummaryBySearchInfos(infos []model.SearchInfo) error {
	if len(infos) == 0 {
		return nil
	}

	orderedInfos := make([]model.SearchInfo, 0, len(infos))
	seenMid := make(map[int64]struct{}, len(infos))
	for _, info := range infos {
		if info.Mid <= 0 {
			continue
		}
		if _, ok := seenMid[info.Mid]; ok {
			continue
		}
		seenMid[info.Mid] = struct{}{}
		orderedInfos = append(orderedInfos, info)
	}
	if len(orderedInfos) == 0 {
		return nil
	}

	mids := make([]int64, 0, len(orderedInfos))
	for _, info := range orderedInfos {
		mids = append(mids, info.Mid)
	}

	var detailInfos []model.MovieDetailInfo
	if err := db.Mdb.Where("mid IN ?", mids).Find(&detailInfos).Error; err != nil {
		return err
	}
	detailByMid := make(map[int64]model.MovieDetail, len(detailInfos))
	for _, item := range detailInfos {
		var detail model.MovieDetail
		if err := json.Unmarshal([]byte(item.Content), &detail); err != nil {
			continue
		}
		detailByMid[item.Mid] = detail
	}

	playlistGroups, err := loadPlaylistGroupsByInfos(orderedInfos)
	if err != nil {
		return err
	}

	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		for _, info := range orderedInfos {
			var detailPtr *model.MovieDetail
			if detail, ok := detailByMid[info.Mid]; ok {
				detailPtr = &detail
			}
			summary := BuildPlayFromSummary(info, detailPtr, playlistGroups[info.Mid])
			if err := tx.Model(&model.SearchInfo{}).
				Where("mid = ?", info.Mid).
				Update("play_from_summary", summary).Error; err != nil {
				return err
			}
		}
		ClearProvideListCache()
		return nil
	})
}

func ClearProvideListCache() {
	pattern := config.TVBoxList + ":*"
	iter := db.Rdb.Scan(db.Cxt, 0, pattern, config.MaxScanCount).Iterator()
	for iter.Next(db.Cxt) {
		db.Rdb.Del(db.Cxt, iter.Val())
	}
}

func findCollectSourceName(sourceID string) string {
	for _, source := range support.GetCollectSourceList() {
		if source.Id == sourceID {
			return source.Name
		}
	}
	return ""
}
