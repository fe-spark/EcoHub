package enrich

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/analytics/types"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/taxonomy"
)

const (
	TopKeepDefault        = 10
	TopPersistLimit       = 50
	ZSetKeep              = 5000
	metaQueryBatchSize    = 200
	filmMetaCacheMaxSize  = 10000
	filmMetaCacheTTL      = 1 * time.Minute
	catCacheTTL           = 5 * time.Minute
	maxCatByNameCacheSize = 2048
)

type FilmMeta struct {
	Title    string
	Category string
	Poster   string
	Year     int64
	CachedAt time.Time
}

var (
	filmMetaMu    sync.RWMutex
	filmMetaCache = map[int64]FilmMeta{}

	catCacheMu       sync.RWMutex
	catNameCache     = map[int64]string{}
	catIdByNameCache = map[string]int64{}
	catCacheAt       time.Time
)

// ParseFilmID 解析合法正整数影片/分类 ID（兼容 "id 123" 前缀）
func ParseFilmID(resource string) (int64, bool) {
	resource = strings.TrimSpace(resource)
	resource = strings.TrimPrefix(resource, "id ")
	id, err := strconv.ParseInt(resource, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// CanonicalFilmID 归一化为十进制正整数字符串
func CanonicalFilmID(resource string) string {
	id, ok := ParseFilmID(resource)
	if !ok {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

// ExtractSingleResourceToken 提取逗号分隔列表中的首项资源标识
func ExtractSingleResourceToken(resource string) string {
	res := strings.TrimSpace(resource)
	if idx := strings.Index(res, ","); idx > 0 {
		res = strings.TrimSpace(res[:idx])
	}
	return res
}

// SanitizePosterURL 仅放行 http/https 协议且无换行的海报地址
func SanitizePosterURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, "\r\n") {
		return ""
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") {
		return s
	}
	return ""
}

// IsPlayEvent 判断事件是否为影视点播行为
func IsPlayEvent(evt *types.Event) bool {
	if evt == nil {
		return false
	}
	if evt.Action == types.ActionPlay || strings.HasPrefix(evt.Path, "/api/films/play-info") {
		return true
	}
	if strings.HasPrefix(evt.Path, "/api/provide/vod") {
		return strings.Contains(evt.Query, "ids=")
	}
	return false
}

// PlayTopFetchCount 为过滤脏数据多拉取 3 倍候选条数
func PlayTopFetchCount(limit int) int {
	if limit <= 0 {
		limit = TopKeepDefault
	}
	fetch := limit * 3
	if fetch > ZSetKeep {
		fetch = ZSetKeep
	}
	return fetch
}

// GetFilmMetaFromMemory 仅从进程内缓存读取影片元数据（绝不查库）
func GetFilmMetaFromMemory(id int64) (FilmMeta, bool) {
	filmMetaMu.RLock()
	defer filmMetaMu.RUnlock()
	item, ok := filmMetaCache[id]
	if !ok || time.Since(item.CachedAt) >= filmMetaCacheTTL {
		return FilmMeta{}, false
	}
	return item, true
}

// GetCategoryNameFromMemory 仅从进程内缓存读取分类名称（绝不查库）
func GetCategoryNameFromMemory(id int64) (string, bool) {
	catCacheMu.RLock()
	defer catCacheMu.RUnlock()
	if time.Since(catCacheAt) >= catCacheTTL {
		return "", false
	}
	name, ok := catNameCache[id]
	return name, ok
}

// ResolveCategoryIDByNameMemoryOnly 仅查进程内分类缓存（含 taxonomy 快照），绝不触发 SQL
func ResolveCategoryIDByNameMemoryOnly(name string) (int64, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, false
	}
	catCacheMu.RLock()
	if time.Since(catCacheAt) < catCacheTTL {
		if id, ok := catIdByNameCache[name]; ok {
			catCacheMu.RUnlock()
			return id, id > 0
		}
	}
	catCacheMu.RUnlock()
	return taxonomy.LookupCategoryIDByNameMemoryOnly(name)
}

func queryInBatches[K any, R any](keys []K, batchSize int, fn func(batch []K) ([]R, error)) ([]R, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	if batchSize <= 0 {
		batchSize = metaQueryBatchSize
	}
	out := make([]R, 0, len(keys))
	for start := 0; start < len(keys); start += batchSize {
		end := start + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		rows, err := fn(keys[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

// ResolveFilmMetas 批量按 mid 查询影片元数据并更新短时内存缓存（分批 IN 防止退化与变量上限）
func ResolveFilmMetas(filmIDs []int64) (map[int64]FilmMeta, error) {
	if len(filmIDs) == 0 {
		return map[int64]FilmMeta{}, nil
	}
	result := make(map[int64]FilmMeta, len(filmIDs))
	missing := make([]int64, 0, len(filmIDs))
	now := time.Now()

	filmMetaMu.RLock()
	for _, id := range filmIDs {
		if item, ok := filmMetaCache[id]; ok && now.Sub(item.CachedAt) < filmMetaCacheTTL {
			result[id] = item
		} else {
			missing = append(missing, id)
		}
	}
	filmMetaMu.RUnlock()

	if len(missing) == 0 {
		return result, nil
	}
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}

	rows, err := queryInBatches(missing, metaQueryBatchSize, func(batch []int64) ([]model.FilmIndex, error) {
		var part []model.FilmIndex
		err := db.Mdb.Model(&model.FilmIndex{}).
			Select("mid, name, c_name, picture, year").
			Where("mid IN ?", batch).
			Find(&part).Error
		return part, err
	})
	if err != nil {
		return nil, err
	}
	foundMap := make(map[int64]FilmMeta, len(rows))
	for _, row := range rows {
		foundMap[row.Mid] = FilmMeta{
			Title:    row.Name,
			Category: row.CName,
			Poster:   row.Picture,
			Year:     row.Year,
			CachedAt: now,
		}
	}

	filmMetaMu.Lock()
	defer filmMetaMu.Unlock()
	for _, id := range missing {
		if item, ok := foundMap[id]; ok {
			filmMetaCache[id] = item
			result[id] = item
		}
	}
	if len(filmMetaCache) > filmMetaCacheMaxSize {
		cutoff := now.Add(-filmMetaCacheTTL)
		for k, v := range filmMetaCache {
			if v.CachedAt.Before(cutoff) {
				delete(filmMetaCache, k)
			}
		}
		// 仅淘汰超出部分，避免整表清空引起瞬时穿透
		if len(filmMetaCache) > filmMetaCacheMaxSize {
			excess := len(filmMetaCache) - (filmMetaCacheMaxSize * 4 / 5)
			for k := range filmMetaCache {
				if excess <= 0 {
					break
				}
				delete(filmMetaCache, k)
				excess--
			}
		}
	}
	return result, nil
}

// ResolveCategoryNames 批量按分类 ID 解析名称
func ResolveCategoryNames(ids []int64) (map[int64]string, error) {
	if len(ids) == 0 {
		return map[int64]string{}, nil
	}
	result := make(map[int64]string, len(ids))
	missing := make([]int64, 0, len(ids))

	catCacheMu.RLock()
	if time.Since(catCacheAt) < catCacheTTL {
		for _, id := range ids {
			if name, ok := catNameCache[id]; ok {
				result[id] = name
			} else {
				missing = append(missing, id)
			}
		}
	} else {
		missing = ids
	}
	catCacheMu.RUnlock()

	if len(missing) == 0 || db.Mdb == nil {
		return result, nil
	}
	cats, err := queryInBatches(missing, metaQueryBatchSize, func(batch []int64) ([]model.Category, error) {
		var part []model.Category
		err := db.Mdb.Model(&model.Category{}).
			Select("id, name").
			Where("id IN ?", batch).
			Find(&part).Error
		return part, err
	})
	if err != nil {
		return nil, err
	}
	catCacheMu.Lock()
	if time.Since(catCacheAt) >= catCacheTTL {
		catNameCache = map[int64]string{}
		catIdByNameCache = map[string]int64{}
		catCacheAt = time.Now()
	}
	for _, c := range cats {
		catNameCache[c.Id] = c.Name
		if len(catIdByNameCache) < maxCatByNameCacheSize {
			catIdByNameCache[c.Name] = c.Id
		}
		result[c.Id] = c.Name
	}
	for _, id := range missing {
		if _, ok := catNameCache[id]; !ok {
			placeholder := fmt.Sprintf("分类 #%d", id)
			catNameCache[id] = placeholder
			result[id] = placeholder
		}
	}
	catCacheMu.Unlock()
	return result, nil
}

func resolveCategoryIDsByNames(names []string) (map[string]int64, error) {
	out := make(map[string]int64, len(names))
	missing := make([]string, 0, len(names))
	for _, name := range names {
		if id, ok := ResolveCategoryIDByNameMemoryOnly(name); ok {
			out[name] = id
		} else if name != "" {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 || db.Mdb == nil {
		return out, nil
	}
	cats, err := queryInBatches(missing, metaQueryBatchSize, func(batch []string) ([]model.Category, error) {
		var part []model.Category
		err := db.Mdb.Model(&model.Category{}).
			Select("id, name").
			Where("name IN ?", batch).
			Find(&part).Error
		return part, err
	})
	if err != nil {
		return nil, err
	}
	catCacheMu.Lock()
	if catCacheAt.IsZero() || time.Since(catCacheAt) >= catCacheTTL {
		catNameCache = map[int64]string{}
		catIdByNameCache = map[string]int64{}
		catCacheAt = time.Now()
	}
	for _, c := range cats {
		trimmed := strings.TrimSpace(c.Name)
		if c.Id > 0 && trimmed != "" {
			out[trimmed] = c.Id
			catNameCache[c.Id] = trimmed
			if len(catIdByNameCache) < maxCatByNameCacheSize {
				catIdByNameCache[trimmed] = c.Id
			}
		}
	}
	catCacheMu.Unlock()
	return out, nil
}

// ResetCachesForTest 重置进程内元数据缓存（仅限测试使用）
func ResetCachesForTest() {
	filmMetaMu.Lock()
	filmMetaCache = map[int64]FilmMeta{}
	filmMetaMu.Unlock()

	catCacheMu.Lock()
	catNameCache = map[int64]string{}
	catIdByNameCache = map[string]int64{}
	catCacheAt = time.Time{}
	catCacheMu.Unlock()
}
