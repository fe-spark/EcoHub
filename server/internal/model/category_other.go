package model

import "strings"

const (
	// OtherCategoryAlias 与 TagOthersName「其他」视为同一兜底分类。
	OtherCategoryAlias = "其它"
	// OtherSourceTypeID 本站没有「其他/其它」时补建用的合成 type_id。
	OtherSourceTypeID int64 = 900000
	// OtherCategorySort 兜底分类默认排在最后。
	OtherCategorySort = 10000
)

func IsOtherCategoryName(name string) bool {
	switch strings.TrimSpace(name) {
	case TagOthersName, OtherCategoryAlias:
		return true
	default:
		return false
	}
}
