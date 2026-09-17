package support

import (
	"fmt"
	"strings"
	"sync"
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

func BuildSourceCategoryKey(sourceId string, sourceTypeId int64) string {
	sourceId = strings.TrimSpace(sourceId)
	if sourceId == "" || sourceTypeId <= 0 {
		return ""
	}
	return fmt.Sprintf("source:%s:%d", sourceId, sourceTypeId)
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
	if db.Rdb == nil {
		return
	}
	db.Rdb.Set(db.Cxt, config.CategoryVersionKey, time.Now().UnixNano(), 0)
}

func TouchSearchTagsVersion() {
	if db.Rdb == nil {
		return
	}
	db.Rdb.Set(db.Cxt, config.SearchTagsVersionKey, time.Now().UnixNano(), 0)
}

func GetCategoryVersion() string {
	if db.Rdb == nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
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
	if db.Rdb == nil {
		return
	}
	iter := db.Rdb.Scan(db.Cxt, 0, config.IndexPageCacheKey+"*", config.MaxScanCount).Iterator()
	for iter.Next(db.Cxt) {
		db.Rdb.Del(db.Cxt, iter.Val())
	}
	db.Rdb.Del(db.Cxt, config.IndexDailyUpdatesCacheKey)
}

func RefreshCategoryCache() {
	if db.Mdb == nil {
		return
	}
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

	ClearRootCategoryCNameCache()
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

// SetCategoryTreeForTest 供单元测试快速注入内存模拟分类树
func SetCategoryTreeForTest(pidMap map[int64]int64, nameMap map[int64]string) {
	catMu.Lock()
	idToPid = pidMap
	catMu.Unlock()
	ResetCategoryNameCache()
	for id, name := range nameMap {
		SetCategoryNameCache(id, name)
	}
	ClearRootCategoryCNameCache()
}

var (
	rootCategoryCNameCache sync.Map // cName (string) -> rootPid (int64)
)

// ClearRootCategoryCNameCache 清空分类名称推断缓存。
func ClearRootCategoryCNameCache() {
	rootCategoryCNameCache.Clear()
}

// ResolveRootCategoryIDByCName 根据分类名称在系统已有分类中匹配归属的一级大类 ID (Pid)。
// 仅按主站/本地系统已有分类做精准匹配，不进行任何猜测；未匹配则返回 0。
func ResolveRootCategoryIDByCName(cName string) int64 {
	cName = strings.TrimSpace(cName)
	if cName == "" {
		return 0
	}

	// 1. 快速读取内存缓存（0ns / 0 SQL 开销）
	if cached, ok := rootCategoryCNameCache.Load(cName); ok {
		return cached.(int64)
	}

	// 2. 确保本地分类表内存缓存就绪，优先匹配全库已有已知分类（精确匹配）
	catMu.RLock()
	isEmpty := len(idToPid) == 0
	catMu.RUnlock()
	if isEmpty {
		RefreshCategoryCache()
	}

	var matchedId int64
	// sync.Map 自带并发安全，遍历无需持有 catMu 锁，彻底杜绝与写锁并发时的重入死锁
	categoryNameCache.Range(func(key, val any) bool {
		id, ok1 := key.(int64)
		name, ok2 := val.(string)
		if ok1 && ok2 && strings.EqualFold(name, cName) {
			matchedId = id
			return false
		}
		return true
	})

	var rootId int64
	if matchedId > 0 {
		rootId = GetRootId(matchedId)
	}

	// 内存遍历未命中即代表库内无此正规分类，不进行盲猜，直接记录 0
	rootCategoryCNameCache.Store(cName, rootId)
	return rootId
}
