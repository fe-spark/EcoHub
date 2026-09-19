package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider"
	"server/internal/utils"
)

// GetLiveFilmDetail 现场拉取采集源详情并合成播放页结构，不写库、不走 mid 缓存。
func (i *IndexService) GetLiveFilmDetail(sourceID string, sourceMid int64) (model.MovieDetailVo, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || sourceMid <= 0 {
		return model.MovieDetailVo{}, nil
	}
	source := repository.FindCollectSourceById(sourceID)
	if source == nil || !source.State || strings.TrimSpace(source.Uri) == "" {
		return model.MovieDetailVo{}, nil
	}
	details, err := spider.FetchSourceDetails(source.Uri, strconv.FormatInt(sourceMid, 10))
	if err != nil {
		return model.MovieDetailVo{}, err
	}
	var detail *model.MovieDetail
	for idx := range details {
		if details[idx].Id == sourceMid {
			item := details[idx]
			detail = &item
			break
		}
	}
	if detail == nil && len(details) == 1 && (details[0].Id == 0 || details[0].Id == sourceMid) && strings.TrimSpace(details[0].Name) != "" {
		item := details[0]
		detail = &item
	}
	if detail == nil || strings.TrimSpace(detail.Name) == "" {
		return model.MovieDetailVo{}, nil
	}
	detail.Id = 0
	detail.Picture = resolveCMSMediaURL(detail.Picture, source.Uri)
	detail.PictureSlide = resolveCMSMediaURL(detail.PictureSlide, source.Uri)
	snapshot := &model.FilmListSnapshot{SourceId: source.Id}
	res := model.MovieDetailVo{MovieDetail: *detail}
	res.List = buildPrimaryPlaySources(snapshot, detail)
	if source.DomainReplaceRules != "" {
		if rules := utils.ParseDomainReplaceRules(source.DomainReplaceRules); len(rules) > 0 {
			res.PlayList = rewriteURLGroups(res.PlayList, rules)
			res.DownloadList = rewriteURLGroups(res.DownloadList, rules)
		}
	}
	return res, nil
}
type LiveRelateFilmVO struct {
	Id        string `json:"id"`
	SourceId  string `json:"sourceId"`
	SourceMid string `json:"sourceMid"`
	Name      string `json:"name"`
	Picture   string `json:"picture"`
	Remarks   string `json:"remarks"`
	Year      string `json:"year"`
	CName     string `json:"cName"`
	Area      string `json:"area"`
}

// GetLiveRelateFilms 获取当前采集源下同类相关推荐
func (i *IndexService) GetLiveRelateFilms(sourceID string, cid int64, excludeSid int64) ([]LiveRelateFilmVO, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return []LiveRelateFilmVO{}, nil
	}

	source := repository.FindCollectSourceById(sourceID)
	if source == nil || !source.State || strings.TrimSpace(source.Uri) == "" {
		return []LiveRelateFilmVO{}, nil
	}

	cacheKey := fmt.Sprintf("EcoHub:Film:LiveRelate:%s:%d", sourceID, cid)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached []LiveRelateFilmVO
			if json.Unmarshal([]byte(data), &cached) == nil {
				return filterExcludeSid(cached, excludeSid), nil
			}
		}
	}

	// 优先拉取同分类详情，若无结果且 cid > 0 则回退拉取最新推荐
	details, err := spider.FetchSourceCategoryDetails(source.Uri, cid, 1)
	if (err != nil || len(details) == 0) && cid > 0 {
		details, err = spider.FetchSourceCategoryDetails(source.Uri, 0, 1)
	}
	if err != nil {
		return []LiveRelateFilmVO{}, err
	}

	list := make([]LiveRelateFilmVO, 0, len(details))
	for _, item := range details {
		if item.Id <= 0 || strings.TrimSpace(item.Name) == "" {
			continue
		}
		pic := resolveCMSMediaURL(item.Picture, source.Uri)
		sidStr := strconv.FormatInt(item.Id, 10)
		list = append(list, LiveRelateFilmVO{
			Id:        sidStr,
			SourceId:  source.Id,
			SourceMid: sidStr,
			Name:      item.Name,
			Picture:   pic,
			Remarks:   item.MovieDescriptor.Remarks,
			Year:      item.MovieDescriptor.Year,
			CName:     item.MovieDescriptor.CName,
			Area:      item.MovieDescriptor.Area,
		})
	}

	// 缓存全量结果 15 分钟
	if db.Rdb != nil && len(list) > 0 {
		if raw, err := json.Marshal(list); err == nil {
			_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), 15*time.Minute).Err()
		}
	}

	return filterExcludeSid(list, excludeSid), nil
}

func filterExcludeSid(list []LiveRelateFilmVO, excludeSid int64) []LiveRelateFilmVO {
	if len(list) == 0 {
		return []LiveRelateFilmVO{}
	}
	excludeStr := strconv.FormatInt(excludeSid, 10)
	filtered := make([]LiveRelateFilmVO, 0, len(list))
	for _, item := range list {
		if excludeSid > 0 && (item.Id == excludeStr || item.SourceMid == excludeStr) {
			continue
		}
		filtered = append(filtered, item)
		if len(filtered) >= 14 {
			break
		}
	}
	return filtered
}

