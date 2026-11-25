package shared

import (
	"strings"

	"server/internal/model"
	"server/internal/taxonomy"
)

// ResolveMovieDetailRootPid 解析详情归属的系统根分类 ID (Pid)。
// 遵循“主站权威”原则：优先认主站自带的真实 Pid/Cid；若未携带，仅当 CName 与本地正规分类完全重合时采纳，绝不盲猜。
func ResolveMovieDetailRootPid(detail model.MovieDetail) int64 {
	// 1. 本地已有合法大类 (主站详情自带的真实大类，以主站为准)
	if detail.Pid > 0 {
		if rootId := taxonomy.GetRootId(detail.Pid); rootId > 0 && taxonomy.IsRootCategory(rootId) {
			return rootId
		}
	}
	if detail.Cid > 0 {
		if rootId := taxonomy.GetRootId(detail.Cid); rootId > 0 && taxonomy.IsRootCategory(rootId) {
			return rootId
		}
	}
	// 2. 本地系统已有正规分类精确匹配 (不进行任何模糊词猜测)
	if strings.TrimSpace(detail.CName) != "" {
		if pid := taxonomy.ResolveRootCategoryIDByCName(detail.CName); pid > 0 {
			return pid
		}
	}
	return 0
}
