package writer

import (
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"
	"server/internal/utils"
)

type resolvedSearchCategory struct {
	Pid              int64
	Cid              int64
	CName            string
	OriginalCategory string
	PKey             string
	CKey             string
}

func resolveLocalCategory(pid int64, cid int64, cName string) resolvedSearchCategory {
	result := resolvedSearchCategory{CName: strings.TrimSpace(cName)}
	if cid > 0 {
		result.Cid = cid
	}
	if result.Cid > 0 {
		result.Pid = support.GetRootId(result.Cid)
	}
	if result.Pid == 0 && pid > 0 {
		result.Pid = pid
	}
	if result.Pid > 0 && result.Cid > 0 && result.CName == "" {
		result.CName = support.GetCategoryNameById(result.Cid)
	}
	if result.Pid > 0 && result.PKey == "" {
		result.PKey = support.GetCategoryStableKeyByID(result.Pid)
	}
	if result.Cid > 0 {
		result.CKey = support.GetCategoryStableKeyByID(result.Cid)
	}
	return result
}

func resolveOriginalCategoryName(sourceId string, sourcePid int64, sourceCid int64, fallback string) string {
	fallback = strings.TrimSpace(fallback)
	if strings.TrimSpace(sourceId) == "manual" {
		return fallback
	}

	if sourcePid > 0 {
		var root model.SourceCategory
		if err := db.Mdb.Select("raw_name").Where("source_id = ? AND source_type_id = ?", sourceId, sourcePid).First(&root).Error; err == nil {
			name := strings.TrimSpace(root.RawName)
			if name != "" {
				return name
			}
		}
	}

	if sourceCid > 0 {
		var row model.SourceCategory
		if err := db.Mdb.Select("raw_name", "parent_source_type_id").Where("source_id = ? AND source_type_id = ?", sourceId, sourceCid).First(&row).Error; err == nil {
			if row.ParentSourceTypeId == 0 {
				name := strings.TrimSpace(row.RawName)
				if name != "" {
					return name
				}
			}
			if row.ParentSourceTypeId > 0 {
				var parent model.SourceCategory
				if err := db.Mdb.Select("raw_name").Where("source_id = ? AND source_type_id = ?", sourceId, row.ParentSourceTypeId).First(&parent).Error; err == nil {
					name := strings.TrimSpace(parent.RawName)
					if name != "" {
						return name
					}
				}
			}
		}
	}

	return fallback
}

func resolveSourceRootTypeID(sourceId string, sourcePid int64, sourceCid int64) int64 {
	if strings.TrimSpace(sourceId) == "" {
		return 0
	}
	if sourcePid > 0 {
		return sourcePid
	}
	if sourceCid <= 0 {
		return 0
	}

	current := sourceCid
	for range [5]int{} {
		var row model.SourceCategory
		if err := db.Mdb.Select("parent_source_type_id").Where("source_id = ? AND source_type_id = ?", sourceId, current).First(&row).Error; err != nil {
			return current
		}
		if row.ParentSourceTypeId <= 0 {
			return current
		}
		current = row.ParentSourceTypeId
	}
	return current
}

type normalizedSearchMeta struct {
	Score       float64
	UpdateStamp int64
	Year        int64
	Area        string
	Language    string
	ClassTag    string
}

func resolveSearchCategory(sourceId string, detail model.MovieDetail) resolvedSearchCategory {
	if strings.TrimSpace(sourceId) == "manual" {
		category := resolveLocalCategory(detail.Pid, detail.Cid, detail.CName)
		category.OriginalCategory = strings.TrimSpace(detail.CName)
		return category
	}

	sourceCid := detail.Cid
	sourcePid := detail.Pid
	if detail.RawCid > 0 {
		sourceCid = detail.RawCid
	}
	if detail.RawPid > 0 {
		sourcePid = detail.RawPid
	}

	result := resolvedSearchCategory{CName: strings.TrimSpace(detail.CName)}
	result.OriginalCategory = resolveOriginalCategoryName(sourceId, sourcePid, sourceCid, detail.CName)
	rootSourceTypeID := resolveSourceRootTypeID(sourceId, sourcePid, sourceCid)
	result.PKey = support.BuildSourceCategoryKey(sourceId, rootSourceTypeID)
	result.CKey = support.BuildSourceCategoryKey(sourceId, sourceCid)
	result.Cid = support.GetLocalCategoryId(sourceId, sourceCid)
	if result.Cid > 0 {
		result.Pid = support.GetRootId(result.Cid)
	}
	if result.Pid == 0 {
		result.Pid = support.GetRootId(support.GetLocalCategoryId(sourceId, sourcePid))
	}
	if result.Pid > 0 && result.Cid == 0 && result.CName != "" {
		var category model.Category
		if err := db.Mdb.Where("pid = ? AND name = ?", result.Pid, result.CName).First(&category).Error; err == nil {
			result.Cid = category.Id
		}
	}
	if result.Pid > 0 && result.CName == "" {
		result.CName = support.GetCategoryNameById(result.Pid)
	}
	if result.PKey == "" && result.Pid > 0 {
		result.PKey = support.GetCategoryStableKeyByID(result.Pid)
	}
	if result.CKey == "" && result.Cid > 0 {
		result.CKey = support.GetCategoryStableKeyByID(result.Cid)
	}
	return result
}

func normalizeSearchMetadata(detail model.MovieDetail, category resolvedSearchCategory) (normalizedSearchMeta, error) {
	score, _ := strconv.ParseFloat(detail.DbScore, 64)
	year, err := strconv.ParseInt(regexp.MustCompile(`[1-9][0-9]{3}`).FindString(detail.ReleaseDate), 10, 64)
	if err != nil {
		year = 0
	}
	updateStamp, err := utils.ParseCollectUpdateTime(detail.UpdateTime)
	if err != nil {
		// 解析失败不阻断入库；用当前时间兜底，避免整条元数据失败
		// 可能影响按 update_stamp 排序的「最新」顺序，属可接受降级
		log.Printf("[FilmWrite] 更新时间解析失败，使用当前时间兜底 name=%s id=%d updateTime=%q err=%v",
			detail.Name, detail.Id, detail.UpdateTime, err)
		updateStamp = time.Now().Unix()
	}

	finalArea := support.NormalizeArea(detail.Area)
	finalLang := support.NormalizeLanguage(detail.Language)
	mainCategoryName := support.GetMainCategoryName(category.Pid)

	return normalizedSearchMeta{
		Score:       score,
		UpdateStamp: updateStamp,
		Year:        year,
		Area:        finalArea,
		Language:    finalLang,
		ClassTag:    support.CleanPlotTags(detail.ClassTag, finalArea, mainCategoryName, category.CName),
	}, nil
}

func buildFilmIndex(sourceId string, detail model.MovieDetail, category resolvedSearchCategory, meta normalizedSearchMeta, categoryVersion string, ruleVersion string) model.FilmIndex {
	return model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid:        detail.Id,
			ContentKey: shared.BuildContentKey(detail),
			SourceId:   sourceId,
			DbId:       detail.DbId,
		},
		FilmIndexCategory: model.FilmIndexCategory{
			Cid:              category.Cid,
			Pid:              category.Pid,
			RootCategoryKey:  category.PKey,
			CategoryKey:      category.CKey,
			OriginalCategory: category.OriginalCategory,
			CName:            category.CName,
		},
		FilmIndexContent: model.FilmIndexContent{
			SeriesKey:          utils.BuildSeriesKey(detail.Name, detail.SubTitle),
			Name:               detail.Name,
			SubTitle:           detail.SubTitle,
			ClassTag:           meta.ClassTag,
			Area:               meta.Area,
			Language:           meta.Language,
			Year:               meta.Year,
			Initial:            detail.Initial,
			Score:              meta.Score,
			UpdateStamp:        meta.UpdateStamp,
			Hits:               detail.Hits,
			State:              detail.State,
			Remarks:            detail.Remarks,
			Picture:            detail.Picture,
			PictureSlide:       detail.PictureSlide,
			CustomPicture:      detail.CustomPicture,
			CustomPictureSlide: detail.CustomPictureSlide,
			IsCustomPicture:    detail.IsCustomPicture,
			Actor:              detail.Actor,
			Director:           detail.Director,
			Blurb:              detail.Blurb,
		},
		FilmIndexVersion: model.FilmIndexVersion{
			CollectStamp:    detail.AddTime,
			CategoryVersion: categoryVersion,
			RuleVersion:     ruleVersion,
		},
	}
}

func ConvertFilmIndex(sourceId string, detail model.MovieDetail, categoryVersion string, ruleVersion string) (model.FilmIndex, error) {
	category := resolveSearchCategory(sourceId, detail)
	meta, err := normalizeSearchMetadata(detail, category)
	if err != nil {
		return model.FilmIndex{}, err
	}
	return buildFilmIndex(sourceId, detail, category, meta, categoryVersion, ruleVersion), nil
}
