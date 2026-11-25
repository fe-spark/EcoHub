package router

import (
	"mime"
	"net/http"
	"path/filepath"

	"server/internal/access"
	"server/internal/admin"
	"server/internal/collect"
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
)

func serveFilmPicture(c *gin.Context) {
	name := filepath.Base(c.Param("name"))
	if !utils.IsGalleryImage(name) {
		c.Status(http.StatusNotFound)
		return
	}
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
		frontApi.GET(`/film/danmaku`, portal.DanmakuHd.GetDanmaku)
		frontApi.POST(`/film/danmaku`, portal.DanmakuHd.PostDanmaku)
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
			sysConfig.POST(`/backup/export`, middleware.AdminAccess(), admin.ManageHd.ExportConfigBackup)
			sysConfig.POST(`/backup/import`, middleware.AdminAccess(), admin.ManageHd.ImportConfigBackup)
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
		banner := manageRoute.Group(`/banners`)
		{
			banner.GET(``, film.BannerHd.BannerList)
			banner.GET(`/categories`, film.BannerHd.BannerCategories)
			banner.GET(`/films`, film.BannerHd.BannerFilms)
			banner.POST(``, film.BannerHd.BannerCreate)
			banner.PUT(`/order`, film.BannerHd.BannerOrder)
			banner.POST(`/generate`, film.BannerHd.BannerGenerate)
			banner.PUT(`/deck`, film.BannerHd.BannerDeck)
			banner.PATCH(`/:id`, film.BannerHd.BannerUpdate)
			banner.DELETE(`/:id`, film.BannerHd.BannerDelete)
		}

		// 映射规则管理
		mapping := manageRoute.Group(`/mapping`)
		{
			mapping.GET(`/group/list`, film.MappingHd.MappingRuleGroups)
			mapping.GET(`/rule/list`, film.MappingHd.MappingRuleList)
			mapping.GET(`/rule/context`, film.MappingHd.MappingRuleContext)
			mapping.POST(`/rule/save`, film.MappingHd.MappingRuleSave)
			mapping.POST(`/rule/delete`, film.MappingHd.MappingRuleDelete)
			mapping.POST(`/rule/reload`, film.MappingHd.MappingRuleReload)
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
		manageRoute.GET(`/primary`, collect.CollectHd.PrimaryState)
		manageRoute.PUT(`/primary`, middleware.AdminAccess(), collect.CollectHd.SwitchPrimary)

		// 采集相关
		collectGroup := manageRoute.Group(`/collect`)
		{
			collectGroup.GET(`/list`, collect.CollectHd.FilmSourceList)
			collectGroup.GET(`/progress-stream`, collect.CollectHd.ProgressStream)
			collectGroup.GET(`/find`, collect.CollectHd.FindFilmSource)
			collectGroup.POST(`/test`, collect.CollectHd.FilmSourceTest)
			collectGroup.POST(`/save`, collect.CollectHd.FilmSourceSave)
			collectGroup.POST(`/state`, collect.CollectHd.FilmSourceBatchChange)
			collectGroup.POST(`/delete`, collect.CollectHd.FilmSourceDelBatch)
			collectGroup.POST(`/delete/cancel`, collect.CollectHd.FilmSourceDelCancel)
			collectGroup.POST(`/check/all`, collect.CollectHd.FilmSourceCheckAll)
			collectGroup.GET(`/options`, collect.CollectHd.GetNormalFilmSource)
			collectGroup.POST(`/sort`, collect.CollectHd.FilmSourceSort)
			collectGroup.POST(`/category/sync`, collect.CollectHd.FilmSourceSyncCategory)
			collectGroup.POST(`/page-total`, collect.CollectHd.RefreshSitePageTotal)

			collectGroup.GET(`/history/list`, collect.CollectRecordHd.HistoryList)
			collectGroup.GET(`/history/films`, collect.CollectRecordHd.HistoryFilms)
			collectGroup.POST(`/history/delete`, collect.CollectRecordHd.HistoryDelete)
			collectGroup.POST(`/history/clear`, collect.CollectRecordHd.HistoryClear)
			collectGroup.POST(`/history/retry`, collect.CollectRecordHd.HistoryRetry)
		}

		// 定时任务相关
		collectCron := manageRoute.Group(`/cron`)
		{
			collectCron.GET(`/list`, collect.CronHd.FilmCronTaskList)
			collectCron.GET(`/find`, collect.CronHd.GetFilmCronTask)
			collectCron.POST(`/save`, collect.CronHd.FilmCronSave)
			collectCron.POST(`/state`, collect.CronHd.ChangeTaskState)
			collectCron.POST(`/run`, collect.CronHd.RunFilmCronTask)
		}

		// spider 数据采集
		spiderRoute := manageRoute.Group(`/spider`)
		{
			spiderRoute.POST(`/start`, collect.SpiderHd.StarSpider)
			spiderRoute.POST(`/stop`, collect.SpiderHd.StopTask)
			spiderRoute.POST(`/clear`, middleware.AdminAccess(), collect.SpiderHd.ClearAllFilm)
			spiderRoute.GET(`/clear/progress`, middleware.AdminAccess(), collect.SpiderHd.ResetProgressStream)
			spiderRoute.GET(`/clear/stats`, collect.SpiderHd.InventoryStats)
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
			proxyRoute.GET(`/config`, admin.ProxyHd.GetConfig)
			proxyRoute.POST(`/config`, middleware.AdminAccess(), admin.ProxyHd.UpdateConfig)
			proxyRoute.POST(`/config/test`, middleware.AdminAccess(), admin.ProxyHd.TestProxy)
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
			fileRoute.POST(`/upload`, admin.FileHd.Upload)
			fileRoute.POST(`/rename`, admin.FileHd.RenameFile)
			fileRoute.POST(`/delete`, admin.FileHd.DelFile)
			fileRoute.GET(`/list`, admin.FileHd.PhotoWall)
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
