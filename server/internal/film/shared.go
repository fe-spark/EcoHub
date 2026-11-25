package film

import (
	"server/internal/config"
	"server/internal/film/cache"
	"server/internal/infra/db"
	"server/internal/repobase"
	"server/internal/taxonomy"
)

func refreshCategoryCaches() {
	if db.Rdb != nil {
		db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
	}
	cache.ClearAllSearchTagsCache()
	taxonomy.RefreshCategoryCache()
}

func markCategoryChanged() {
	refreshCategoryCaches()
	taxonomy.InitMappingEngine()
	taxonomy.TouchCategoryVersion()
	repobase.ClearIndexPageCache()
}
