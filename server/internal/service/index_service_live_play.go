package service

import (
	"strconv"
	"strings"

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
