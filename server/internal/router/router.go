package router

import (
	"mime"
	"net/http"
	"path/filepath"

	"server/internal/config"
	"server/internal/handler"
	"server/internal/infra/syslog"
	"server/internal/middleware"
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

	api.GET(`/config/basic`, handler.ManageHd.SiteBasicConfig)
	api.POST(`/login`, handler.UserHd.Login)
	api.POST(`/logout`, middleware.AuthToken(), handler.UserHd.Logout)

	// 前台业务接口，开启私有化模式时需携带有效登录态
	frontApi := api.Group("/", middleware.PrivateAccessGuard())
	{
		frontApi.GET(`/home`, handler.IndexHd.Index)
		frontApi.GET(`/daily-updates`, handler.IndexHd.DailyUpdates)
		frontApi.GET(`/categories`, handler.IndexHd.CategoriesInfo)
		frontApi.GET(`/categories/overview`, handler.IndexHd.FilmClassify)
		frontApi.GET(`/films/play-info`, handler.IndexHd.FilmPlayInfo)
		frontApi.GET(`/films/related`, handler.IndexHd.FilmRelate)
		frontApi.GET(`/films/search`, handler.IndexHd.SearchFilm)
		frontApi.GET(`/films/filter`, handler.IndexHd.FilmTagSearch)
		frontApi.GET(`/search/hot-keywords`, handler.IndexHd.HotKeywords)
		frontApi.POST(`/stats/view`, handler.AccessHd.TrackView)
		frontApi.GET(`/film/danmaku`, handler.DanmakuHd.GetDanmaku)
		frontApi.POST(`/film/danmaku`, handler.DanmakuHd.PostDanmaku)
	}

	manageRoute := api.Group(`/manage`)
	manageRoute.Use(middleware.AuthToken(), middleware.WriteAccess())
	{
		manageRoute.GET(`/index`, handler.ManageHd.ManageIndex)
		manageRoute.GET(`/version`, handler.ManageHd.AppVersion)
		manageRoute.POST(`/version/upgrade`, middleware.AdminAccess(), handler.ManageHd.UpgradeApp)

		// 系统相关
		sysConfig := manageRoute.Group(`/config`)
		{
			sysConfig.GET(`/basic`, handler.ManageHd.SiteBasicConfig)
			sysConfig.POST(`/basic`, handler.ManageHd.UpdateSiteBasic)

			sysConfig.GET(`/access`, handler.ManageHd.SiteAccessConfig)
			sysConfig.POST(`/access`, middleware.AdminAccess(), handler.ManageHd.UpdateSiteAccess)

			sysConfig.GET(`/danmaku`, handler.ManageHd.SiteDanmakuConfig)
			sysConfig.POST(`/danmaku`, middleware.AdminAccess(), handler.ManageHd.UpdateSiteDanmaku)

			sysConfig.GET(`/tip`, handler.ManageHd.SiteTipConfig)
			sysConfig.POST(`/tip`, handler.ManageHd.UpdateSiteTip)

			sysConfig.GET(`/notice`, handler.ManageHd.SiteNoticeConfig)
			sysConfig.POST(`/notice`, handler.ManageHd.UpdateSiteNotice)

			// 配置备份：导出/导入（需管理密码，不含影视库存与账号，仅超级管理员）
			sysConfig.POST(`/backup/export`, middleware.AdminAccess(), handler.ManageHd.ExportConfigBackup)
			sysConfig.POST(`/backup/import`, middleware.AdminAccess(), handler.ManageHd.ImportConfigBackup)
		}
		systemLog := manageRoute.Group(`/system/logs`, middleware.AdminAccess())
		{
			systemLog.GET(`/stream`, handler.SystemLogHd.Stream)
			systemLog.GET(`/delta`, handler.SystemLogHd.Delta)
		}

		accessRoute := manageRoute.Group(`/access`)
		{
			accessRoute.GET(`/status`, handler.AccessHd.Status)
			accessRoute.GET(`/overview`, middleware.AdminAccess(), handler.AccessHd.Overview)
			accessRoute.GET(`/sources`, middleware.AdminAccess(), handler.AccessHd.SourceCalls)
			accessRoute.GET(`/tops`, middleware.AdminAccess(), handler.AccessHd.Tops)
			accessRoute.GET(`/logs`, middleware.AdminAccess(), handler.AccessHd.Logs)
			accessRoute.GET(`/stats`, middleware.AdminAccess(), handler.AccessHd.DataStats)
			accessRoute.POST(`/clean`, middleware.AdminAccess(), handler.AccessHd.CleanData)
		}

		// 首页轮播管理 (手动排片 + 一键排片 + 草稿刮削)
		banner := manageRoute.Group(`banner`)
		{
			banner.GET(`/list`, handler.ManageHd.BannerList)
			banner.POST(`/save`, handler.ManageHd.BannerSave)
			banner.POST(`/delete`, handler.ManageHd.BannerDelete)
			banner.POST(`/deck`, handler.ManageHd.BannerSaveDeck)
			banner.GET(`/quick-config`, handler.ManageHd.BannerQuickConfigGet)
			banner.POST(`/quick-config`, handler.ManageHd.BannerQuickConfigSave)
			banner.POST(`/quick-generate`, handler.ManageHd.BannerQuickGenerate)
		}

		// 映射规则管理
		mapping := manageRoute.Group(`/mapping`)
		{
			mapping.GET(`/group/list`, handler.ManageHd.MappingRuleGroups)
			mapping.GET(`/rule/list`, handler.ManageHd.MappingRuleList)
			mapping.POST(`/rule/check`, handler.ManageHd.MappingRuleCheck)
			mapping.POST(`/rule/save`, handler.ManageHd.MappingRuleSave)
			mapping.POST(`/rule/delete`, handler.ManageHd.MappingRuleDelete)
			mapping.POST(`/rule/reload`, handler.ManageHd.MappingRuleReload)
		}

		// 用户相关
		userRoute := manageRoute.Group(`/user`)
		{
			userRoute.GET(`/info`, handler.UserHd.UserInfo)
			userRoute.GET(`/list`, middleware.AdminAccess(), handler.UserHd.UserListPage)
			userRoute.POST(`/save`, middleware.AdminAccess(), handler.UserHd.UserSave)
			userRoute.POST(`/delete`, middleware.AdminAccess(), handler.UserHd.UserDelete)
		}

		// 采集相关
		collect := manageRoute.Group(`/collect`)
		{
			collect.GET(`/list`, handler.CollectHd.FilmSourceList)
			collect.GET(`/progress-stream`, handler.CollectHd.ProgressStream)
			collect.GET(`/find`, handler.CollectHd.FindFilmSource)
			collect.POST(`/test`, handler.CollectHd.FilmSourceTest)
			collect.POST(`/save`, handler.CollectHd.FilmSourceSave)
			collect.POST(`/state`, handler.CollectHd.FilmSourceBatchChange)
			collect.POST(`/delete`, handler.CollectHd.FilmSourceDelBatch)
			collect.POST(`/check/all`, handler.CollectHd.FilmSourceCheckAll)
			collect.GET(`/options`, handler.CollectHd.GetNormalFilmSource)
			collect.POST(`/sort`, handler.CollectHd.FilmSourceSort)
			collect.POST(`/category/sync`, handler.CollectHd.FilmSourceSyncCategory)
			collect.POST(`/page-total`, handler.CollectHd.RefreshSitePageTotal)

			collect.GET(`/record/list`, handler.CollectHd.FailureRecordList)
			collect.GET(`/record/stream`, handler.CollectHd.RecordStream)
			collect.POST(`/record/retry`, handler.CollectHd.CollectRecover)
			collect.POST(`/record/clear`, handler.CollectHd.ClearRecords)
		}

		// 定时任务相关
		collectCron := manageRoute.Group(`/cron`)
		{
			collectCron.GET(`/list`, handler.CronHd.FilmCronTaskList)
			collectCron.GET(`/find`, handler.CronHd.GetFilmCronTask)
			collectCron.POST(`/save`, handler.CronHd.FilmCronSave)
			collectCron.POST(`/state`, handler.CronHd.ChangeTaskState)
			collectCron.POST(`/run`, handler.CronHd.RunFilmCronTask)
		}

		// spider 数据采集
		spiderRoute := manageRoute.Group(`/spider`)
		{
			spiderRoute.POST(`/start`, handler.SpiderHd.StarSpider)
			spiderRoute.POST(`/stop`, handler.SpiderHd.StopTask)
			spiderRoute.POST(`/clear`, middleware.AdminAccess(), handler.SpiderHd.ClearAllFilm)
			spiderRoute.GET(`/clear/progress`, middleware.AdminAccess(), handler.SpiderHd.ResetProgressStream)
			spiderRoute.GET(`/clear/stats`, handler.SpiderHd.InventoryStats)
		}

		// filmManage 影视管理
		filmRoute := manageRoute.Group(`/film`)
		{
			filmRoute.POST(`/save`, handler.FilmHd.FilmSave)
			filmRoute.POST(`/refresh`, handler.FilmHd.FilmRefresh)
			filmRoute.GET(`/search/list`, handler.FilmHd.FilmSearchPage)
			filmRoute.GET(`/search/options`, handler.FilmHd.FilmSearchOptions)
			filmRoute.POST(`/delete`, handler.FilmHd.FilmDelete)

			filmRoute.GET(`/class/tree`, handler.FilmHd.FilmClassTree)
			filmRoute.POST(`/class/tree`, handler.FilmHd.SaveFilmClassTree)
		}

		// TMDB 刮削相关
		tmdbRoute := manageRoute.Group(`/tmdb`)
		{
			tmdbRoute.GET(`/config`, handler.TMDBHd.GetConfig)
			tmdbRoute.POST(`/config`, middleware.AdminAccess(), handler.TMDBHd.UpdateConfig)
			tmdbRoute.POST(`/config/test`, middleware.AdminAccess(), handler.TMDBHd.TestConfig)
			tmdbRoute.GET(`/search`, handler.TMDBHd.Search)
			tmdbRoute.POST(`/scrape`, handler.TMDBHd.Scrape)
			tmdbRoute.POST(`/apply`, handler.TMDBHd.Apply)
			tmdbRoute.GET(`/prefill`, handler.TMDBHd.Prefill)
		}

		// 采集网络代理相关
		proxyRoute := manageRoute.Group(`/proxy`)
		{
			proxyRoute.GET(`/config`, handler.ProxyHd.GetConfig)
			proxyRoute.POST(`/config`, middleware.AdminAccess(), handler.ProxyHd.UpdateConfig)
			proxyRoute.POST(`/config/test`, middleware.AdminAccess(), handler.ProxyHd.TestProxy)
		}

		// 消息通知相关
		notifyRoute := manageRoute.Group(`/notify`)
		{
			notifyRoute.GET(`/config`, handler.NotifyHd.GetConfig)
			notifyRoute.POST(`/config`, middleware.AdminAccess(), handler.NotifyHd.UpdateConfig)
			notifyRoute.POST(`/config/test`, middleware.AdminAccess(), handler.NotifyHd.TestNotify)
		}

		// 文件管理
		fileRoute := manageRoute.Group(`/file`)
		{
			fileRoute.POST(`/upload`, handler.FileHd.Upload)
			fileRoute.POST(`/rename`, handler.FileHd.RenameFile)
			fileRoute.POST(`/delete`, handler.FileHd.DelFile)
			fileRoute.GET(`/list`, handler.FileHd.PhotoWall)
		}
	}

	provideRoute := api.Group(`/provide`, middleware.ProvideKeyGuard())
	{
		provideRoute.GET(`/vod`, handler.ProvideHd.HandleProvide)
		provideRoute.GET(`/tvbox`, handler.ProvideHd.HandleProvideConfig)
		provideRoute.GET(`/app`, handler.ProvideHd.HandleProvideApp)
	}

	return r
}
