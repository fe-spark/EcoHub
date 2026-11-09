package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/utils"
)

// BannerService 按站点管理首页轮播。轮播行只存排序和覆盖项，展示字段读取时从片库组装。
type BannerService struct{}

var BannerSvc = new(BannerService)

// BannerInputError 请求参数不合法，handler 原样返回给前端。
type BannerInputError struct{ msg string }

func (e *BannerInputError) Error() string { return e.msg }

func bannerInputErr(format string, args ...any) error {
	return &BannerInputError{msg: fmt.Sprintf(format, args...)}
}

// 一键排片单个分类最多排几部，和后台输入框上限一致。
const bannerQuotaMax = 5

// 一个站点一次发布的轮播最多几条。
const bannerDeckMax = 99

// 一键排片查询的超时上限。
const bannerGenerateTimeout = 10 * time.Second

// BannerFields 新增、编辑、发布时可提交的字段。指针为空表示不改；空字符串或 0 表示清除覆盖、跟随片库。
//   - score 填 0 等于清除评分覆盖，显示片库评分；year 填 0 同理。
//   - 换片（mid 变化）时会清掉刮削和文字覆盖；本次没提交 poster/backdrop 时图片也回到跟随片库。
type BannerFields struct {
	Mid       *int64   `json:"mid"`
	ImageMode *string  `json:"imageMode"`
	Poster    *string  `json:"poster"`
	Backdrop  *string  `json:"backdrop"`
	Blurb     *string  `json:"blurb"`
	Score     *float64 `json:"score"`
	Year      *int64   `json:"year"`
	TmdbID    *int64   `json:"tmdbId"`
}

// BannerListView 一个站点的轮播列表。
type BannerListView struct {
	SourceId string                  `json:"sourceId"`
	Revision string                  `json:"revision"`
	Items    []model.BannerAdminItem `json:"items"`
}

// BannerQuota 一键排片里一个分类排几部。
type BannerQuota struct {
	CategoryID int64 `json:"categoryId"`
	Count      int   `json:"count"`
}

// BannerGenerateReq 一键排片请求。策略和数量都由前端提交，服务端不保存。
type BannerGenerateReq struct {
	SourceId string        `json:"sourceId"`
	Strategy string        `json:"strategy"`
	Quotas   []BannerQuota `json:"quotas"`
}

// BannerSkipped 一键排片里被跳过或数量不足的分类。
type BannerSkipped struct {
	CategoryID int64  `json:"categoryId"`
	Reason     string `json:"reason"` // not_site_category | not_enough
	Requested  int    `json:"requested"`
	Got        int    `json:"got"`
}

// BannerGenerateResult 一键排片预览。revision 是生成时该站轮播的版本号，发布时带回。
type BannerGenerateResult struct {
	Revision string                  `json:"revision"`
	Items    []model.BannerAdminItem `json:"items"`
	Skipped  []BannerSkipped         `json:"skipped"`
}

var bannerStrategies = map[string]string{
	"latest": "latest_random",
	"hot":    "hot_random",
	"score":  "score_random",
}

// resolveBannerSource 不传站点时用当前首选站。
func resolveBannerSource(sourceID string) (*model.FilmSource, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		primary := repository.PickPrimarySourceForCategory()
		if primary == nil {
			return nil, bannerInputErr("没有已启用的采集站")
		}
		return primary, nil
	}
	src := repository.FindCollectSourceById(sourceID)
	if src == nil {
		return nil, bannerInputErr("采集站不存在")
	}
	return src, nil
}

// AssembleBanners 用片库数据组装轮播行。影片不在片库、或在该行站点已没有播放线路时视为不可用：
// keepUnavailable 为 false（首页）时跳过；为 true（后台）时保留并标记 Unavailable，便于删除和排序。
// 读路径不删数据。片库查询出错时返回 error，不当成“没有影片”。
func AssembleBanners(ctx context.Context, slots []model.BannerSlot, keepUnavailable bool) ([]model.BannerAdminItem, error) {
	out := make([]model.BannerAdminItem, 0, len(slots))
	if len(slots) == 0 {
		return out, nil
	}
	midsBySource := make(map[string][]int64)
	for _, s := range slots {
		midsBySource[s.SourceId] = append(midsBySource[s.SourceId], s.Mid)
	}
	live := make(map[string]map[int64]filmrepo.LiveBannerSnapshot, len(midsBySource))
	for sourceID, mids := range midsBySource {
		snaps, err := filmrepo.LiveBannerSnapshotsByMIDs(ctx, sourceID, mids)
		if err != nil {
			return nil, err
		}
		live[sourceID] = snaps
	}
	for _, s := range slots {
		snap, ok := live[s.SourceId][s.Mid]
		if !ok {
			if !keepUnavailable {
				continue
			}
			item := assembleBanner(s, filmrepo.LiveBannerSnapshot{Mid: s.Mid})
			item.Unavailable = true
			out = append(out, item)
			continue
		}
		out = append(out, assembleBanner(s, snap))
	}
	for i := range out {
		out[i].Sort = int64(i + 1)
	}
	return out, nil
}

func assembleBanner(s model.BannerSlot, snap filmrepo.LiveBannerSnapshot) model.BannerAdminItem {
	libPoster := strings.TrimSpace(snap.DisplayPicture())
	libSlide := strings.TrimSpace(snap.DisplayPictureSlide())
	if libSlide == libPoster {
		libSlide = ""
	}
	library := model.BannerValues{
		Poster: libPoster, Backdrop: libSlide, Blurb: snap.Blurb, Score: snap.Score, Year: snap.Year,
	}
	overrides := model.BannerValues{
		Poster: s.PosterOverride, Backdrop: s.BackdropOverride, Blurb: s.BlurbOverride,
		Score: s.ScoreOverride, Year: s.YearOverride,
	}

	custom := s.ImageMode == model.BannerImageCustom
	poster, slide, customPic := libPoster, libSlide, ""
	if custom {
		if s.PosterOverride != "" {
			poster = s.PosterOverride
			customPic = s.PosterOverride
		}
		if s.BackdropOverride != "" {
			slide = s.BackdropOverride
		}
	}
	if slide == poster {
		slide = ""
	}
	blurb := snap.Blurb
	if s.BlurbOverride != "" {
		blurb = s.BlurbOverride
	}
	score := snap.Score
	if s.ScoreOverride > 0 {
		score = s.ScoreOverride
	}
	year := snap.Year
	if s.YearOverride > 0 {
		year = s.YearOverride
	}
	imageMode := model.BannerImageFollow
	if custom {
		imageMode = model.BannerImageCustom
	}
	return model.BannerAdminItem{
		Banner: model.Banner{
			Id:            s.Id,
			Mid:           s.Mid,
			Name:          snap.Name,
			Year:          year,
			CName:         snap.CName,
			Poster:        poster,
			Picture:       poster,
			PictureSlide:  slide,
			CustomPicture: customPic,
			Remark:        snap.Remarks,
			Sort:          s.Sort,
			IsCustomPic:   custom,
			Area:          snap.Area,
			ClassTag:      snap.ClassTag,
			Actor:         snap.Actor,
			Director:      snap.Director,
			Blurb:         blurb,
			Score:         score,
			TmdbID:        s.TmdbID,
			IsScraped:     s.TmdbID > 0,
		},
		SourceId:  s.SourceId,
		ImageMode: imageMode,
		Overrides: overrides,
		Library:   library,
	}
}

// HomeBanners 首页用的轮播：该站组装后的行，字段和旧版 /home 一致。
func (s *BannerService) HomeBanners(sourceID string) (model.Banners, error) {
	out := make(model.Banners, 0)
	if strings.TrimSpace(sourceID) == "" {
		return out, nil
	}
	slots, err := repository.ListBannerSlots(sourceID)
	if err != nil {
		return nil, err
	}
	items, err := AssembleBanners(context.Background(), slots, false)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		out = append(out, item.Banner)
	}
	return out, nil
}

// List 一个站点的全部轮播和版本号。不可用的行也列出（带 unavailable 标记），
// 这样前端提交排序时的 ids 和服务端算 revision 用的是同一组行。
func (s *BannerService) List(sourceID string) (BannerListView, error) {
	src, err := resolveBannerSource(sourceID)
	if err != nil {
		return BannerListView{}, err
	}
	slots, err := repository.ListBannerSlots(src.Id)
	if err != nil {
		return BannerListView{}, err
	}
	items, err := AssembleBanners(context.Background(), slots, true)
	if err != nil {
		return BannerListView{}, err
	}
	return BannerListView{
		SourceId: src.Id,
		Revision: repository.BannerRevision(slots),
		Items:    items,
	}, nil
}

func (s *BannerService) findItem(slot model.BannerSlot) (model.BannerAdminItem, error) {
	items, err := AssembleBanners(context.Background(), []model.BannerSlot{slot}, true)
	if err != nil {
		return model.BannerAdminItem{}, err
	}
	if len(items) == 0 {
		return model.BannerAdminItem{}, bannerInputErr("影片不存在")
	}
	item := items[0]
	item.Sort = slot.Sort
	return item, nil
}

func checkFilmOnSource(mid int64, sourceID string) error {
	if mid <= 0 {
		return bannerInputErr("请选择影片")
	}
	ok, err := repository.FilmsOnSource(context.Background(), sourceID, []int64{mid})
	if err != nil {
		return err
	}
	if !ok[mid] {
		return bannerInputErr("影片不存在或不属于该采集站")
	}
	return nil
}

// applyBannerFields 把提交的字段合并进轮播行。没提交的字段保持原值。
func applyBannerFields(slot *model.BannerSlot, f BannerFields) error {
	if f.Mid != nil && *f.Mid != slot.Mid {
		slot.Mid = *f.Mid
		// 换了影片，旧影片的刮削和覆盖不再适用，除非这次一起提交了。
		slot.TmdbID = 0
		slot.BlurbOverride = ""
		slot.ScoreOverride = 0
		slot.YearOverride = 0
		// 本次没提交图片时，旧影片的自定义海报和横图也不再适用，回到跟随片库。
		if f.Poster == nil && f.Backdrop == nil {
			slot.ImageMode = model.BannerImageFollow
			slot.PosterOverride = ""
			slot.BackdropOverride = ""
		}
	}
	if f.ImageMode != nil {
		switch strings.TrimSpace(*f.ImageMode) {
		case model.BannerImageFollow:
			slot.ImageMode = model.BannerImageFollow
			slot.PosterOverride = ""
			slot.BackdropOverride = ""
		case model.BannerImageCustom:
			slot.ImageMode = model.BannerImageCustom
		default:
			return bannerInputErr("图片模式只能是 follow 或 custom")
		}
	}
	if slot.ImageMode == "" {
		slot.ImageMode = model.BannerImageFollow
	}
	if slot.ImageMode == model.BannerImageCustom {
		if f.Poster != nil {
			slot.PosterOverride = strings.TrimSpace(*f.Poster)
		}
		if f.Backdrop != nil {
			slot.BackdropOverride = strings.TrimSpace(*f.Backdrop)
		}
	}
	if f.Blurb != nil {
		slot.BlurbOverride = strings.TrimSpace(*f.Blurb)
	}
	if f.Score != nil {
		if *f.Score < 0 || *f.Score > 10 {
			return bannerInputErr("评分只能在 0 到 10 之间")
		}
		slot.ScoreOverride = *f.Score
	}
	if f.Year != nil {
		if *f.Year < 0 {
			return bannerInputErr("年份不合法")
		}
		slot.YearOverride = *f.Year
	}
	if f.TmdbID != nil {
		if *f.TmdbID < 0 {
			return bannerInputErr("TMDB 编号不合法")
		}
		slot.TmdbID = *f.TmdbID
	}
	return nil
}

// Create 新增一行，排在末尾，立即生效。
func (s *BannerService) Create(sourceID string, f BannerFields) (model.BannerAdminItem, error) {
	src, err := resolveBannerSource(sourceID)
	if err != nil {
		return model.BannerAdminItem{}, err
	}
	if f.Mid == nil {
		return model.BannerAdminItem{}, bannerInputErr("请选择影片")
	}
	if err := checkFilmOnSource(*f.Mid, src.Id); err != nil {
		return model.BannerAdminItem{}, err
	}
	id, err := utils.GenerateSalt()
	if err != nil {
		return model.BannerAdminItem{}, err
	}
	slot := model.BannerSlot{Id: id, SourceId: src.Id, Mid: *f.Mid, ImageMode: model.BannerImageFollow}
	if err := applyBannerFields(&slot, f); err != nil {
		return model.BannerAdminItem{}, err
	}
	if err := repository.CreateBannerSlot(&slot); err != nil {
		return model.BannerAdminItem{}, err
	}
	return s.findItem(slot)
}

// Update 只改提交了的字段，立即生效。读出、合并、保存在同一个事务里完成。
func (s *BannerService) Update(id string, f BannerFields) (model.BannerAdminItem, error) {
	slot, err := repository.UpdateBannerSlot(id, func(slot *model.BannerSlot) error {
		oldMid := slot.Mid
		if err := applyBannerFields(slot, f); err != nil {
			return err
		}
		if slot.Mid != oldMid {
			return checkFilmOnSource(slot.Mid, slot.SourceId)
		}
		return nil
	})
	if err != nil {
		return model.BannerAdminItem{}, err
	}
	return s.findItem(slot)
}

// Delete 删除一行，立即生效。
func (s *BannerService) Delete(id string) error {
	return repository.DeleteBannerSlot(id)
}

// Reorder 只提交顺序。revision 不一致返回 repository.ErrBannerRevisionConflict。
func (s *BannerService) Reorder(sourceID, revision string, ids []string) (BannerListView, error) {
	src, err := resolveBannerSource(sourceID)
	if err != nil {
		return BannerListView{}, err
	}
	if err := repository.ReorderBannerSlots(src.Id, revision, ids); err != nil {
		return BannerListView{}, err
	}
	return s.List(src.Id)
}

// PublishDeck 发布一键排片的预览结果，整组替换该站轮播。
func (s *BannerService) PublishDeck(sourceID, revision string, items []BannerFields) (BannerListView, error) {
	src, err := resolveBannerSource(sourceID)
	if err != nil {
		return BannerListView{}, err
	}
	if len(items) > bannerDeckMax {
		return BannerListView{}, bannerInputErr("一次最多发布 %d 条轮播", bannerDeckMax)
	}
	mids := make([]int64, 0, len(items))
	seen := make(map[int64]bool, len(items))
	for _, f := range items {
		if f.Mid == nil || *f.Mid <= 0 {
			return BannerListView{}, bannerInputErr("排片里有未选择影片的行")
		}
		if seen[*f.Mid] {
			return BannerListView{}, bannerInputErr("排片里有重复的影片")
		}
		seen[*f.Mid] = true
		mids = append(mids, *f.Mid)
	}
	onSource, err := repository.FilmsOnSource(context.Background(), src.Id, mids)
	if err != nil {
		return BannerListView{}, err
	}
	slots := make([]model.BannerSlot, 0, len(items))
	for _, f := range items {
		if !onSource[*f.Mid] {
			return BannerListView{}, bannerInputErr("影片不存在或不属于该采集站")
		}
		id, err := utils.GenerateSalt()
		if err != nil {
			return BannerListView{}, err
		}
		slot := model.BannerSlot{Id: id, SourceId: src.Id, Mid: *f.Mid, ImageMode: model.BannerImageFollow}
		if err := applyBannerFields(&slot, f); err != nil {
			return BannerListView{}, err
		}
		slot.Sort = int64(len(slots) + 1)
		slots = append(slots, slot)
	}
	if err := repository.ReplaceBannerSlots(src.Id, revision, slots); err != nil {
		return BannerListView{}, err
	}
	return s.List(src.Id)
}

// Generate 按策略和每个分类的数量生成预览，不落库、不保存配置。
func (s *BannerService) Generate(ctx context.Context, req BannerGenerateReq) (BannerGenerateResult, error) {
	if strings.TrimSpace(req.SourceId) == "" {
		return BannerGenerateResult{}, bannerInputErr("缺少采集站")
	}
	src, err := resolveBannerSource(req.SourceId)
	if err != nil {
		return BannerGenerateResult{}, err
	}
	if !src.State {
		return BannerGenerateResult{}, bannerInputErr("采集站已停用")
	}
	strategy, ok := bannerStrategies[strings.TrimSpace(req.Strategy)]
	if !ok {
		return BannerGenerateResult{}, bannerInputErr("排片策略只能是 latest、hot 或 score")
	}
	quotas := make([]BannerQuota, 0, len(req.Quotas))
	seen := make(map[int64]bool, len(req.Quotas))
	for _, q := range req.Quotas {
		if q.CategoryID <= 0 {
			return BannerGenerateResult{}, bannerInputErr("分类编号不合法")
		}
		if q.Count < 0 || q.Count > bannerQuotaMax {
			return BannerGenerateResult{}, bannerInputErr("每个分类的数量只能在 0 到 %d 之间", bannerQuotaMax)
		}
		if q.Count == 0 || seen[q.CategoryID] {
			continue
		}
		seen[q.CategoryID] = true
		quotas = append(quotas, q)
	}
	if len(quotas) == 0 {
		return BannerGenerateResult{}, bannerInputErr("请至少为一个分类设置排片数量")
	}

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, bannerGenerateTimeout)
	defer cancel()

	current, err := repository.ListBannerSlotsCtx(ctx, src.Id)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return BannerGenerateResult{}, ctxErr
		}
		return BannerGenerateResult{}, err
	}
	version := filmsnapshot.GetActiveSnapshotVersion()

	picks := make([][]model.FilmListSnapshot, len(quotas))
	skipped := make([]BannerSkipped, 0)
	used := make([]int64, 0)
	for i, q := range quotas {
		if err := ctx.Err(); err != nil {
			return BannerGenerateResult{}, err
		}
		cats, fellBack := repository.ResolveBannerSourceCategories(src.Id, []int64{q.CategoryID})
		if err := ctx.Err(); err != nil {
			return BannerGenerateResult{}, err
		}
		if fellBack || len(cats) == 0 {
			skipped = append(skipped, BannerSkipped{CategoryID: q.CategoryID, Reason: "not_site_category", Requested: q.Count})
			continue
		}
		cands, err := filmsnapshot.GetSnapshotBannerCandidates(ctx, version, strategy, src.Id, cats, q.Count, used)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return BannerGenerateResult{}, ctxErr
			}
			return BannerGenerateResult{}, err
		}
		picks[i] = cands
		for _, c := range cands {
			used = append(used, c.Mid)
		}
		if len(cands) < q.Count {
			skipped = append(skipped, BannerSkipped{CategoryID: q.CategoryID, Reason: "not_enough", Requested: q.Count, Got: len(cands)})
		}
	}

	// 各分类轮流穿插。
	slots := make([]model.BannerSlot, 0, len(used))
	for idx := 0; ; idx++ {
		added := false
		for _, list := range picks {
			if idx < len(list) {
				slots = append(slots, model.BannerSlot{
					SourceId: src.Id, Mid: list[idx].Mid, Sort: int64(len(slots) + 1), ImageMode: model.BannerImageFollow,
				})
				added = true
			}
		}
		if !added {
			break
		}
	}
	items, err := AssembleBanners(ctx, slots, false)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return BannerGenerateResult{}, ctxErr
		}
		return BannerGenerateResult{}, err
	}
	return BannerGenerateResult{
		Revision: repository.BannerRevision(current),
		Items:    items,
		Skipped:  skipped,
	}, nil
}

// IsBannerConflict 是否版本号冲突。
func IsBannerConflict(err error) bool {
	return errors.Is(err, repository.ErrBannerRevisionConflict)
}

// IsBannerUserError 是否可以直接提示给用户的错误。
func IsBannerUserError(err error) bool {
	var input *BannerInputError
	return errors.As(err, &input) ||
		errors.Is(err, repository.ErrBannerSlotNotFound) ||
		errors.Is(err, repository.ErrBannerSourceNotFound) ||
		errors.Is(err, repository.ErrBannerDuplicateFilm)
}
