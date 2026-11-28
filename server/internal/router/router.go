package router

import (
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"server/internal/access"
	"server/internal/admin"
	"server/internal/config"
	"server/internal/film"
	"server/internal/infra/syslog"
	"server/internal/middleware"
	"server/internal/notify"
	"server/internal/portal"
	"server/internal/tmdb"
	"server/internal/user"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
	"server/internal/backup"
	"server/internal/banner"
	"server/internal/collectctl"
	"server/internal/danmaku"
	"server/internal/file"
	"server/internal/mapping"
	"server/internal/proxy"
	"server/internal/recommend"
	"server/internal/siteops"
)

func serveFilmPicture(c *gin.Context) {
	name := filepath.Base(c.Param("name"))
	if !utils.IsGalleryImage(name) {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.File(filepath.Join(config.FilmPictureUploadDir, name))
}

func SetupRouter() *gin.Engine {
	// 部分运行环境系统 mime 库可能缺失 webp/ico，主动注册保证图库可渲染
	_ = mime.AddExtensionType(".webp", "image/webp")
	_ = mime.AddExtensionType(".ico", "image/x-icon")

	r := gin.New()
	if err := r.SetTrustedProxies(config.TrustedProxies); err != nil {
		syslog.Warnf("[HTTP] 设置 TrustedProxies 失败，回退本地环回: %v", err)
		_ = r.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	}
	r.Use(middleware.AccessLog())
	r.Use(gin.Recovery())
	r.Use(middleware.Cors())
	r.Use(httpPolicyMiddleware())
	r.Use(maintenanceGuardMiddleware())

	r.GET(config.FilmPictureAccess+"*name", serveFilmPicture)

	api := r.Group("/api")

	api.GET(`/config/basic`, admin.ManageHd.SiteBasicConfig)
	api.POST(`/login`, user.UserHd.Login)
	api.POST(`/logout`, middleware.AuthToken(), user.UserHd.Logout)

	// 前台业务接口，开启私有化模式时需携带有效登录态
	frontApi := api.Group("/", middleware.PrivateAccessGuard())
	{
		frontApi.GET(`/home`, portal.IndexHd.Index)
		frontApi.GET(`/daily-updates`, portal.IndexHd.DailyUpdates)
		frontApi.GET(`/categories`, portal.IndexHd.CategoriesInfo)
		frontApi.GET(`/categories/overview`, portal.IndexHd.FilmClassify)
		frontApi.GET(`/films/play-info`, portal.IndexHd.FilmPlayInfo)
		frontApi.GET(`/films/related`, portal.IndexHd.FilmRelate)
		frontApi.GET(`/films/search`, portal.IndexHd.SearchFilm)
		frontApi.GET(`/films/filter`, portal.IndexHd.FilmTagSearch)
		frontApi.GET(`/search/hot-keywords`, portal.IndexHd.HotKeywords)
		frontApi.POST(`/stats/view`, access.AccessHd.TrackView)
		frontApi.GET(`/film/danmaku`, danmaku.DanmakuHd.GetDanmaku)
		frontApi.POST(`/film/danmaku`, danmaku.DanmakuHd.PostDanmaku)
	}

	manageRoute := api.Group(`/manage`)
	manageRoute.Use(middleware.AuthToken(), middleware.WriteAccess())
	{
		manageRoute.GET(`/index`, admin.ManageHd.ManageIndex)
		manageRoute.GET(`/version`, admin.ManageHd.AppVersion)
		manageRoute.POST(`/version/upgrade`, middleware.AdminAccess(), admin.ManageHd.UpgradeApp)

		// 系统相关
		sysConfig := manageRoute.Group(`/config`)
		{
			sysConfig.GET(`/basic`, admin.ManageHd.SiteBasicConfig)
			sysConfig.POST(`/basic`, middleware.AdminAccess(), admin.ManageHd.UpdateSiteBasic)

			sysConfig.GET(`/access`, admin.ManageHd.SiteAccessConfig)
			sysConfig.POST(`/access`, middleware.AdminAccess(), admin.ManageHd.UpdateSiteAccess)

			sysConfig.GET(`/media-proxy`, admin.ManageHd.SiteMediaProxyConfig)
			sysConfig.POST(`/media-proxy`, middleware.AdminAccess(), admin.ManageHd.UpdateSiteMediaProxy)

			sysConfig.GET(`/danmaku`, admin.ManageHd.SiteDanmakuConfig)
			sysConfig.POST(`/danmaku`, middleware.AdminAccess(), admin.ManageHd.UpdateSiteDanmaku)
			sysConfig.POST(`/danmaku/test`, middleware.AdminAccess(), admin.ManageHd.TestSiteDanmaku)

			sysConfig.GET(`/tip`, admin.ManageHd.SiteTipConfig)
			sysConfig.POST(`/tip`, middleware.AdminAccess(), admin.ManageHd.UpdateSiteTip)

			sysConfig.GET(`/notice`, admin.ManageHd.SiteNoticeConfig)
			sysConfig.POST(`/notice`, middleware.AdminAccess(), admin.ManageHd.UpdateSiteNotice)

			// 配置备份：导出/导入（需管理密码，不含影视库存与账号，仅超级管理员）
			sysConfig.POST(`/backup/export`, middleware.AdminAccess(), backup.BackupHd.ExportConfigBackup)
			sysConfig.POST(`/backup/import`, middleware.AdminAccess(), backup.BackupHd.ImportConfigBackup)
		}
		systemLog := manageRoute.Group(`/system/logs`, middleware.AdminAccess())
		{
			systemLog.GET(`/stream`, admin.SystemLogHd.Stream)
			systemLog.GET(`/delta`, admin.SystemLogHd.Delta)
		}

		accessRoute := manageRoute.Group(`/access`)
		{
			accessRoute.GET(`/status`, access.AccessHd.Status)
			accessRoute.GET(`/config`, access.AccessHd.GetConfig)
			accessRoute.POST(`/config`, middleware.AdminAccess(), access.AccessHd.UpdateConfig)
			accessRoute.GET(`/overview`, access.AccessHd.Overview)
			accessRoute.GET(`/sources`, access.AccessHd.SourceCalls)
			accessRoute.GET(`/tops`, access.AccessHd.Tops)
			accessRoute.GET(`/logs`, access.AccessHd.Logs)
			accessRoute.GET(`/stats`, access.AccessHd.DataStats)
			accessRoute.POST(`/clean`, middleware.AdminAccess(), access.AccessHd.CleanData)
		}

		// 首页轮播管理：按采集站区分
		bannerRoute := manageRoute.Group(`/banners`)
		{
			bannerRoute.GET(``, banner.BannerHd.BannerList)
			bannerRoute.GET(`/categories`, banner.BannerHd.BannerCategories)
			bannerRoute.GET(`/films`, banner.BannerHd.BannerFilms)
			bannerRoute.POST(``, banner.BannerHd.BannerCreate)
			bannerRoute.PUT(`/order`, banner.BannerHd.BannerOrder)
			bannerRoute.POST(`/generate`, banner.BannerHd.BannerGenerate)
			bannerRoute.PUT(`/deck`, banner.BannerHd.BannerDeck)
			bannerRoute.PATCH(`/:id`, banner.BannerHd.BannerUpdate)
			bannerRoute.DELETE(`/:id`, banner.BannerHd.BannerDelete)
		}

		// 首页推荐管理：按采集站区分分组与影片
		recommendRoute := manageRoute.Group(`/recommends`)
		{
			recommendRoute.GET(``, recommend.RecommendHd.List)
			recommendRoute.POST(`/presets`, recommend.RecommendHd.InitPresets)
			recommendRoute.POST(`/categories/import`, recommend.RecommendHd.ImportCategories)
			recommendRoute.POST(`/groups`, recommend.RecommendHd.CreateGroup)
			recommendRoute.PATCH(`/groups/:id`, recommend.RecommendHd.UpdateGroup)
			recommendRoute.DELETE(`/groups/:id`, recommend.RecommendHd.DeleteGroup)
			recommendRoute.PUT(`/groups/order`, recommend.RecommendHd.ReorderGroups)
			recommendRoute.POST(`/groups/:id/items`, recommend.RecommendHd.AddItem)
			recommendRoute.PUT(`/groups/:id/items/order`, recommend.RecommendHd.ReorderItems)
			recommendRoute.DELETE(`/groups/:id/items`, recommend.RecommendHd.ClearGroupItems)
			recommendRoute.POST(`/groups/:id/sync`, recommend.RecommendHd.SyncGroup)
			recommendRoute.POST(`/sync-all`, recommend.RecommendHd.SyncAll)
			recommendRoute.DELETE(`/items/:itemId`, recommend.RecommendHd.DeleteItem)
			recommendRoute.POST(`/generate`, recommend.RecommendHd.Generate)
			recommendRoute.PUT(`/deck`, recommend.RecommendHd.PublishDeck)
		}

		// 映射规则管理
		mappingRoute := manageRoute.Group(`/mapping`)
		{
			mappingRoute.GET(`/group/list`, mapping.MappingHd.MappingRuleGroups)
			mappingRoute.GET(`/rule/list`, mapping.MappingHd.MappingRuleList)
			mappingRoute.GET(`/rule/context`, mapping.MappingHd.MappingRuleContext)
			mappingRoute.POST(`/rule/save`, mapping.MappingHd.MappingRuleSave)
			mappingRoute.POST(`/rule/delete`, mapping.MappingHd.MappingRuleDelete)
			mappingRoute.POST(`/rule/reload`, mapping.MappingHd.MappingRuleReload)
		}

		// 用户相关
		userRoute := manageRoute.Group(`/user`)
		{
			userRoute.GET(`/info`, user.UserHd.UserInfo)
			userRoute.POST(`/profile`, user.UserHd.UserProfile)
			userRoute.GET(`/list`, middleware.AdminAccess(), user.UserHd.UserListPage)
			userRoute.POST(`/save`, middleware.AdminAccess(), user.UserHd.UserSave)
			userRoute.POST(`/delete`, middleware.AdminAccess(), user.UserHd.UserDelete)
		}

		// 主站状态与切换（切换仅超管，D4；查询保持默认可写权限供只读展示）
		manageRoute.GET(`/primary`, collectctl.CollectHd.PrimaryState)
		manageRoute.PUT(`/primary`, middleware.AdminAccess(), collectctl.CollectHd.SwitchPrimary)

		// 采集相关
		collectGroup := manageRoute.Group(`/collect`)
		{
			collectGroup.GET(`/list`, collectctl.CollectHd.FilmSourceList)
			collectGroup.GET(`/progress-stream`, collectctl.CollectHd.ProgressStream)
			collectGroup.GET(`/find`, collectctl.CollectHd.FindFilmSource)
			collectGroup.POST(`/test`, collectctl.CollectHd.FilmSourceTest)
			collectGroup.POST(`/save`, collectctl.CollectHd.FilmSourceSave)
			collectGroup.POST(`/state`, collectctl.CollectHd.FilmSourceBatchChange)
			collectGroup.POST(`/delete`, collectctl.CollectHd.FilmSourceDelBatch)
			collectGroup.POST(`/delete/cancel`, collectctl.CollectHd.FilmSourceDelCancel)
			collectGroup.POST(`/check/all`, collectctl.CollectHd.FilmSourceCheckAll)
			collectGroup.GET(`/options`, collectctl.CollectHd.GetNormalFilmSource)
			collectGroup.POST(`/sort`, collectctl.CollectHd.FilmSourceSort)
			collectGroup.POST(`/category/sync`, collectctl.CollectHd.FilmSourceSyncCategory)
			collectGroup.POST(`/page-total`, collectctl.CollectHd.RefreshSitePageTotal)

			collectGroup.GET(`/history/list`, collectctl.CollectRecordHd.HistoryList)
			collectGroup.POST(`/history/delete`, collectctl.CollectRecordHd.HistoryDelete)
			collectGroup.POST(`/history/clear`, collectctl.CollectRecordHd.HistoryClear)
			collectGroup.POST(`/history/retry`, collectctl.CollectRecordHd.HistoryRetry)
		}

		// 定时任务相关
		collectCron := manageRoute.Group(`/cron`)
		{
			collectCron.GET(`/list`, collectctl.CronHd.FilmCronTaskList)
			collectCron.GET(`/find`, collectctl.CronHd.GetFilmCronTask)
			collectCron.POST(`/save`, collectctl.CronHd.FilmCronSave)
			collectCron.POST(`/state`, collectctl.CronHd.ChangeTaskState)
			collectCron.POST(`/run`, collectctl.CronHd.RunFilmCronTask)
		}

		// spider 数据采集
		spiderRoute := manageRoute.Group(`/spider`)
		{
			spiderRoute.POST(`/start`, collectctl.SpiderHd.StarSpider)
			spiderRoute.POST(`/stop`, collectctl.SpiderHd.StopTask)
			spiderRoute.POST(`/clear`, middleware.AdminAccess(), siteops.ResetHd.ClearAllFilm)
			spiderRoute.GET(`/clear/progress`, middleware.AdminAccess(), siteops.ResetHd.ResetProgressStream)
			spiderRoute.GET(`/clear/stats`, collectctl.SpiderHd.InventoryStats)
		}

		// filmManage 影视管理
		filmRoute := manageRoute.Group(`/film`)
		{
			filmRoute.GET(`/detail`, film.FilmHd.FilmDetail)
			filmRoute.POST(`/save`, film.FilmHd.FilmSave)
			filmRoute.POST(`/refresh`, film.FilmHd.FilmRefresh)
			filmRoute.GET(`/search/list`, film.FilmHd.FilmSearchPage)
			filmRoute.GET(`/search/options`, film.FilmHd.FilmSearchOptions)
			filmRoute.POST(`/delete`, film.FilmHd.FilmDelete)

			filmRoute.GET(`/class/tree`, film.FilmHd.FilmClassTree)
			filmRoute.POST(`/class/tree`, film.FilmHd.SaveFilmClassTree)
		}

		// TMDB 刮削相关
		tmdbRoute := manageRoute.Group(`/tmdb`)
		{
			tmdbRoute.GET(`/config`, tmdb.TMDBHd.GetConfig)
			tmdbRoute.POST(`/config`, middleware.AdminAccess(), tmdb.TMDBHd.UpdateConfig)
			tmdbRoute.POST(`/config/test`, middleware.AdminAccess(), tmdb.TMDBHd.TestConfig)
			tmdbRoute.GET(`/search`, tmdb.TMDBHd.Search)
			tmdbRoute.POST(`/apply`, tmdb.TMDBHd.Apply)
			tmdbRoute.GET(`/prefill`, tmdb.TMDBHd.Prefill)
		}

		// 采集网络代理相关
		proxyRoute := manageRoute.Group(`/proxy`)
		{
			proxyRoute.GET(`/config`, proxy.ProxyHd.GetConfig)
			proxyRoute.POST(`/config`, middleware.AdminAccess(), proxy.ProxyHd.UpdateConfig)
			proxyRoute.POST(`/config/test`, middleware.AdminAccess(), proxy.ProxyHd.TestProxy)
		}

		// 消息通知相关
		notifyRoute := manageRoute.Group(`/notify`)
		{
			notifyRoute.GET(`/config`, notify.NotifyHd.GetConfig)
			notifyRoute.POST(`/config`, middleware.AdminAccess(), notify.NotifyHd.UpdateConfig)
			notifyRoute.POST(`/config/test`, middleware.AdminAccess(), notify.NotifyHd.TestNotify)
		}

		// 文件管理
		fileRoute := manageRoute.Group(`/file`)
		{
			fileRoute.POST(`/upload`, file.FileHd.Upload)
			fileRoute.POST(`/rename`, file.FileHd.RenameFile)
			fileRoute.POST(`/delete`, file.FileHd.DelFile)
			fileRoute.GET(`/list`, file.FileHd.PhotoWall)
		}
	}

	provideRoute := api.Group(`/provide`, middleware.ProvideKeyGuard())
	{
		provideRoute.GET(`/vod`, portal.ProvideHd.HandleProvide)
		provideRoute.GET(`/tvbox`, portal.ProvideHd.HandleProvideConfig)
		provideRoute.GET(`/app`, portal.ProvideHd.HandleProvideApp)
	}

	return r
}

const defaultRequestBodyMaxBytes = 2 << 20 // 2MB

func isSSERoutePath(path string) bool {
	return path == "/api/manage/system/logs/stream" ||
		path == "/api/manage/collect/progress-stream" ||
		path == "/api/manage/spider/clear" ||
		path == "/api/manage/spider/clear/progress"
}

func hasDedicatedBodyLimitPath(path string) bool {
	return path == "/api/manage/file/upload" || path == "/api/manage/config/backup/import"
}

// httpPolicyMiddleware 统一设置常规路由请求体大小上限与读写超时（放行大文件上传接口与长连接 SSE）。
func httpPolicyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if !isSSERoutePath(path) {
			rc := http.NewResponseController(c.Writer)
			readTimeout := 15 * time.Second
			if hasDedicatedBodyLimitPath(path) {
				readTimeout = 120 * time.Second
			}
			_ = rc.SetReadDeadline(time.Now().Add(readTimeout))
			_ = rc.SetWriteDeadline(time.Now().Add(60 * time.Second))
		}
		if c.Request.Body != nil &&
			c.Request.Method != http.MethodGet &&
			c.Request.Method != http.MethodHead &&
			!hasDedicatedBodyLimitPath(path) &&
			!strings.HasPrefix(path, config.FilmPictureAccess) {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, defaultRequestBodyMaxBytes)
		}
		c.Next()
	}
}

func maintenanceGuardMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !siteops.ResetSvc.SiteMaintenanceActive() {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		if path == "/api/config/basic" ||
			path == "/api/manage/spider/clear" ||
			path == "/api/manage/spider/clear/progress" ||
			path == "/api/manage/spider/clear/stats" {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"code": 1,
			"msg":  "系统维护中，请稍后再试",
		})
	}
}
