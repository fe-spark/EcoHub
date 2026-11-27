package banner

import (
	"context"
	"strings"

	"server/internal/film/catalog"
	"server/internal/model"
	"server/internal/model/dto"
)

// Categories 获取指定采集站可用于一键排片的一级分类（缺省取当前主站）。
func (s *BannerService) Categories(sourceID string) ([]*model.CategoryTree, error) {
	src, _, _, err := resolveBannerSource(sourceID, false)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return []*model.CategoryTree{}, nil
	}
	tree, err := catalog.GetActiveCategoryTree(src.Id)
	if err != nil {
		return nil, err
	}
	if len(tree.Children) == 0 {
		return []*model.CategoryTree{}, nil
	}
	return tree.Children, nil
}

// SearchFilms 按采集站直查可供轮播选取的影片列表（缺省取当前主站）。
func (s *BannerService) SearchFilms(ctx context.Context, sourceID, keyword, sortField string, page *dto.Page) ([]BannerFilmOption, string, error) {
	src, _, _, err := resolveBannerSource(sourceID, false)
	if err != nil {
		return nil, "", err
	}
	if src == nil {
		return []BannerFilmOption{}, "", nil
	}
	list, err := SearchBannerFilmOptions(ctx, src.Id, strings.TrimSpace(keyword), strings.TrimSpace(sortField), page)
	if err != nil {
		return nil, "", err
	}
	return list, src.Id, nil
}
