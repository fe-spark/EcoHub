package support

import (
	"fmt"
	"strconv"
	"strings"

	"server/internal/model"
)

// LookupOtherSourceCategory 从当前映射快照里找该站一级「其他/其它」。
func LookupOtherSourceCategory(sourceId string) (typeID int64, categoryID int64, ok bool) {
	sourceId = strings.TrimSpace(sourceId)
	if sourceId == "" {
		return 0, 0, false
	}
	snap := getMappingSnapshot()
	if snap == nil || snap.SourceCategories == nil {
		return 0, 0, false
	}
	prefix := sourceId + "_"
	var bestType int64
	for key, info := range snap.SourceCategories {
		if !strings.HasPrefix(key, prefix) || info.ParentSourceTypeId != 0 {
			continue
		}
		if !model.IsOtherCategoryName(info.RawName) {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(key, prefix), 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		if bestType == 0 || id < bestType {
			bestType = id
		}
	}
	if bestType == 0 {
		return 0, 0, false
	}
	categoryID = snap.Source[fmt.Sprintf("%s_%d", sourceId, bestType)]
	if categoryID <= 0 {
		return 0, 0, false
	}
	return bestType, categoryID, true
}
