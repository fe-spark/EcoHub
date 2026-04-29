package film

import (
	"server/internal/config"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
)

var filmIndexUpsertUpdateColumns = []string{
	"source_id", "cid", "pid", "root_category_key", "category_key", "original_category", "name", "sub_title", "c_name", "class_tag",
	"series_key", "area", "language", "year", "initial", "score",
	"update_stamp", "hits", "state", "remarks", "play_from_summary", "db_id", "collect_stamp", "category_version", "rule_version",
	"picture", "picture_slide", "actor", "director", "blurb", "updated_at", "deleted_at",
}

var initializedPids sync.Map

var defaultSortTagStrings = []string{"最近更新:update_stamp", "人气:hits", "评分:score", "时间:year"}

const latestUpdateOrderSQL = "update_stamp DESC, mid DESC"

var allowedSearchSortColumns = map[string]string{
	"update_stamp": "update_stamp",
	"hits":         "hits",
	"score":        "score",
	"year":         "year",
}

// ExistFilmIndexTable 检查影片索引表是否存在
func ExistFilmIndexTable() bool {
	return db.Mdb.Migrator().HasTable(&model.FilmIndex{})
}

func ExistFilmIndexByMid(mid int64) bool {
	var count int64
	db.Mdb.Model(&model.FilmIndex{}).Where("mid = ?", mid).Count(&count)
	return count > 0
}

func ExistFilmIndex(mid int64) bool {
	var count int64
	db.Mdb.Model(&model.FilmIndex{}).Where("mid", mid).Count(&count)
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
