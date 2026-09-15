package film

import (
	"fmt"
	"strings"

	"server/internal/model"
	"server/internal/repository/support"
	"server/internal/utils"
)

// ResolveMovieDetailRootPid 解析详情归属的系统根分类 ID (Pid)。
// 遵循“主站权威”原则：优先认主站自带的真实 Pid/Cid；若未携带，仅当 CName 与本地正规分类完全重合时采纳，绝不盲猜。
func ResolveMovieDetailRootPid(detail model.MovieDetail) int64 {
	// 1. 本地已有合法大类 (主站详情自带的真实大类，以主站为准)
	if detail.Pid > 0 {
		if rootId := support.GetRootId(detail.Pid); rootId > 0 && support.IsRootCategory(rootId) {
			return rootId
		}
	}
	if detail.Cid > 0 {
		if rootId := support.GetRootId(detail.Cid); rootId > 0 && support.IsRootCategory(rootId) {
			return rootId
		}
	}
	// 2. 本地系统已有正规分类精确匹配 (不进行任何模糊词猜测)
	if strings.TrimSpace(detail.CName) != "" {
		if pid := support.ResolveRootCategoryIDByCName(detail.CName); pid > 0 {
			return pid
		}
	}
	return 0
}

func BuildPlaylistMovieKeys(detail model.MovieDetail) []string {
	pid := ResolveMovieDetailRootPid(detail)
	return BuildMovieMatchKeysWithCategory(detail.DbId, detail.Name, pid)
}

// BuildPlaylistPrimaryMovieKey 提取详情最精准的首选唯一主键：
// 1. 若有豆瓣 ID，返回 dbid_{id}；
// 2. 若有明确大类 (pid > 0)，返回片名#大类后缀哈希；
// 3. 未知大类时降级返回纯片名哈希。
// 专用于播放列表 (slave_movie_playlist) 与海报 (movie_poster) 的物理实体存储，彻底杜绝数据翻倍与跨类串台。
func BuildPlaylistPrimaryMovieKey(detail model.MovieDetail) string {
	keys := BuildPlaylistMovieKeys(detail)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func BuildMovieMatchKeys(dbID int64, name string) []string {
	return BuildMovieMatchKeysWithCategory(dbID, name, 0)
}

// BuildMovieMatchKeysWithCategory 构造支持大类隔离与向后兼容的双轨匹配键：
// 1. 若有豆瓣 ID，生成 dbid_{id} 精准匹配键；
// 2. 当 pid > 0 时，优先生成带大类后缀的哈希键（如 hash("片名#cat_20")），精准隔离同名不同类剧集；
// 3. 始终保留纯片名哈希键，确保按主站为准与存量影片全向平滑兼容。
func BuildMovieMatchKeysWithCategory(dbID int64, name string, pid int64) []string {
	keys := make([]string, 0, 3)
	if dbIdentity := utils.BuildCollectionDbIdentity(dbID, name); dbIdentity != "" {
		keys = append(keys, utils.GenerateHashKey(dbIdentity))
	}

	normalizedTitle := utils.NormalizeCollectionTitle(name)
	if normalizedTitle != "" {
		if pid > 0 {
			keys = append(keys, utils.GenerateHashKey(fmt.Sprintf("%s#cat_%d", normalizedTitle, pid)))
		}
		keys = append(keys, utils.GenerateHashKey(normalizedTitle))
	}
	return UniqueKeys(keys)
}


// inheritPrimaryMovieKeyIfUnique 副站未识别大类时，仅当片名只命中一部主站影片，才沿用该片已有主键。
// 命中 0 部或同名多部（跨类）时返回空，继续用纯片名键，避免串台。
func inheritPrimaryMovieKeyIfUnique(candidateMids []int64, keysByMid map[int64][]string) string {
	seen := make(map[int64]struct{}, len(candidateMids))
	uniq := make([]int64, 0, 1)
	for _, mid := range candidateMids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		uniq = append(uniq, mid)
	}
	if len(uniq) != 1 {
		return ""
	}
	keys := keysByMid[uniq[0]]
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func UniqueKeys(keys []string) []string {
	orderedKeys := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		orderedKeys = append(orderedKeys, k)
	}
	return orderedKeys
}

// BuildDisplaySourceName 统一站点播放源展示名。
// 单线路仅展示站点名，多线路优先使用“站点名-原始线路名”，缺失时降级为“站点名-线路N”。
func BuildDisplaySourceName(siteName, rawName string, index, total int) string {
	siteName = strings.TrimSpace(siteName)
	rawName = strings.TrimSpace(rawName)

	if siteName == "" {
		if rawName != "" {
			return rawName
		}
		if total > 1 {
			return fmt.Sprintf("播放源%d", index+1)
		}
		return "默认源"
	}

	if total <= 1 {
		return siteName
	}
	if rawName != "" {
		return fmt.Sprintf("%s-%s", siteName, rawName)
	}
	return fmt.Sprintf("%s-线路%d", siteName, index+1)
}

// BuildPlayGroupID 为单个可播放分组构建稳定 ID。
// 单线路时直接复用站点 ID，多线路时追加线路标识，确保同站不同线路可区分。
func BuildPlayGroupID(sourceID, rawName string, index, total int) string {
	sourceID = strings.TrimSpace(sourceID)
	rawName = strings.TrimSpace(rawName)
	if sourceID == "" {
		if rawName != "" {
			return rawName
		}
		return fmt.Sprintf("group_%d", index)
	}
	if total <= 1 {
		return sourceID
	}
	if rawName != "" {
		return fmt.Sprintf("%s::%s", sourceID, rawName)
	}
	return fmt.Sprintf("%s::group_%d", sourceID, index)
}
