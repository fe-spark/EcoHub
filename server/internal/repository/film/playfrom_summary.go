package film

import (
	"encoding/json"
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func BuildPlayFromSummary(filmIndex model.FilmIndex, detail *model.MovieDetail, groupsBySource map[string][]model.PlayLinkVo) string {
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
		if filmIndex.SourceId != "" {
			siteName = findCollectSourceName(filmIndex.SourceId)
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
		return ""
	}
	return strings.Join(playNames, "$$$")
}

func RefreshPlayFromSummaryByIndexes(infos []model.FilmIndex) error {
	if err := RefreshPlayFromSummaryByIndexesTx(db.Mdb, infos); err != nil {
		return err
	}
	ClearProvideListCache()
	return nil
}

func RefreshPlayFromSummaryByIndexesTx(tx *gorm.DB, infos []model.FilmIndex) error {
	if len(infos) == 0 {
		return nil
	}

	orderedInfos := make([]model.FilmIndex, 0, len(infos))
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
	if err := tx.Where("mid IN ?", mids).Find(&detailInfos).Error; err != nil {
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

	playlistGroups, err := loadPlaylistGroupsByInfosTx(tx, orderedInfos)
	if err != nil {
		return err
	}

	summaries := make(map[int64]string, len(orderedInfos))
	for _, info := range orderedInfos {
		var detailPtr *model.MovieDetail
		if detail, ok := detailByMid[info.Mid]; ok {
			detailPtr = &detail
		}
		summaries[info.Mid] = BuildPlayFromSummary(info, detailPtr, playlistGroups[info.Mid])
	}
	return batchUpdatePlayFromSummariesTx(tx, summaries)
}

func batchUpdatePlayFromSummariesTx(tx *gorm.DB, summaries map[int64]string) error {
	if len(summaries) == 0 {
		return nil
	}

	caseExpr := "CASE mid"
	mids := make([]int64, 0, len(summaries))
	args := make([]any, 0, len(summaries)*2)
	for mid, summary := range summaries {
		if mid <= 0 {
			continue
		}
		caseExpr += " WHEN ? THEN ?"
		args = append(args, mid, summary)
		mids = append(mids, mid)
	}
	if len(mids) == 0 {
		return nil
	}
	caseExpr += " ELSE play_from_summary END"

	return tx.Model(&model.FilmIndex{}).
		Where("mid IN ?", mids).
		Update("play_from_summary", clause.Expr{SQL: caseExpr, Vars: args}).Error
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
