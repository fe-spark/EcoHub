package taxonomy

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
)

func BuildSourceCategoryKey(sourceId string, sourceTypeId int64) string {
	sourceId = strings.TrimSpace(sourceId)
	if sourceId == "" || sourceTypeId <= 0 {
		return ""
	}
	return fmt.Sprintf("source:%s:%d", sourceId, sourceTypeId)
}

// ParseSourceCategoryKey 解析 source:{sourceId}:{typeId}。对不上返回 false。
func ParseSourceCategoryKey(key string) (sourceID string, typeID int64, ok bool) {
	key = strings.TrimSpace(key)
	const prefix = "source:"
	if !strings.HasPrefix(key, prefix) {
		return "", 0, false
	}
	rest := key[len(prefix):]
	idx := strings.LastIndex(rest, ":")
	if idx <= 0 || idx >= len(rest)-1 {
		return "", 0, false
	}
	parsed, err := strconv.ParseInt(rest[idx+1:], 10, 64)
	if err != nil || parsed <= 0 {
		return "", 0, false
	}
	sourceID = strings.TrimSpace(rest[:idx])
	if sourceID == "" {
		return "", 0, false
	}
	return sourceID, parsed, true
}

func GetCategoryStableKeyByID(id int64) string {
	if id <= 0 {
		return ""
	}
	if k, ok := GetCategoryStableKeyFromCache(id); ok {
		return k
	}
	catMu.RLock()
	if k, ok := GetCategoryStableKeyFromCache(id); ok {
		catMu.RUnlock()
		return k
	}
	initialized := catInitialized
	catMu.RUnlock()
	if initialized || db.Mdb == nil {
		return ""
	}
	var category model.Category
	if err := db.Mdb.Select("stable_key").Where("id = ?", id).First(&category).Error; err != nil {
		SetCategoryStableKeyCache(id, "")
		return ""
	}
	SetCategoryStableKeyCache(id, category.StableKey)
	return category.StableKey
}

func ResolveCategoryID(id int64) int64 {
	return id
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
	if set, _ := db.Rdb.SetNX(db.Cxt, config.CategoryVersionKey, version, 0).Result(); !set {
		if cur, err := db.Rdb.Get(db.Cxt, config.CategoryVersionKey).Result(); err == nil && cur != "" {
			return cur
		}
	}
	return version
}

func RefreshCategoryCache() {
	if db.Mdb == nil {
		return
	}
	var all []model.Category
	if err := db.Mdb.Find(&all).Error; err != nil {
		return
	}

	newPidMap := make(map[int64]int64, len(all))
	newShowMap := make(map[int64]bool, len(all))
	for _, c := range all {
		newPidMap[c.Id] = c.Pid
		newShowMap[c.Id] = c.Show
	}

	catMu.Lock()
	catSnapVer.Add(1)
	ResetCategoryNameCache()
	ResetCategoryStableKeyCache()
	for _, c := range all {
		SetCategoryNameCache(c.Id, c.Name)
		SetCategoryStableKeyCache(c.Id, c.StableKey)
	}
	idToPid = newPidMap
	categoryShowMap = newShowMap
	catInitialized = true
	ClearRootCategoryCNameCache()
	catMu.Unlock()
}

func GetRootId(id int64) int64 {
	if id == 0 {
		return 0
	}

	catMu.RLock()
	if !catInitialized {
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
	if !catInitialized {
		catMu.RUnlock()
		RefreshCategoryCache()
		catMu.RLock()
	}
	defer catMu.RUnlock()

	p, ok := idToPid[id]
	return ok && p == 0
}

// isCategoryVisibleLocked 判断分类是否可用，调用方必须持有 catMu 锁
func isCategoryVisibleLocked(cid, pid int64) bool {
	if len(categoryShowMap) == 0 {
		return true
	}

	checkID := func(id int64) bool {
		if id <= 0 {
			return true
		}
		curr := id
		for range [5]int{} {
			if show, exists := categoryShowMap[curr]; exists && !show {
				return false
			}
			p, ok := idToPid[curr]
			if !ok || p == 0 {
				break
			}
			curr = p
		}
		return true
	}

	if cid > 0 && !checkID(cid) {
		return false
	}
	if pid > 0 && !checkID(pid) {
		return false
	}
	return true
}

// IsCategoryVisible 判断分类是否处于全站可用状态（自身及所有父级大类均 Show = true）
func IsCategoryVisible(cid, pid int64) bool {
	catMu.RLock()
	if !catInitialized {
		catMu.RUnlock()
		RefreshCategoryCache()
		catMu.RLock()
	}
	defer catMu.RUnlock()

	return isCategoryVisibleLocked(cid, pid)
}

// GetDisabledCategoryIDs 返回所有当前不可见（自身或父级停用）的分类 ID 集合
func GetDisabledCategoryIDs() []int64 {
	catMu.RLock()
	if !catInitialized {
		catMu.RUnlock()
		RefreshCategoryCache()
		catMu.RLock()
	}
	defer catMu.RUnlock()

	if len(categoryShowMap) == 0 {
		return nil
	}
	var disabled []int64
	for id := range categoryShowMap {
		if !isCategoryVisibleLocked(id, 0) {
			disabled = append(disabled, id)
		}
	}
	return disabled
}

// SetCategoryShowForTest 供单元测试注入分类可见性映射
func SetCategoryShowForTest(showMap map[int64]bool) {
	catMu.Lock()
	categoryShowMap = showMap
	catInitialized = true
	catMu.Unlock()
}

// SetCategoryTreeForTest 供单元测试快速注入内存模拟分类树
func SetCategoryTreeForTest(pidMap map[int64]int64, nameMap map[int64]string) {
	catMu.Lock()
	idToPid = pidMap
	categoryShowMap = make(map[int64]bool)
	catInitialized = true
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

	ver := catSnapVer.Load()
	var matchedId int64
	catMu.RLock()
	categoryNameCache.Range(func(key, val any) bool {
		id, ok1 := key.(int64)
		name, ok2 := val.(string)
		if ok1 && ok2 && strings.EqualFold(name, cName) {
			matchedId = id
			return false
		}
		return true
	})
	catMu.RUnlock()

	var rootId int64
	if matchedId > 0 {
		rootId = GetRootId(matchedId)
	}

	// 仅在分类缓存世代未发生刷新时写入结果，且限制负缓存上限避免异常参数驻留过多
	if catSnapVer.Load() == ver {
		if rootId == 0 {
			const maxRootCNameNegativeCache = 512
			count := 0
			rootCategoryCNameCache.Range(func(_, _ any) bool {
				count++
				return count <= maxRootCNameNegativeCache
			})
			if count > maxRootCNameNegativeCache {
				ClearRootCategoryCNameCache()
			}
		}
		rootCategoryCNameCache.Store(cName, rootId)
	}
	return rootId
}
