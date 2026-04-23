package film

import (
	"server/internal/config"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
)

var searchInfoUpsertUpdateColumns = []string{
	"source_id", "cid", "pid", "root_category_key", "category_key", "name", "sub_title", "c_name", "class_tag",
	"series_key", "area", "language", "year", "initial", "score",
	"update_stamp", "latest_source_stamp", "hits", "state", "remarks", "play_from_summary", "db_id", "collect_stamp",
	"picture", "picture_slide", "actor", "director", "blurb", "updated_at", "deleted_at",
}

var initializedPids sync.Map

var defaultSortTagStrings = []string{"最新更新:latest_source_stamp", "人气:hits", "评分:score", "最新采集:collect_stamp"}

const latestUpdateOrderSQL = "COALESCE(NULLIF(latest_source_stamp, 0), update_stamp) DESC, update_stamp DESC, mid DESC"

var allowedSearchSortColumns = map[string]string{
	"latest_source_stamp": "latest_source_stamp",
	"update_stamp":        "update_stamp",
	"hits":                "hits",
	"score":               "score",
	"collect_stamp":       "collect_stamp",
}

// ExistSearchTable 检查搜索表是否存在
func ExistSearchTable() bool {
	return db.Mdb.Migrator().HasTable(&model.SearchInfo{})
}

func ExistSearchInMid(mid int64) bool {
	var count int64
	db.Mdb.Model(&model.SearchInfo{}).Where("mid = ?", mid).Count(&count)
	return count > 0
}

func ExistSearchInfo(mid int64) bool {
	var count int64
	db.Mdb.Model(&model.SearchInfo{}).Where("mid", mid).Count(&count)
	return count > 0
}

func refreshCategoryCaches() {
	db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
	ClearAllSearchTagsCache()
	support.RefreshCategoryCache()
}

func markCategoryChanged() {
	refreshCategoryCaches()
	support.InitMappingEngine()
	support.TouchCategoryVersion()
	support.ClearIndexPageCache()
}
