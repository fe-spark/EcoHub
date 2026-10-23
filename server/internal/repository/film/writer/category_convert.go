package writer

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository/support"
	"server/internal/utils"
)

var ErrCategoryNotMapped = errors.New("影片未映射到分类")

func IsCategoryNotMapped(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrCategoryNotMapped) || strings.Contains(err.Error(), "影片未映射到分类")
}

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
	if result.Pid == 0 && result.CName != "" {
		result.Pid = support.ResolveRootCategoryIDByCName(result.CName)
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

	if sourceCid > 0 {
		if row, ok := support.GetSourceCategoryInfo(sourceId, sourceCid); ok {
			if row.ParentSourceTypeId == 0 {
				name := strings.TrimSpace(row.RawName)
				if name != "" {
					return name
				}
			}
			if row.ParentSourceTypeId > 0 {
				if parent, ok := support.GetSourceCategoryInfo(sourceId, row.ParentSourceTypeId); ok {
					name := strings.TrimSpace(parent.RawName)
					if name != "" {
						return name
					}
				}
			}
		}
	}

	if sourcePid > 0 {
		if root, ok := support.GetSourceCategoryInfo(sourceId, sourcePid); ok {
			name := strings.TrimSpace(root.RawName)
			if name != "" {
				return name
			}
		}
	}

	return fallback
}

func resolveSourceRootTypeID(sourceId string, sourcePid int64, sourceCid int64) int64 {
	if strings.TrimSpace(sourceId) == "" {
		return 0
	}
	if sourceCid > 0 {
		current := sourceCid
		for step := range [5]int{} {
			row, ok := support.GetSourceCategoryInfo(sourceId, current)
			if !ok {
				if step == 0 && sourcePid > 0 {
					return sourcePid
				}
				return current
			}
			if row.ParentSourceTypeId <= 0 {
				return current
			}
			current = row.ParentSourceTypeId
		}
		return current
	}
	if sourcePid > 0 {
		return sourcePid
	}
	return sourceCid
}

type normalizedSearchMeta struct {
	Score        float64
	UpdateStamp  int64
	UpdateReason string
	Year         int64
	Area         string
	Language     string
	ClassTag     string
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
	if sourceCid <= 0 && sourcePid > 0 {
		sourceCid = sourcePid
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
	if result.Pid == 0 && detail.Pid > 0 {
		result.Pid = support.GetRootId(detail.Pid)
	}
	if result.Pid == 0 && detail.CName != "" {
		result.Pid = support.ResolveRootCategoryIDByCName(detail.CName)
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

func normalizeSearchMetadata(sourceId string, detail model.MovieDetail, category resolvedSearchCategory) (normalizedSearchMeta, error) {
	score, _ := strconv.ParseFloat(detail.DbScore, 64)
	year := parseYear(detail.ReleaseDate)
	if year == 0 {
		year = parseYear(detail.Year)
	}
	updateStamp, err := utils.ParseCollectUpdateTime(detail.UpdateTime)
	if err != nil {
		// 解析失败不阻断入库；用当前时间兜底，避免整条元数据失败
		// 可能影响按 update_stamp 排序的「最新」顺序，属可接受降级
		log.Printf("[FilmWrite] 更新时间解析失败，使用当前时间兜底 name=%s id=%d updateTime=%q err=%v",
			detail.Name, detail.Id, detail.UpdateTime, err)
		updateStamp = time.Now().Unix()
	}

	updateReason := "新片入库"
	if strings.TrimSpace(sourceId) == "manual" {
		updateReason = "后台修改"
	}

	finalArea := support.NormalizeArea(detail.Area)
	finalLang := support.NormalizeLanguage(detail.Language)
	mainCategoryName := support.GetMainCategoryName(category.Pid)

	return normalizedSearchMeta{
		Score:        score,
		UpdateStamp:  updateStamp,
		UpdateReason: updateReason,
		Year:         year,
		Area:         finalArea,
		Language:     finalLang,
		ClassTag:     support.CleanPlotTags(detail.ClassTag, finalArea, mainCategoryName, category.CName),
	}, nil
}

func buildFilmIndex(sourceId string, detail model.MovieDetail, category resolvedSearchCategory, meta normalizedSearchMeta, categoryVersion string, ruleVersion string) model.FilmIndex {
	return model.FilmIndex{
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		FilmIndexIdentity: model.FilmIndexIdentity{
			FirstSourceId: sourceId,
			DbId:          detail.DbId,
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
			UpdateReason:       meta.UpdateReason,
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
			Writer:             detail.Writer,
			Blurb:              detail.Blurb,
			Content:            detail.Content,
			ReleaseDate:        firstISOReleaseDate(detail.ReleaseDate),
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
	if category.Pid <= 0 {
		return model.FilmIndex{}, fmt.Errorf("%w: %s", ErrCategoryNotMapped, strings.TrimSpace(detail.Name))
	}
	meta, err := normalizeSearchMetadata(sourceId, detail, category)
	if err != nil {
		return model.FilmIndex{}, err
	}
	return buildFilmIndex(sourceId, detail, category, meta, categoryVersion, ruleVersion), nil
}
