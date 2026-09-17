package film

import (
	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/repository/film/cache"
	"server/internal/repository/support"
)

func refreshCategoryCaches() {
	if db.Rdb != nil {
		db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
	}
	cache.ClearAllSearchTagsCache()
	support.RefreshCategoryCache()
}

func markCategoryChanged() {
	refreshCategoryCaches()
	support.InitMappingEngine()
	support.TouchCategoryVersion()
	support.ClearIndexPageCache()
}
