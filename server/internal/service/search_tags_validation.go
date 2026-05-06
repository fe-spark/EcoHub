package service

import (
	"errors"
	"strings"

	"server/internal/model"
)

func validateReadModelSearchTags(st model.SearchTagsVO) error {
	if st.Cid == model.TagUncategorizedValue {
		return errors.New("当前读模型不支持未细分类筛选")
	}
	if isUnsupportedTagValue(st.Plot) || isUnsupportedTagValue(st.Area) || isUnsupportedTagValue(st.Language) || isUnsupportedTagValue(st.Year) {
		return errors.New("当前读模型不支持未知或其他筛选")
	}
	return nil
}

func isUnsupportedTagValue(value string) bool {
	switch strings.TrimSpace(value) {
	case model.TagUnknownValue:
		return true
	default:
		return false
	}
}
