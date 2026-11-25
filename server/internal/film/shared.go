package film

import (
	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/film/cache"
	"server/internal/repobase"
)

func refreshCategoryCaches() {
	if db.Rdb != nil {
		db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
	}
	cache.ClearAllSearchTagsCache()
	repobase.RefreshCategoryCache()
}

func markCategoryChanged() {
	refreshCategoryCaches()
	repobase.InitMappingEngine()
	repobase.TouchCategoryVersion()
	repobase.ClearIndexPageCache()
}
