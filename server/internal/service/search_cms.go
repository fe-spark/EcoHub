package service

import (
	"errors"
	"log"
	"net/url"
	"strconv"
	"strings"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmshared "server/internal/repository/film/shared"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider"
)

var (
	findCollectSourceById = repository.FindCollectSourceById
	searchSourceList      = spider.SearchSourceList
)

// SearchFilmResult 搜索接口业务结果。
type SearchFilmResult struct {
	List    []model.MovieBasicInfo
	Sources []model.SearchSourceTab
	Error   string
}

// SearchFilm 聚合走本地快照；指定 source 时只打该采集源 CMS 搜索，不写库。
func (i *IndexService) SearchFilm(keyword, sourceID, sortField string, page *dto.Page) SearchFilmResult {
	keyword = strings.TrimSpace(keyword)
	sourceID = strings.TrimSpace(sourceID)
	if page == nil {
		page = &dto.Page{Current: 1, PageSize: 12}
	}
	if page.Current <= 0 {
		page.Current = 1
	}
	sources, masterID := buildSearchSourceTabs()
	out := SearchFilmResult{
		List:    []model.MovieBasicInfo{},
		Sources: sources,
	}
	if keyword == "" {
		return out
	}
	if sourceID == "" {
		list := i.SearchFilmInfoWithSort(keyword, sortField, page)
		attachSearchSource(list, masterID, 0)
		out.List = list
		setSearchSourceCount(out.Sources, "", page.Total)
		return out
	}
	list, cmsPage, errMsg := searchCollectSourceCMS(sourceID, keyword, page.Current)
	applyCMSPage(page, cmsPage, len(list))
	out.List = list
	out.Error = errMsg
	setSearchSourceCount(out.Sources, sourceID, page.Total)
	return out
}

func buildSearchSourceTabs() ([]model.SearchSourceTab, string) {
	sources := repository.GetEnabledCollectSourceList()
	tabs := make([]model.SearchSourceTab, 0, len(sources)+1)
	tabs = append(tabs, model.SearchSourceTab{Id: "", Name: "聚合"})
	masterID := ""
	for _, source := range sources {
		name := strings.TrimSpace(source.Name)
		if name == "" {
			name = source.Id
		}
		if source.Grade == model.MasterCollect {
			if masterID == "" {
				masterID = source.Id
			}
			name = "主站"
			if strings.TrimSpace(source.Name) != "" {
				name = source.Name
			}
		}
		tabs = append(tabs, model.SearchSourceTab{Id: source.Id, Name: name})
	}
	if masterID == "" && len(tabs) > 1 {
		masterID = tabs[1].Id
	}
	return tabs, masterID
}

func setSearchSourceCount(tabs []model.SearchSourceTab, sourceID string, total int) {
	for i := range tabs {
		if tabs[i].Id == sourceID {
			tabs[i].Count = total
			return
		}
	}
}

func attachSearchSource(list []model.MovieBasicInfo, sourceID string, fallbackSourceMid int64) {
	for i := range list {
		if list[i].SourceId == "" {
			list[i].SourceId = sourceID
		}
		if list[i].SourceMid <= 0 && fallbackSourceMid > 0 {
			list[i].SourceMid = fallbackSourceMid
		}
	}
}

func formatCMSSearchError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, spider.ErrCMSSearchUnsupported) {
		detail := strings.TrimSpace(strings.TrimPrefix(err.Error(), spider.ErrCMSSearchUnsupported.Error()))
		detail = strings.TrimLeft(detail, ": ")
		if detail != "" {
			return detail
		}
		return "暂不支持搜索"
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		return "源站超时"
	case strings.Contains(lower, "too many requests") || strings.Contains(lower, "status=429"):
		return "源站限流"
	case strings.Contains(lower, "response is empty"):
		return "源站无响应"
	case strings.Contains(lower, "invalid character") || strings.Contains(lower, "unmarshal"):
		return "源站返回异常"
	default:
		return "源站搜索失败"
	}
}

func searchCollectSourceCMS(sourceID, keyword string, current int) ([]model.MovieBasicInfo, model.FilmListPage, string) {
	source := findCollectSourceById(sourceID)
	if source == nil || !source.State || strings.TrimSpace(source.Uri) == "" {
		return []model.MovieBasicInfo{}, model.FilmListPage{}, "源站不可用"
	}
	pageData, err := searchSourceList(source.Uri, keyword, current)
	if err != nil {
		msg := formatCMSSearchError(err)
		log.Printf("[SearchFilm] 源站搜索失败 source=%s(%s) keyword=%q err=%v", source.Name, source.Id, keyword, err)
		return []model.MovieBasicInfo{}, model.FilmListPage{}, msg
	}
	sourceMids := make([]int64, 0, len(pageData.List))
	for _, item := range pageData.List {
		if item.VodID > 0 {
			sourceMids = append(sourceMids, item.VodID)
		}
	}
	localBySourceMid, snapByMid := resolveCMSLocalCards(source, sourceMids)
	fillMissingCMSSearchPictures(source.Uri, pageData.List, localBySourceMid, snapByMid)
	list := make([]model.MovieBasicInfo, 0, len(pageData.List))
	for _, item := range pageData.List {
		if strings.TrimSpace(item.VodName) == "" {
			continue
		}
		mid := localBySourceMid[item.VodID]
		card := model.MovieBasicInfo{
			Id:        mid,
			Name:      item.VodName,
			CName:     item.TypeName,
			Picture:   resolveCMSMediaURL(item.VodPic, source.Uri),
			Remarks:   item.VodRemarks,
			SourceId:  source.Id,
			SourceMid: item.VodID,
		}
		if snap, ok := snapByMid[mid]; ok {
			applyLocalSnapshotToCMSCard(&card, snap)
		}
		list = append(list, card)
	}
	return list, pageData, ""
}

func fillMissingCMSSearchPictures(uri string, list []model.FilmList, localBySourceMid map[int64]int64, snapByMid map[int64]model.FilmListSnapshot) {
	if len(list) == 0 || strings.TrimSpace(uri) == "" {
		return
	}
	ids := make([]string, 0, len(list))
	seen := make(map[int64]struct{}, len(list))
	for _, item := range list {
		if item.VodID <= 0 || strings.TrimSpace(item.VodPic) != "" {
			continue
		}
		if mid, ok := localBySourceMid[item.VodID]; ok {
			if snap, exists := snapByMid[mid]; exists && strings.TrimSpace(snap.DisplayPicture()) != "" {
				continue
			}
		}
		if _, ok := seen[item.VodID]; ok {
			continue
		}
		seen[item.VodID] = struct{}{}
		ids = append(ids, strconv.FormatInt(item.VodID, 10))
	}
	if len(ids) == 0 {
		return
	}
	details, err := spider.FetchSourceDetails(uri, strings.Join(ids, ","))
	if err != nil || len(details) == 0 {
		return
	}
	picByID := make(map[int64]string, len(details))
	for _, detail := range details {
		pic := strings.TrimSpace(detail.DisplayPicture())
		if detail.Id <= 0 || pic == "" {
			continue
		}
		picByID[detail.Id] = pic
	}
	for i := range list {
		if strings.TrimSpace(list[i].VodPic) != "" {
			continue
		}
		if pic := picByID[list[i].VodID]; pic != "" {
			list[i].VodPic = pic
		}
	}
}

func applyLocalSnapshotToCMSCard(card *model.MovieBasicInfo, snap model.FilmListSnapshot) {
	if card == nil || snap.Mid <= 0 {
		return
	}
	if pic := strings.TrimSpace(snap.DisplayPicture()); pic != "" {
		card.Picture = pic
	}
	if strings.TrimSpace(card.CName) == "" {
		card.CName = snap.CName
	}
	if strings.TrimSpace(card.Remarks) == "" {
		card.Remarks = snap.Remarks
	}
	if strings.TrimSpace(card.Actor) == "" {
		card.Actor = snap.Actor
	}
	if strings.TrimSpace(card.Year) == "" && snap.Year > 0 {
		card.Year = strconv.FormatInt(snap.Year, 10)
	}
}

func resolveCMSMediaURL(raw, sourceURI string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, " ", "%20")
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(sourceURI)), "https://") {
			return "https:" + raw
		}
		return "http:" + raw
	}
	parsed, err := url.Parse(strings.TrimSpace(sourceURI))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return raw
	}
	base := parsed.Scheme + "://" + parsed.Host
	if strings.HasPrefix(raw, "/") {
		return base + raw
	}
	return base + "/" + raw
}

func resolveCMSLocalCards(source *model.FilmSource, sourceMids []int64) (map[int64]int64, map[int64]model.FilmListSnapshot) {
	out := make(map[int64]int64, len(sourceMids))
	snaps := make(map[int64]model.FilmListSnapshot, len(sourceMids))
	if source == nil || len(sourceMids) == 0 {
		return out, snaps
	}
	version := filmsnapshot.GetActiveReadModelVersion()
	lookup := sourceMids
	mapped := map[int64]int64{}
	if source.Grade == model.MasterCollect {
		for _, id := range sourceMids {
			if id > 0 {
				mapped[id] = id
			}
		}
	} else {
		mapped = filmshared.LoadGlobalMidsBySourceMids(source.Id, sourceMids)
		lookup = make([]int64, 0, len(mapped))
		for _, mid := range mapped {
			lookup = append(lookup, mid)
		}
	}
	if len(lookup) == 0 {
		return out, snaps
	}
	for _, snap := range filmsnapshot.GetSnapshotsByMidsOrdered(version, lookup) {
		if snap.Mid > 0 {
			snaps[snap.Mid] = snap
		}
	}
	for sourceMid, mid := range mapped {
		if _, ok := snaps[mid]; ok {
			out[sourceMid] = mid
		}
	}
	return out, snaps
}

func applyCMSPage(page *dto.Page, cms model.FilmListPage, listLen int) {
	if page == nil {
		return
	}
	page.Current = coerceCMSInt(cms.Page, page.Current)
	if page.Current <= 0 {
		page.Current = 1
	}
	page.PageCount = cms.PageCount
	page.Total = cms.Total
	page.PageSize = coerceCMSInt(cms.Limit, page.PageSize)
	if page.PageSize <= 0 {
		if listLen > 0 {
			page.PageSize = listLen
		} else {
			page.PageSize = 12
		}
	}
	if page.PageCount <= 0 && page.Total > 0 && page.PageSize > 0 {
		page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	}
	if page.Total <= 0 {
		page.Total = listLen
	}
	if page.PageCount <= 0 && listLen > 0 {
		page.PageCount = 1
	}
}

func coerceCMSInt(v any, fallback int) int {
	switch n := v.(type) {
	case int:
		if n > 0 {
			return n
		}
	case int64:
		if n > 0 {
			return int(n)
		}
	case float64:
		if n > 0 {
			return int(n)
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(n))
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}
