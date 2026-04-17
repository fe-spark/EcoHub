package config

import (
	"fmt"
	"os"
	"strconv"
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
)

const (
	// MAXGoroutine max goroutine, 执行spider中对协程的数量限制
	MAXGoroutine = 10

	FilmPictureUploadDir = "./static/upload/gallery"
	FilmPictureAccess    = "/api/upload/pic/poster/"
)

// -------------------------redis key-----------------------------------
const (
	// CategoryTreeKey 分类树 key
	CategoryTreeKey = "Category:Tree"
	// ActiveCategoryTreeKey 活跃分类树缓存 key
	ActiveCategoryTreeKey = "Category:ActiveTree"
	// ConfigCacheTTL 管理员写入控制的配置类 key 有效期 (以长 TTL 最大化命中率)
	ConfigCacheTTL = time.Hour * 24

	// SearchTags 搜索分类标签缓存 key (前缀)
	SearchTags = "Search:Tags"
	// TVBoxConfigCacheKey TVBox 分类及筛选配置缓存 key
	TVBoxConfigCacheKey = "TVBox:Config"
	// IndexPageCacheKey 首页数据缓存 key
	IndexPageCacheKey = "Index:Page"
	// CategoryVersionKey 分类版本号缓存 key
	CategoryVersionKey = "Category:Version"
	// TVBoxList TVBox 列表页缓存前缀
	TVBoxList = "TVBox:List"

	// VirtualPictureKey 待同步图片临时存储 key
	VirtualPictureKey = "Gallery:VirtualPicture"
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
	SiteConfigBasic = "Config:Site:Basic"
	// BannersKey 轮播组件key
	BannersKey = "Config:Banners"

	// DefaultUpdateSpec 每20分钟执行一次
	DefaultUpdateSpec = "0 */20 * * * ?"
	// EveryWeekSpec 每周日凌晨4点更新一次
	EveryWeekSpec = "0 0 4 * * 0"
	// EveryDaySpec 每天凌晨0点执行一次
	EveryDaySpec = "0 0 0 * * *"
	// DefaultUpdateTime 每次采集最近 3 小时内更新的影片
	DefaultUpdateTime = 3
	// DefaultSpiderInterval 默认采集间隔 (ms)，当站点未配置时使用
	DefaultSpiderInterval = 500
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
	UserTokenKey     = "User:Token:%d"
)

// init func for loading from env
func init() {
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
}

// GetRootMysqlDsn 获取不带数据库名的 DSN，用于 CREATE DATABASE 等管理操作
func GetRootMysqlDsn() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/?charset=utf8mb4&parseTime=True&loc=Local",
		MysqlUser, MysqlPass, MysqlHost, MysqlPort)
}
