package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	FilmPictureUploadDir = filmPictureUploadDirLocal

	// ClusterRole 当前节点在集群中的角色：master（默认，负责写与定时任务）或 worker（纯读节点）
	ClusterRole = ClusterRoleMaster
)

const (
	ClusterRoleMaster = "master"
	ClusterRoleWorker = "worker"
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

	filmPictureUploadDirContainer = "/app/static/upload/gallery"
	filmPictureUploadDirLocal     = "./static/upload/gallery"
	FilmPictureAccess             = "/api/upload/pic/poster/"
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
	// RedisKeyPrefix 项目 Redis 统一前缀，便于启动时整批清理。
	RedisKeyPrefix = "EcoHub"
	// RedisProjectKeyPattern 项目 Redis 键全量扫描模式。
	RedisProjectKeyPattern = RedisKeyPrefix + ":*"

	// CategoryTreeKey 分类树 key
	CategoryTreeKey = RedisKeyPrefix + ":Category:Tree"
	// ActiveCategoryTreeKey 活跃分类树缓存 key
	ActiveCategoryTreeKey = RedisKeyPrefix + ":Category:ActiveTree"
	// ConfigCacheTTL 管理员写入控制的配置类 key 有效期 (以长 TTL 最大化命中率)
	ConfigCacheTTL = time.Hour * 24
	// LatestReleaseCacheKey GitHub 最新正式版 Release 缓存
	LatestReleaseCacheKey = RedisKeyPrefix + ":Version:LatestRelease"
	// LatestReleasePreCacheKey 当前为测试版时，含 pre-release 的最新缓存
	LatestReleasePreCacheKey = RedisKeyPrefix + ":Version:LatestRelease:pre"
	// LatestReleaseCacheTTL 版本检查缓存，避免打满 GitHub 匿名限额
	LatestReleaseCacheTTL = time.Hour

	// SearchTags 搜索分类标签缓存 key (前缀)
	SearchTags = RedisKeyPrefix + ":Search:Tags"
	// SearchTagsVersionKey 搜索分类标签缓存版本号 key
	SearchTagsVersionKey = RedisKeyPrefix + ":Search:Tags:Version"
	// TVBoxConfigCacheKey TVBox 分类及筛选配置缓存 key
	TVBoxConfigCacheKey = RedisKeyPrefix + ":TVBox:Config"
	// TVBoxNetworkConfigCacheKey TVBox/影视仓一键网络配置缓存 key 前缀
	TVBoxNetworkConfigCacheKey = RedisKeyPrefix + ":TVBox:NetworkConfig"
	// IndexPageCacheKey 首页数据缓存 key
	IndexPageCacheKey = RedisKeyPrefix + ":Index:Page"
	// IndexDailyUpdatesCacheKey 首页「每日更新」候选池短缓存；接口每次从池中随机抽取
	IndexDailyUpdatesCacheKey = RedisKeyPrefix + ":Index:DailyUpdates:v4"
	// CategoryVersionKey 分类版本号缓存 key
	CategoryVersionKey = RedisKeyPrefix + ":Category:Version"
	// RuleVersionKey 分类规则版本号缓存 key
	RuleVersionKey = RedisKeyPrefix + ":Rule:Version"
	// TVBoxList TVBox 列表页缓存前缀
	TVBoxList = RedisKeyPrefix + ":TVBox:List"
	// SnapshotActiveVersionKey 前台只读影片列表快照当前生效版本
	SnapshotActiveVersionKey = RedisKeyPrefix + ":Snapshot:ActiveVersion"
	// SnapshotBuildVersionKey 最近一次快照构建版本
	SnapshotBuildVersionKey = RedisKeyPrefix + ":Snapshot:BuildVersion"
	// SnapshotRevisionKey 快照修订版本号（增量或全量快照变动时自增，供多节点同步读模型与搜索索引）
	SnapshotRevisionKey = RedisKeyPrefix + ":Snapshot:Revision"
	// FilmClassifyCacheKey 分类首页快照缓存前缀
	FilmClassifyCacheKey = RedisKeyPrefix + ":FilmClassify"
	// FilmClassifySearchKey 分类筛选快照缓存前缀
	FilmClassifySearchKey = RedisKeyPrefix + ":FilmClassifySearch"

	// VirtualPictureKey 待同步图片临时存储 key
	VirtualPictureKey = RedisKeyPrefix + ":Gallery:VirtualPicture"
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
	// NotifyBotPollerLockKey Telegram getUpdates 跨实例领导锁（启动清缓存时须保留）
	NotifyBotPollerLockKey = RedisKeyPrefix + ":Notify:BotPoller:Lock"
	// BannersKey 轮播组件key
	BannersKey = RedisKeyPrefix + ":Config:Banners"

	// DefaultUpdateSpec 每30分钟执行一次
	DefaultUpdateSpec = "0 */30 * * * ?"
	// EveryWeekSpec 每天凌晨4点执行一次
	EveryWeekSpec = "0 0 4 * * *"
	// EveryDaySpec 每天凌晨0点执行一次
	EveryDaySpec = "0 0 0 * * *"
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

// init func for loading from env
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

	ClusterRole = parseClusterRole()
	fmt.Printf("[Config] 集群角色: %s (CronEnabled: %v)\n", ClusterRole, IsCronEnabled())
}

func parseClusterRole() string {
	if strings.ToLower(strings.TrimSpace(os.Getenv("CLUSTER_ROLE"))) == ClusterRoleWorker {
		return ClusterRoleWorker
	}
	return ClusterRoleMaster
}

// IsClusterWorker 是否为集群 Worker 纯读节点
func IsClusterWorker() bool {
	return ClusterRole == ClusterRoleWorker
}

// IsCronEnabled 检查当前节点是否应启动定时采集任务调度器（Worker 节点自动禁用）
func IsCronEnabled() bool {
	return ClusterRole != ClusterRoleWorker
}

// resolveFilmPictureUploadDir 容器内写死发布卷路径；仅非容器（本地 go run）用相对路径。
func resolveFilmPictureUploadDir() string {
	if runningInContainer() {
		return filmPictureUploadDirContainer
	}
	return filmPictureUploadDirLocal
}

func runningInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	switch filepath.Dir(exe) {
	case "/app", "/app/server":
		return true
	default:
		return false
	}
}

// ContainerUploadVolumeOK 非容器或已挂 /app/static/upload 为 true。
func ContainerUploadVolumeOK() bool {
	if !runningInContainer() {
		return true
	}
	return uploadPathIsMounted()
}

// EnsureContainerUploadVolume 未挂卷时返回错误供启动日志，不阻断启动。
func EnsureContainerUploadVolume() error {
	if ContainerUploadVolumeOK() {
		return nil
	}
	return fmt.Errorf("素材目录 %s 未挂载发布卷 /app/static/upload", FilmPictureUploadDir)
}

func uploadPathIsMounted() bool {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		switch fields[4] {
		case "/app/static/upload", "/app/static/upload/gallery":
			return true
		}
	}
	return false
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
