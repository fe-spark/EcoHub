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
	"server/internal/repository/support"
	"server/internal/spider"
)

var (
	findCollectSourceById     = repository.FindCollectSourceById
	searchSourceList          = spider.SearchSourceList
	searchSourceListWithProxy = spider.SearchSourceListWithProxy
	fetchSourceDetails        = spider.FetchSourceDetails
	fetchSourceDetailsWithProxy = spider.FetchSourceDetailsWithProxy
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
	tabs = append(tabs, model.SearchSourceTab{Id: "", Name: "综合"})
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
	var proxyURL string
	if ok, p := repository.ResolveSourceProxy(source.Id); ok {
		proxyURL = p
	}
	var pageData model.FilmListPage
	var err error
	if proxyURL != "" {
		pageData, err = searchSourceListWithProxy(source.Uri, keyword, current, proxyURL)
	} else {
		pageData, err = searchSourceList(source.Uri, keyword, current)
	}
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
	localBySourceMid, snaps := resolveCMSLocalCards(source, sourceMids)
	detailsByID := fetchCMSSearchDetails(source.Uri, sourceMids, proxyURL)
	list := make([]model.MovieBasicInfo, 0, len(pageData.List))
	for _, item := range pageData.List {
		if strings.TrimSpace(item.VodName) == "" {
			continue
		}
		card := movieBasicInfoFromCMSList(source, item)
		if detail, ok := detailsByID[item.VodID]; ok {
			applyCMSDetailToCard(&card, detail, source.Uri)
		}
		list = append(list, card)
	}
	assignCMSSearchLocalIDs(list, localBySourceMid, snaps, detailsByID)
	return list, pageData, ""
}

func movieBasicInfoFromCMSList(source *model.FilmSource, item model.FilmList) model.MovieBasicInfo {
	return model.MovieBasicInfo{
		Cid:       item.TypeID,
		Name:      item.VodName,
		CName:     strings.TrimSpace(item.TypeName),
		Picture:   resolveCMSMediaURL(item.VodPic, source.Uri),
		Remarks:   item.VodRemarks,
		SourceId:  source.Id,
		SourceMid: item.VodID,
	}
}

func fetchCMSSearchDetails(uri string, ids []int64, proxyURLs ...string) map[int64]model.MovieDetail {
	out := make(map[int64]model.MovieDetail, len(ids))
	uri = strings.TrimSpace(uri)
	if uri == "" || len(ids) == 0 {
		return out
	}
	requested := make(map[int64]struct{}, len(ids))
	idStrs := make([]string, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := requested[id]; ok {
			continue
		}
		requested[id] = struct{}{}
		idStrs = append(idStrs, strconv.FormatInt(id, 10))
	}
	if len(idStrs) == 0 {
		return out
	}
	proxyURL := ""
	if len(proxyURLs) > 0 {
		proxyURL = strings.TrimSpace(proxyURLs[0])
	}
	var details []model.MovieDetail
	var err error
	if proxyURL != "" {
		details, err = fetchSourceDetailsWithProxy(uri, strings.Join(idStrs, ","), proxyURL)
	} else {
		details, err = fetchSourceDetails(uri, strings.Join(idStrs, ","))
	}
	if err != nil || len(details) == 0 {
		return out
	}
	for _, detail := range details {
		if detail.Id <= 0 {
			continue
		}
		if _, ok := requested[detail.Id]; !ok {
			continue
		}
		out[detail.Id] = detail
	}
	return out
}

func applyCMSDetailToCard(card *model.MovieBasicInfo, detail model.MovieDetail, sourceURI string) {
	if card == nil || detail.Id <= 0 || detail.Id != card.SourceMid {
		return
	}
	if pic := strings.TrimSpace(detail.DisplayPicture()); pic != "" {
		card.Picture = resolveCMSMediaURL(pic, sourceURI)
	}
	if strings.TrimSpace(card.CName) == "" {
		card.CName = strings.TrimSpace(detail.CName)
	}
	if strings.TrimSpace(card.ClassTag) == "" {
		card.ClassTag = strings.TrimSpace(detail.ClassTag)
	}
	if card.Cid <= 0 {
		if detail.RawCid > 0 {
			card.Cid = detail.RawCid
		} else {
			card.Cid = detail.Cid
		}
	}
	if strings.TrimSpace(card.Remarks) == "" {
		card.Remarks = strings.TrimSpace(detail.Remarks)
	}
	if strings.TrimSpace(card.Actor) == "" {
		card.Actor = strings.TrimSpace(detail.Actor)
	}
	if strings.TrimSpace(card.Director) == "" {
		card.Director = strings.TrimSpace(detail.Director)
	}
	if strings.TrimSpace(card.Blurb) == "" {
		card.Blurb = strings.TrimSpace(detail.Blurb)
	}
	if strings.TrimSpace(card.Area) == "" {
		card.Area = strings.TrimSpace(detail.Area)
	}
	if strings.TrimSpace(card.Year) == "" {
		card.Year = strings.TrimSpace(detail.Year)
	}
	if strings.TrimSpace(card.State) == "" {
		card.State = strings.TrimSpace(detail.State)
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

func assignCMSSearchLocalIDs(cards []model.MovieBasicInfo, localBySourceMid map[int64]int64, snaps map[int64]model.FilmListSnapshot, detailsByID map[int64]model.MovieDetail) {
	if len(cards) == 0 {
		return
	}
	hits := make([]int64, len(cards))
	claimed := make(map[int64]int, len(cards))
	for i := range cards {
		mid := localBySourceMid[cards[i].SourceMid]
		snap, ok := snaps[mid]
		if mid <= 0 || !ok {
			continue
		}
		if !cmsSearchCardMatchesLocal(cards[i], detailsByID[cards[i].SourceMid], snap) {
			continue
		}
		hits[i] = mid
		claimed[mid]++
	}
	for i := range cards {
		mid := hits[i]
		if mid <= 0 || claimed[mid] != 1 {
			cards[i].Id = 0
			continue
		}
		cards[i].Id = mid
	}
}

func cmsSearchCardMatchesLocal(card model.MovieBasicInfo, detail model.MovieDetail, snap model.FilmListSnapshot) bool {
	slave := identityFromCMSSearchCard(card, detail)
	master := filmshared.IdentityFromSnapshot(snap)
	if !filmshared.CompatibleIdentity(master, slave) || !filmshared.CompatibleWorkShape(master, slave) {
		return false
	}
	if slave.RootPid > 0 && master.RootPid > 0 && slave.RootPid != master.RootPid {
		return false
	}
	return true
}

func identityFromCMSSearchCard(card model.MovieBasicInfo, detail model.MovieDetail) filmshared.IdentityProfile {
	slave := filmshared.IdentityFromMovieDetail(detail)
	if name := strings.TrimSpace(card.Name); name != "" {
		slave.Name = name
	}
	if cname := strings.TrimSpace(card.CName); cname != "" {
		slave.CName = cname
		if root := support.ResolveRootCategoryIDByCName(cname); root > 0 {
			slave.RootPid = root
		}
	}
	if remarks := strings.TrimSpace(card.Remarks); remarks != "" {
		slave.Remarks = remarks
	}
	if year := filmshared.ParseIdentityYear(card.Year); year > 0 {
		slave.Year = year
	}
	if director := strings.TrimSpace(card.Director); director != "" {
		slave.Director = director
	}
	if classTag := strings.TrimSpace(card.ClassTag); classTag != "" {
		slave.ClassTag = classTag
	}
	return slave
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
