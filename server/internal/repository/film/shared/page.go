package shared

import "server/internal/model/dto"

// EnsurePage 归一化分页参数：nil 或非法值回落到第 1 页、每页 20 条。
func EnsurePage(page *dto.Page) *dto.Page {
	if page == nil {
		return &dto.Page{Current: 1, PageSize: 20}
	}
	if page.Current <= 0 {
		page.Current = 1
	}
	if page.PageSize <= 0 {
		page.PageSize = 20
	}
	return page
}

// PageOffset 计算已归一化分页参数的 SQL 偏移量。
func PageOffset(page *dto.Page) int {
	page = EnsurePage(page)
	if page.Current <= 1 {
		return 0
	}
	return (page.Current - 1) * page.PageSize
}
