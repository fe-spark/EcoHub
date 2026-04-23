package support

import (
	"fmt"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
)

func BuildCategoryStableKey(pid int64, name string) string {
	name = strings.TrimSpace(name)
	if pid == 0 {
		return fmt.Sprintf("root:%s", name)
	}
	parentKey := GetCategoryStableKeyByID(pid)
	if parentKey == "" {
		return fmt.Sprintf("sub:%d:%s", pid, name)
	}
	return fmt.Sprintf("%s/%s", parentKey, name)
}

func GetCategoryStableKeyByID(id int64) string {
	if id <= 0 {
		return ""
	}
	var category model.Category
	if err := db.Mdb.Select("stable_key").Where("id = ?", id).First(&category).Error; err != nil {
		return ""
	}
	return category.StableKey
}

func ResolveCategoryID(id int64) int64 {
	if id <= 0 {
		return id
	}
	var category model.Category
	if err := db.Mdb.Where("id = ?", id).First(&category).Error; err != nil {
		return id
	}
	if category.StableKey != "" {
		var current model.Category
		if err := db.Mdb.Where("stable_key = ?", category.StableKey).First(&current).Error; err == nil {
			return current.Id
		}
	}
	return category.Id
}

func TouchCategoryVersion() {
	db.Rdb.Set(db.Cxt, config.CategoryVersionKey, time.Now().UnixNano(), 0)
}

func GetCategoryVersion() string {
	version, err := db.Rdb.Get(db.Cxt, config.CategoryVersionKey).Result()
	if err == nil && version != "" {
		return version
	}
	version = fmt.Sprintf("%d", time.Now().UnixNano())
	db.Rdb.Set(db.Cxt, config.CategoryVersionKey, version, 0)
	return version
}

func GetVersionedIndexPageCacheKey() string {
	return fmt.Sprintf("%s:v%s", config.IndexPageCacheKey, GetCategoryVersion())
}

func ClearIndexPageCache() {
	iter := db.Rdb.Scan(db.Cxt, 0, config.IndexPageCacheKey+"*", config.MaxScanCount).Iterator()
	for iter.Next(db.Cxt) {
		db.Rdb.Del(db.Cxt, iter.Val())
	}
}

func RefreshCategoryCache() {
	var all []model.Category
	db.Mdb.Find(&all)

	newPidMap := make(map[int64]int64)
	ResetCategoryNameCache()
	for _, c := range all {
		item := c
		newPidMap[item.Id] = item.Pid
		SetCategoryNameCache(item.Id, item.Name)
	}

	catMu.Lock()
	idToPid = newPidMap
	catMu.Unlock()
}

func GetRootId(id int64) int64 {
	if id == 0 {
		return 0
	}

	catMu.RLock()
	if len(idToPid) == 0 {
		catMu.RUnlock()
		RefreshCategoryCache()
		catMu.RLock()
	}
	defer catMu.RUnlock()

	curr := id
	for range [5]int{} {
		p, ok := idToPid[curr]
		if !ok || p == 0 {
			return curr
		}
		curr = p
	}
	return curr
}

func IsRootCategory(id int64) bool {
	if id == 0 {
		return false
	}

	catMu.RLock()
	if len(idToPid) == 0 {
		catMu.RUnlock()
		RefreshCategoryCache()
		catMu.RLock()
	}
	defer catMu.RUnlock()

	p, ok := idToPid[id]
	return ok && p == 0
}

func GetParentId(id int64) int64 {
	if id == 0 {
		return 0
	}

	catMu.RLock()
	if len(idToPid) == 0 {
		catMu.RUnlock()
		RefreshCategoryCache()
		catMu.RLock()
	}
	defer catMu.RUnlock()

	return idToPid[id]
}
