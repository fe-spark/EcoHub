package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider/progress"
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

// BannerSourceOption 轮播管理页采集站下拉选项。
type BannerSourceOption struct {
	Id    string `json:"id"`
	Name  string `json:"name"`
	State bool   `json:"state"`
}

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

// BannerListView 一个站点的轮播列表及采集站上下文。
type BannerListView struct {
	SourceId        string                  `json:"sourceId"`
	PrimarySourceId string                  `json:"primarySourceId"`
	Sources         []BannerSourceOption    `json:"sources"`
	Revision        string                  `json:"revision"`
	Items           []model.BannerAdminItem `json:"items"`
}

func toBannerSourceOptions(sources []model.FilmSource) []BannerSourceOption {
	out := make([]BannerSourceOption, 0, len(sources))
	for _, src := range sources {
		out = append(out, BannerSourceOption{
			Id:    src.Id,
			Name:  src.Name,
			State: src.State,
		})
	}
	return out
}

// resolveBannerSource 解析目标采集站。sourceID 为空时默认当前主站；requireTarget 为 true 时要求必须解析出站点。
func resolveBannerSource(sourceID string, requireTarget bool) (*model.FilmSource, []BannerSourceOption, string, error) {
	sources, err := repository.GetCollectSourceList()
	if err != nil {
		return nil, nil, "", err
	}
	options := toBannerSourceOptions(sources)
	primary := repository.CurrentPrimarySource()
	primaryID := ""
	if primary != nil {
		primaryID = primary.Id
	}

	targetID := strings.TrimSpace(sourceID)
	if targetID == "" {
		if primary == nil {
			if requireTarget {
				return nil, options, primaryID, bannerInputErr("没有已启用的采集站")
			}
			return nil, options, primaryID, nil
		}
		if requireTarget && progress.IsSourceDeleting(primary.Id) {
			return nil, options, primaryID, bannerInputErr("该采集站正在删除中")
		}
		return primary, options, primaryID, nil
	}

	for i := range sources {
		if strings.TrimSpace(sources[i].Id) == targetID {
			if requireTarget && progress.IsSourceDeleting(sources[i].Id) {
				return nil, options, primaryID, bannerInputErr("该采集站正在删除中")
			}
			chosen := sources[i]
			return &chosen, options, primaryID, nil
		}
	}
	return nil, options, primaryID, repository.ErrBannerSourceNotFound
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

// List 获取指定采集站的全部轮播和版本号（sourceID 为空时默认当前主站）。不可用的行也列出（带 unavailable 标记），
// 这样前端提交排序时的 ids 和服务端算 revision 用的是同一组行。
func (s *BannerService) List(sourceID string) (BannerListView, error) {
	src, options, primaryID, err := resolveBannerSource(sourceID, false)
	if err != nil {
		return BannerListView{}, err
	}
	if src == nil {
		return BannerListView{
			PrimarySourceId: primaryID,
			Sources:         options,
			Items:           []model.BannerAdminItem{},
		}, nil
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
		SourceId:        src.Id,
		PrimarySourceId: primaryID,
		Sources:         options,
		Revision:        repository.BannerRevision(slots),
		Items:           items,
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

// Create 新增一行，排在目标采集站末尾，立即生效。
func (s *BannerService) Create(sourceID string, f BannerFields) (model.BannerAdminItem, error) {
	src, _, _, err := resolveBannerSource(sourceID, true)
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

// Update 只改提交了的字段，立即生效。传 expectedSourceID 时校对行归属防串站。
func (s *BannerService) Update(id, expectedSourceID string, f BannerFields) (model.BannerAdminItem, error) {
	expectedSourceID = strings.TrimSpace(expectedSourceID)
	slot, err := repository.UpdateBannerSlot(id, func(slot *model.BannerSlot) error {
		if expectedSourceID != "" && slot.SourceId != expectedSourceID {
			return bannerInputErr("轮播不属于当前采集站")
		}
		if progress.IsSourceDeleting(slot.SourceId) {
			return bannerInputErr("该采集站正在删除中")
		}
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

// Delete 删除一行，立即生效。传 expectedSourceID 时校对行归属防串站。
func (s *BannerService) Delete(id, expectedSourceID string) error {
	expectedSourceID = strings.TrimSpace(expectedSourceID)
	slot, err := repository.GetBannerSlot(id)
	if err != nil {
		return err
	}
	if expectedSourceID != "" && slot.SourceId != expectedSourceID {
		return bannerInputErr("轮播不属于当前采集站")
	}
	if progress.IsSourceDeleting(slot.SourceId) {
		return bannerInputErr("该采集站正在删除中")
	}
	return repository.DeleteBannerSlot(id)
}

// Reorder 只提交指定采集站顺序。revision 不一致返回 repository.ErrBannerRevisionConflict。
func (s *BannerService) Reorder(sourceID, revision string, ids []string) (BannerListView, error) {
	src, _, _, err := resolveBannerSource(sourceID, true)
	if err != nil {
		return BannerListView{}, err
	}
	if err := repository.ReorderBannerSlots(src.Id, revision, ids); err != nil {
		return BannerListView{}, err
	}
	return s.List(src.Id)
}

// PublishDeck 发布一键排片的预览结果，整组替换指定采集站轮播。
func (s *BannerService) PublishDeck(sourceID, revision string, items []BannerFields) (BannerListView, error) {
	src, _, _, err := resolveBannerSource(sourceID, true)
	if err != nil {
		return BannerListView{}, err
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
