package config

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

/*
 Global Configuration Variables
*/

var (
	// ListenerPort web服务监听的端口
	ListenerPort = ""
	// MysqlDsn mysql服务配置信息
	MysqlDsn = ""

	// Redis连接信息
	RedisAddr     = ""
	RedisPassword = ""
	RedisDBNo     = 0

	// MySQL 原始分项配置 (用于建库与重建等底层操作)
	MysqlHost   = ""
	MysqlPort   = ""
	MysqlUser   = ""
	MysqlPass   = ""
	MysqlDBName = ""

	// JwtSecret JWT 签名密钥
	JwtSecret = ""

	// FilmPictureUploadDir 用户上传素材落地目录。
	// 容器固定走发布卷；相对路径仅本地 go run 使用，生产不得回退。
	FilmPictureUploadDir = resolveFilmPictureUploadDir()
)

const (
	// MAXGoroutine 历史常量：单站分页 worker 默认值（优先用 CollectPageWorkers）。
	MAXGoroutine = 6

	// 采集默认面向 2C2G 单机「速度优先仍可控」档（写阀 + 站/页并发），
	// 同时作为运行时参数为 0 时的兜底默认值。实际档位按 CPU 核数自动选
	// （见 resolveCollectProfile），可用 COLLECT_PROFILE 手动覆盖。
	DefaultCollectWriteMaxInflight              = 3   // 写库事务并发（2 核可承受 3）
	DefaultCollectWritePagesPerSec              = 24  // 稳态页/秒（主要吞吐闸）
	DefaultCollectWriteBurstPages               = 8   // 突发，减少写阀空转
	DefaultCollectWriteMaxPendingPagesPerSource = 48  // 单站写队列
	DefaultCollectWriteMaxPendingPagesGlobal    = 144 // 全局写队列
	DefaultCollectPageWorkers                   = 6   // 单站分页 HTTP 并发
	DefaultCollectPageWorkersSolo               = 10  // 仅 1 站在跑时提高页并发，吃满写阀
	DefaultCollectSourceConcurrency             = 6   // 批量同时跑的站点数；0=不限制
	DefaultCollectStatsFlushIntervalSec         = 5
	DefaultCollectCacheFlushIntervalSec         = 5
	// DefaultCollectProgressRetainSec done/failed 在列表中短暂保留秒数。
	// 仅保留足够前端完成结束态展示的窗口（轮询 + 倒计时），过后查询不到，避免残留到下次进入。
	DefaultCollectProgressRetainSec = 10
	// DefaultCollectProgressStaleSec 无 live task 的 starting/running 超时秒数，超时标 failed 可重采。
	// 不含 waiting_publish / finalizing / page_done（整批收尾等待，不按单站超时）。
	DefaultCollectProgressStaleSec = 30 * 60

	FilmPictureAccess = "/api/upload/pic/poster/"
)

// 采集写阀 / 并发 运行时参数（InitConfig 按 CPU 核数自动选档，见 resolveCollectProfile）。
var (
	// CollectWriteMaxInflight 全局同时进行的写库事务上限。
	CollectWriteMaxInflight = DefaultCollectWriteMaxInflight
	// CollectWritePagesPerSec 写库稳态流速（页/秒）。
	CollectWritePagesPerSec = DefaultCollectWritePagesPerSec
	// CollectWriteBurstPages 写库允许的小突发（页）。
	CollectWriteBurstPages = DefaultCollectWriteBurstPages
	// CollectWriteMaxPendingPagesPerSource 单站写库队列上限。
	CollectWriteMaxPendingPagesPerSource = DefaultCollectWriteMaxPendingPagesPerSource
	// CollectWriteMaxPendingPagesGlobal 全局写库缓冲上限。
	CollectWriteMaxPendingPagesGlobal = DefaultCollectWriteMaxPendingPagesGlobal
	// CollectPageWorkers 单站分页请求并发数。
	CollectPageWorkers = DefaultCollectPageWorkers
	// CollectPageWorkersSolo 仅 1 个 live 任务时的页并发（单站全量提速）。
	CollectPageWorkersSolo = DefaultCollectPageWorkersSolo
	// CollectSourceConcurrency 批量采集同时运行的站点数；0 表示不限制。
	CollectSourceConcurrency = DefaultCollectSourceConcurrency
	// CollectStatsFlushIntervalSec last_collect_time 合并写库最小间隔（秒）。
	CollectStatsFlushIntervalSec = DefaultCollectStatsFlushIntervalSec
	// CollectCacheFlushIntervalSec 采集 Redis 缓存合并清理最小间隔（秒）。
	CollectCacheFlushIntervalSec = DefaultCollectCacheFlushIntervalSec
	// CollectProgressRetainSec done/failed 进度保留秒数。
	CollectProgressRetainSec = DefaultCollectProgressRetainSec
	// CollectProgressStaleSec 活跃进度超时秒数。
	CollectProgressStaleSec = DefaultCollectProgressStaleSec
)

// -------------------------redis key-----------------------------------
const (
	// RedisKeyPrefix 项目 Redis 统一顶层前缀。
	RedisKeyPrefix = "EcoHub"

	// --- 1. 分类与规则 (Category & Rule) ---
	// CategoryTreeKey 分类树 key
	CategoryTreeKey = RedisKeyPrefix + ":Category:Tree"
	// ActiveCategoryTreeKey 活跃分类树缓存 key
	ActiveCategoryTreeKey = RedisKeyPrefix + ":Category:ActiveTree"
	// ActiveCategoryIDsKey 活跃分类 ID 集合缓存 key
	ActiveCategoryIDsKey = RedisKeyPrefix + ":Category:ActiveIDs"
	// CategoryVersionKey 分类版本号缓存 key
	CategoryVersionKey = RedisKeyPrefix + ":Category:Version"
	// RuleVersionKey 分类规则版本号缓存 key
	RuleVersionKey = RedisKeyPrefix + ":Rule:Version"

	// --- 2. 搜索模块 (Search) ---
	// SearchTags 搜索分类标签缓存 key (前缀)
	SearchTags = RedisKeyPrefix + ":Search:Tags"
	// FilmSearchTagsKey 复合搜索标签缓存 key 前缀 (与 SearchTags 保持一致)
	FilmSearchTagsKey = SearchTags
	// SearchTagsVersionKey 搜索分类标签缓存版本号 key
	SearchTagsVersionKey = RedisKeyPrefix + ":Search:Tags:Version"
	// SearchCacheVersionKey 前台搜索缓存版本号 key
	SearchCacheVersionKey = RedisKeyPrefix + ":Search:Version"
	// FilmSearchCachePrefix 前台搜索缓存 key 前缀
	FilmSearchCachePrefix = RedisKeyPrefix + ":Search:Result"

	// --- 3. 影片动态数据与只读快照 (Film & Snapshot) ---
	// SnapshotActiveVersionKey 前台只读影片列表快照当前生效版本（重启备忘）
	SnapshotActiveVersionKey = RedisKeyPrefix + ":Snapshot:ActiveVersion"
	// FilmPlayInfoKey 影片播放详情缓存 key 前缀 (后接 :mid)
	FilmPlayInfoKey = RedisKeyPrefix + ":Film:PlayInfo"
	// FilmHotKeywordsKey 搜索热词缓存 key 前缀 (后接 :v%s)
	FilmHotKeywordsKey = RedisKeyPrefix + ":Film:HotKeywords"
	// FilmCategoryCachePrefix 分类影片列表缓存前缀
	FilmCategoryCachePrefix = RedisKeyPrefix + ":Film:Category"
	// FilmCategoryPageCachePrefix 分类分页列表缓存前缀
	FilmCategoryPageCachePrefix = RedisKeyPrefix + ":Film:CategoryPage"
	// FilmHotCachePrefix 分类热门列表缓存前缀
	FilmHotCachePrefix = RedisKeyPrefix + ":Film:Hot"
	// FilmHotPoolCachePrefix 分类热门候选池缓存前缀
	FilmHotPoolCachePrefix = RedisKeyPrefix + ":Film:HotPool"
	// FilmSortCachePrefix 分类排序列表缓存前缀
	FilmSortCachePrefix = RedisKeyPrefix + ":Film:Sort"
	// FilmClassifyCacheKey 分类首页快照缓存前缀
	FilmClassifyCacheKey = RedisKeyPrefix + ":Film:Classify"
	// FilmFilterOptionKey 筛选选项缓存 key 前缀
	FilmFilterOptionKey = RedisKeyPrefix + ":Film:FilterOption"
	// FilmRelatePrefix 影片相关推荐缓存根前缀 (用于统一清理)
	FilmRelatePrefix = RedisKeyPrefix + ":Film:Relate"
	// FilmRelateCandCachePrefix 相关推荐候选集缓存前缀
	FilmRelateCandCachePrefix = RedisKeyPrefix + ":Film:Relate:Cand"
	// FilmRelateVOCachePrefix 相关推荐视图对象缓存前缀
	FilmRelateVOCachePrefix = RedisKeyPrefix + ":Film:Relate:VO"

	// --- 4. TVBox 与外部提供接口 (TVBox & Provide) ---
	// TVBoxConfigCacheKey TVBox 分类及筛选配置缓存 key
	TVBoxConfigCacheKey = RedisKeyPrefix + ":TVBox:Config"
	// TVBoxNetworkConfigCacheKey TVBox/影视仓一键网络配置缓存 key 前缀
	TVBoxNetworkConfigCacheKey = RedisKeyPrefix + ":TVBox:NetworkConfig"
	// TVBoxList TVBox 列表页缓存前缀
	TVBoxList = RedisKeyPrefix + ":TVBox:List"
	// ProvideListKey Provide 接口列表缓存前缀
	ProvideListKey = RedisKeyPrefix + ":Provide:List"

	// --- 5. 首页聚合 (Index) ---
	// IndexPageCacheKey 首页数据缓存 key
	IndexPageCacheKey = RedisKeyPrefix + ":Index:Page"
	// IndexDailyUpdatesCacheKey 首页「每日更新」候选池短缓存；接口每次从池中随机抽取
	IndexDailyUpdatesCacheKey = RedisKeyPrefix + ":Index:DailyUpdates:v4"

	// --- 6. 运维治理与通知 (Maintenance & Notice) ---
	// OrphanCleanCursorKey 附属站孤儿播放列表治理断点游标重启备忘 key
	OrphanCleanCursorKey = RedisKeyPrefix + ":Film:Orphan:Cursor"
	// MasterSwitchProtectKey 主站切换冷启动保护期重启备忘 key
	MasterSwitchProtectKey = RedisKeyPrefix + ":CleanOrphan:MasterSwitchProtect"
	// NotifyBatchCachePrefix 变更通知批次缓存前缀
	NotifyBatchCachePrefix = RedisKeyPrefix + ":Notify:Batch"
	// VirtualPictureKey 待同步图片临时存储 key
	VirtualPictureKey = RedisKeyPrefix + ":Gallery:VirtualPicture"

	// --- 7. 版本检查缓存 (Version) ---
	// LatestReleaseCacheKey GitHub 最新正式版 Release 缓存
	LatestReleaseCacheKey = RedisKeyPrefix + ":Version:LatestRelease"
	// LatestReleasePreCacheKey 当前为测试版时，含 pre-release 的最新缓存
	LatestReleasePreCacheKey = RedisKeyPrefix + ":Version:LatestRelease:pre"
	// LatestReleaseCacheTTL 版本检查缓存，避免打满 GitHub 匿名限额
	LatestReleaseCacheTTL = time.Hour

	// ConfigCacheTTL 管理员写入控制的配置类 key 有效期 (以长 TTL 最大化命中率)
	ConfigCacheTTL = time.Hour * 24
	// MaxScanCount redis Scan 操作每次扫描的数据量, 每次最多扫描300条数据
	MaxScanCount = 300
)

const (
	AuthUserClaims     = "UserClaims"
	AuthCookieName     = "ecohub_auth_token"
	DefaultAdminUser   = "admin"
	DefaultAdminPass   = "admin"
	DefaultVisitorUser = "guest"
	DefaultVisitorPass = "guest"
)

// -------------------------manage 管理后台相关key----------------------------------
const (
	// SiteConfigBasic 网站参数配置
	SiteConfigBasic = RedisKeyPrefix + ":Config:Site:Basic"
	// NotifyConfigKey Telegram 通知配置缓存
	NotifyConfigKey = RedisKeyPrefix + ":Config:Notify"
	// BannersKey 轮播组件key
	BannersKey = RedisKeyPrefix + ":Config:Banners"

	// DefaultUpdateSpec 每30分钟执行一次
	DefaultUpdateSpec = "0 */30 * * * ?"
	// EveryWeekSpec 每天凌晨4点执行一次
	EveryWeekSpec = "0 0 4 * * *"
	// EveryDaySpec 每天凌晨0点执行一次
	EveryDaySpec = "0 0 0 * * *"
	// OrphanCleanSpec 每天 04:35，错开 DefaultUpdateSpec 的 30 分钟整点（含 04:30）。
	OrphanCleanSpec = "0 35 4 * * *"
	// DefaultUpdateTime 每次采集最近 3 小时内更新的影片
	DefaultUpdateTime = 3
	// DefaultSpiderInterval 默认采集间隔 (ms)，当站点未配置时使用
	DefaultSpiderInterval = 100
)

// -------------------------Database Connection Params-----------------------------------
const (
	UserIdInitialVal = 10000
)

// -------------------------Provide Config-----------------------------------
const (
	PlayForm      = "bkm3u8"
	PlayFormCloud = "ecohub"
	PlayFormAll   = "ecohub$$$bkm3u8"
	RssVersion    = "5.1"
)

const (
	Issuer           = "EcoHub"
	AuthTokenExpires = 10 * 24 // 单位 h
	UserTokenKey     = RedisKeyPrefix + ":User:Token:%d"
)

func isTestEnv() bool {
	if flag.Lookup("test.v") != nil {
		return true
	}
	if len(os.Args) > 0 && (strings.HasSuffix(os.Args[0], ".test") || strings.HasSuffix(os.Args[0], ".test.exe")) {
		return true
	}
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-test.") {
			return true
		}
	}
	return false
}

// IsUpgradeHelper 判断当前进程是否为版本升级助手容器
func IsUpgradeHelper() bool {
	if os.Getenv("ECOHUB_UPGRADE_HELPER") == "1" {
		return true
	}
	for _, a := range os.Args[1:] {
		if a == "upgrade-helper" {
			return true
		}
	}
	return false
}

func init() {
	if IsUpgradeHelper() || isTestEnv() {
		return
	}
	// 本地直接运行服务端时，优先从当前目录 .env 加载环境变量。
	// Docker Compose 会显式注入 environment，这里不会覆盖已存在的值。
	_ = godotenv.Load()

	InitConfig()
}

func InitConfig() {
	// 加载监听端口
	if port := os.Getenv("PORT"); port != "" {
		ListenerPort = port
	}
	if ListenerPort == "" {
		panic("环境变量缺失: PORT")
	}

	fmt.Printf("[Config] 加载端口: %s\n", ListenerPort)

	// 加载 MySQL 配置
	mHost := os.Getenv("MYSQL_HOST")
	mPort := os.Getenv("MYSQL_PORT")
	mUser := os.Getenv("MYSQL_USER")
	mPass := os.Getenv("MYSQL_PASSWORD")
	mDB := os.Getenv("MYSQL_DBNAME")

	if mHost == "" || mPort == "" || mUser == "" || mDB == "" {
		panic(fmt.Sprintf("环境变量缺失: MYSQL_HOST=%s, MYSQL_PORT=%s, MYSQL_USER=%s, MYSQL_DBNAME=%s",
			mHost, mPort, mUser, mDB))
	}

	MysqlHost = mHost
	MysqlPort = mPort
	MysqlUser = mUser
	MysqlPass = mPass
	MysqlDBName = mDB

	MysqlDsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local&timeout=10s&readTimeout=30s&interpolateParams=true",
		mUser, mPass, mHost, mPort, mDB)
	fmt.Printf("[Config] 加载 MySQL DSN: %s:%s@(%s:%s)/%s\n", mUser, "******", mHost, mPort, mDB)

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		panic("环境变量缺失: JWT_SECRET")
	}
	JwtSecret = jwtSecret

	// 加载 Redis 配置
	rHost := os.Getenv("REDIS_HOST")
	rPort := os.Getenv("REDIS_PORT")
	rPass := os.Getenv("REDIS_PASSWORD")
	rDB := os.Getenv("REDIS_DB")

	if rHost == "" || rPort == "" {
		panic(fmt.Sprintf("环境变量缺失: REDIS_HOST=%s, REDIS_PORT=%s", rHost, rPort))
	}

	RedisAddr = fmt.Sprintf("%s:%s", rHost, rPort)
	if rPass != "" {
		RedisPassword = rPass
	}
	if rDB != "" {
		if dbNo, err := strconv.Atoi(rDB); err == nil {
			RedisDBNo = dbNo
		}
	}
	fmt.Printf("[Config] 加载 Redis 地址: %s, DB: %d\n", RedisAddr, RedisDBNo)

	FilmPictureUploadDir = resolveFilmPictureUploadDir()
	fmt.Printf("[Config] 素材目录: %s\n", FilmPictureUploadDir)

	loadCollectRuntimeConfig()
	loadAccessRuntimeConfig()

}

// collectProfile 采集并发/写阀档位：light=2C2G 保守档，standard=4C 中档，high=8C+ 高档。
type collectProfile struct {
	writeMaxInflight      int
	writePagesPerSec      int
	writeBurstPages       int
	writePendingPerSource int
	writePendingGlobal    int
	pageWorkers           int
	pageWorkersSolo       int
	sourceConcurrency     int
}

// collectProfiles 档位数值表（light 与 Default* 兜底一致）。
var collectProfiles = map[string]collectProfile{
	"light": {
		writeMaxInflight:      3,
		writePagesPerSec:      24,
		writeBurstPages:       8,
		writePendingPerSource: 48,
		writePendingGlobal:    144,
		pageWorkers:           6,
		pageWorkersSolo:       10,
		sourceConcurrency:     6,
	},
	"standard": {
		writeMaxInflight:      6,
		writePagesPerSec:      48,
		writeBurstPages:       12,
		writePendingPerSource: 96,
		writePendingGlobal:    288,
		pageWorkers:           10,
		pageWorkersSolo:       16,
		sourceConcurrency:     8,
	},
	"high": {
		writeMaxInflight:      10,
		writePagesPerSec:      96,
		writeBurstPages:       20,
		writePendingPerSource: 160,
		writePendingGlobal:    480,
		pageWorkers:           16,
		pageWorkersSolo:       24,
		sourceConcurrency:     12,
	},
}

// resolveCollectProfile 解析采集档位：COLLECT_PROFILE 显式指定（auto|light|standard|high）；
// auto 或未设置时按 CPU 核数自动选档（≤2=light，3-7=standard，≥8=high）。
func resolveCollectProfile() (collectProfile, string) {
	name := strings.ToLower(strings.TrimSpace(os.Getenv("COLLECT_PROFILE")))
	if name == "" || name == "auto" {
		name = ""
	} else if p, ok := collectProfiles[name]; ok {
		return p, name
	} else {
		fmt.Printf("[Config] COLLECT_PROFILE=%q 无效，回退自动选档\n", name)
		name = ""
	}
	switch n := runtime.NumCPU(); {
	case n <= 2:
		return collectProfiles["light"], "light"
	case n >= 8:
		return collectProfiles["high"], "high"
	default:
		return collectProfiles["standard"], "standard"
	}
}

// loadCollectRuntimeConfig 按服务器资源自动选择采集写阀/并发档位（无环境变量配置）。
func loadCollectRuntimeConfig() {
	p, name := resolveCollectProfile()
	CollectWriteMaxInflight = p.writeMaxInflight
	CollectWritePagesPerSec = p.writePagesPerSec
	CollectWriteBurstPages = p.writeBurstPages
	CollectWriteMaxPendingPagesPerSource = p.writePendingPerSource
	CollectWriteMaxPendingPagesGlobal = p.writePendingGlobal
	CollectPageWorkers = p.pageWorkers
	CollectPageWorkersSolo = p.pageWorkersSolo
	CollectSourceConcurrency = p.sourceConcurrency

	srcLimit := "不限制"
	if CollectSourceConcurrency > 0 {
		srcLimit = fmt.Sprintf("%d", CollectSourceConcurrency)
	}
	fmt.Printf("[Config] 采集档位=%s（CPU=%d核） 写阀 inflight=%d pages/s=%d burst=%d pending=%d/%d | 页并发=%d(单站=%d) 站并发=%s | retain=%ds stale=%ds\n",
		name, runtime.NumCPU(),
		CollectWriteMaxInflight, CollectWritePagesPerSec, CollectWriteBurstPages,
		CollectWriteMaxPendingPagesPerSource, CollectWriteMaxPendingPagesGlobal,
		CollectPageWorkers, CollectPageWorkersSolo, srcLimit,
		CollectProgressRetainSec, CollectProgressStaleSec)
}

// GetRootMysqlDsn 获取不带数据库名的 DSN，用于 CREATE DATABASE 等管理操作
func GetRootMysqlDsn() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/?charset=utf8mb4&parseTime=True&loc=Local",
		MysqlUser, MysqlPass, MysqlHost, MysqlPort)
}
