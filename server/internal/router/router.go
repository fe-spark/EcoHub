package router

import (
	"mime"
	"server/internal/config"
	"server/internal/handler"
	"server/internal/infra/syslog"
	"server/internal/middleware"

	"github.com/gin-gonic/gin"
)

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

	r.Static(config.FilmPictureAccess, config.FilmPictureUploadDir)

	api := r.Group("/api")

	// Deprecated: 后续主版本计划移除。
	// 废弃原因：/api/health 仅返回静态健康状态，无法校验私有化密钥安全与站点核心依赖。
	// 替代方案：探活与站点公开基础信息统一使用 /api/config/basic；EcoHub 原生客户端软件源鉴权测通统一使用 /api/provide/app。
	api.GET(`/health`, handler.Health)
	api.HEAD(`/health`, handler.Health)
	api.GET(`/config/basic`, handler.ManageHd.SiteBasicConfig)
	api.POST(`/login`, handler.UserHd.Login)
	api.POST(`/logout`, middleware.AuthToken(), handler.UserHd.Logout)

	// 前台业务接口，开启私有化模式时需携带有效登录态
	frontApi := api.Group("/", middleware.PrivateAccessGuard())
	{
		frontApi.GET(`/index`, handler.IndexHd.Index)
		// Deprecated: 后续主版本计划移除。
		// 废弃原因：早期版本（beta.3）遗留的每日更新接口，不支持大类分类联动且缺乏规范的分页参数与短缓存。
		// 替代方案：请使用 /api/dailyUpdates (对应 IndexHandler.DailyUpdatesV2)，支持 pid 分类筛选、标准分页及排重。
		frontApi.GET(`/index/dailyUpdates`, handler.IndexHd.DailyUpdates)
		frontApi.GET(`/dailyUpdates`, handler.IndexHd.DailyUpdatesV2)
		frontApi.GET(`/navCategory`, handler.IndexHd.CategoriesInfo)
		frontApi.GET(`/filmPlayInfo`, handler.IndexHd.FilmPlayInfo)
		frontApi.GET(`/filmRelate`, handler.IndexHd.FilmRelate)
		frontApi.GET(`/liveFilmPlayInfo`, handler.IndexHd.LiveFilmPlayInfo)
		frontApi.GET(`/liveFilmRelate`, handler.IndexHd.LiveFilmRelate)
		frontApi.GET(`/searchFilm`, handler.IndexHd.SearchFilm)
		frontApi.GET(`/hotKeywords`, handler.IndexHd.HotKeywords)
		frontApi.GET(`/filmClassify`, handler.IndexHd.FilmClassify)
		frontApi.GET(`/filmClassifySearch`, handler.IndexHd.FilmTagSearch)
		frontApi.POST(`/stat/view`, handler.AccessHd.TrackView)
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
			sysConfig.POST(`/basic/update`, handler.ManageHd.UpdateSiteBasic)

			sysConfig.GET(`/access`, handler.ManageHd.SiteAccessConfig)
			sysConfig.POST(`/access/update`, handler.ManageHd.UpdateSiteAccess)

			sysConfig.GET(`/tip`, handler.ManageHd.SiteTipConfig)
			sysConfig.POST(`/tip/update`, handler.ManageHd.UpdateSiteTip)

			sysConfig.GET(`/notice`, handler.ManageHd.SiteNoticeConfig)
			sysConfig.POST(`/notice/update`, handler.ManageHd.UpdateSiteNotice)

			// 通知配置（仅超级管理员）
			sysConfig.GET(`/notify`, middleware.AdminAccess(), handler.NotifyHd.GetNotifyConfig)
			sysConfig.POST(`/notify/update`, middleware.AdminAccess(), handler.NotifyHd.UpdateNotifyConfig)
			sysConfig.POST(`/notify/test`, middleware.AdminAccess(), handler.NotifyHd.TestNotify)

			// 配置备份：导出/导入（不含影视库存与账号，仅超级管理员）
			sysConfig.GET(`/backup/export`, middleware.AdminAccess(), handler.ManageHd.ExportConfigBackup)
			sysConfig.POST(`/backup/import`, middleware.AdminAccess(), handler.ManageHd.ImportConfigBackup)
		}
		systemLog := manageRoute.Group(`/system/logs`, middleware.AdminAccess())
		{
			systemLog.GET(`/delta`, handler.SystemLogHd.Delta)
		}

		accessRoute := manageRoute.Group(`/access`)
		{
			accessRoute.GET(`/status`, handler.AccessHd.Status)
			accessRoute.GET(`/overview`, middleware.AdminAccess(), handler.AccessHd.Overview)
			accessRoute.GET(`/tops`, middleware.AdminAccess(), handler.AccessHd.Tops)
			accessRoute.GET(`/logs`, middleware.AdminAccess(), handler.AccessHd.Logs)
			accessRoute.GET(`/stats`, middleware.AdminAccess(), handler.AccessHd.DataStats)
			accessRoute.POST(`/clean`, middleware.AdminAccess(), handler.AccessHd.CleanData)
		}

		// 轮播相关
		banner := manageRoute.Group(`banner`)
		{
			banner.GET(`/list`, handler.ManageHd.BannerList)
			banner.GET(`/find`, handler.ManageHd.BannerFind)
			banner.POST(`/add`, handler.ManageHd.BannerAdd)
			banner.POST(`/update`, handler.ManageHd.BannerUpdate)
			banner.POST(`/del`, handler.ManageHd.BannerDel)
		}

		// 映射规则管理
		mapping := manageRoute.Group(`/mapping`)
		{
			mapping.GET(`/group/list`, handler.ManageHd.MappingRuleGroups)
			mapping.GET(`/rule/list`, handler.ManageHd.MappingRuleList)
			mapping.POST(`/rule/check`, handler.ManageHd.MappingRuleCheck)
			mapping.POST(`/rule/add`, handler.ManageHd.MappingRuleAdd)
			mapping.POST(`/rule/update`, handler.ManageHd.MappingRuleUpdate)
			mapping.POST(`/rule/del`, handler.ManageHd.MappingRuleDel)
			mapping.POST(`/rule/reload`, handler.ManageHd.MappingRuleReload)
		}

		// 用户相关
		userRoute := manageRoute.Group(`/user`)
		{
			userRoute.GET(`/info`, handler.UserHd.UserInfo)
			userRoute.GET(`/list`, handler.UserHd.UserListPage)
			userRoute.POST(`/add`, handler.UserHd.UserAdd)
			userRoute.POST(`/update`, handler.UserHd.UserUpdate)
			userRoute.POST(`/del`, handler.UserHd.UserDelete)
		}

		// 采集相关
		collect := manageRoute.Group(`/collect`)
		{
			collect.GET(`/list`, handler.CollectHd.FilmSourceList)
			collect.GET(`/find`, handler.CollectHd.FindFilmSource)
			collect.POST(`/test`, handler.CollectHd.FilmSourceTest)
			collect.POST(`/add`, handler.CollectHd.FilmSourceAdd)
			collect.POST(`/update`, handler.CollectHd.FilmSourceUpdate)
			collect.POST(`/change`, handler.CollectHd.FilmSourceChange)
			collect.POST(`/change/batch`, handler.CollectHd.FilmSourceBatchChange)
			collect.POST(`/del`, handler.CollectHd.FilmSourceDel)
			collect.POST(`/del/batch`, handler.CollectHd.FilmSourceDelBatch)
			collect.POST(`/check/all`, handler.CollectHd.FilmSourceCheckAll)
			collect.GET(`/options`, handler.CollectHd.GetNormalFilmSource)

			collect.GET(`/record/list`, handler.CollectHd.FailureRecordList)
			collect.POST(`/record/retry`, handler.CollectHd.CollectRecover)
			collect.POST(`/record/retry/all`, handler.CollectHd.CollectRecoverAll)
			collect.POST(`/record/clear/result`, handler.CollectHd.ClearRetriedRecords)
			collect.POST(`/record/clear/all`, handler.CollectHd.ClearAllRecord)
		}

		// 定时任务相关
		collectCron := manageRoute.Group(`/cron`)
		{
			collectCron.GET(`/list`, handler.CronHd.FilmCronTaskList)
			collectCron.GET(`/find`, handler.CronHd.GetFilmCronTask)
			collectCron.POST(`/update`, handler.CronHd.FilmCronUpdate)
			collectCron.POST(`/change`, handler.CronHd.ChangeTaskState)
			collectCron.POST(`/run`, handler.CronHd.RunFilmCronTask)
		}

		// spider 数据采集
		spiderRoute := manageRoute.Group(`/spider`)
		{
			spiderRoute.POST(`/start`, handler.SpiderHd.StarSpider)
			spiderRoute.POST(`/stop`, handler.SpiderHd.StopTask)
			spiderRoute.POST(`/clear`, middleware.AdminAccess(), handler.SpiderHd.ClearAllFilm)
			spiderRoute.GET(`/clear/progress`, middleware.AdminAccess(), handler.SpiderHd.ResetProgress)
			spiderRoute.GET(`/clear/stats`, handler.SpiderHd.ResetImpactStats)
			spiderRoute.POST(`/update/single`, handler.SpiderHd.SingleUpdateSpider)
			spiderRoute.POST(`/stopAll`, handler.SpiderHd.StopAllTasks)
		}

		// filmManage 影视管理
		filmRoute := manageRoute.Group(`/film`)
		{
			filmRoute.POST(`/add`, handler.FilmHd.FilmAdd)
			filmRoute.GET(`/search/list`, handler.FilmHd.FilmSearchPage)
			filmRoute.POST(`/search/del`, handler.FilmHd.FilmDelete)

			filmRoute.GET(`/class/tree`, handler.FilmHd.FilmClassTree)
			filmRoute.GET(`/class/find`, handler.FilmHd.FindFilmClass)
			filmRoute.POST(`/class/collect`, handler.FilmHd.CollectFilmClass)
			filmRoute.POST(`/class/tree/save`, handler.FilmHd.SaveFilmClassTree)
			filmRoute.POST(`/class/update`, handler.FilmHd.UpdateFilmClass)
		}

		// TMDB 刮削相关
		tmdbRoute := manageRoute.Group(`/tmdb`)
		{
			tmdbRoute.GET(`/config`, handler.TMDBHd.GetConfig)
			tmdbRoute.POST(`/config/update`, middleware.AdminAccess(), handler.TMDBHd.UpdateConfig)
			tmdbRoute.POST(`/config/test`, middleware.AdminAccess(), handler.TMDBHd.TestConfig)
			tmdbRoute.GET(`/search`, handler.TMDBHd.Search)
			tmdbRoute.POST(`/apply`, handler.TMDBHd.Apply)
			tmdbRoute.GET(`/prefill`, handler.TMDBHd.Prefill)
		}

		// 文件管理
		fileRoute := manageRoute.Group(`/file`)
		{
			fileRoute.POST(`/upload`, handler.FileHd.SingleUpload)
			fileRoute.POST(`/upload/multiple`, handler.FileHd.MultipleUpload)
			fileRoute.POST(`/rename`, handler.FileHd.RenameFile)
			fileRoute.POST(`/del`, handler.FileHd.DelFile)
			fileRoute.GET(`/list`, handler.FileHd.PhotoWall)
		}
	}

	provideRoute := api.Group(`/provide`, middleware.ProvideKeyGuard())
	{
		provideRoute.GET(`/vod`, handler.ProvideHd.HandleProvide)
		// Deprecated: 后续主版本计划移除该别名路由。
		// 废弃原因：早期 TVBox 聚合配置路径，命名过于泛化。
		// 替代方案：第三方 TVBox / 影视仓配置推荐统一使用 /api/provide/tvbox；EcoHub 原生客户端请使用 /api/provide/app。
		provideRoute.GET(`/config`, handler.ProvideHd.HandleProvideConfig)
		provideRoute.GET(`/tvbox`, handler.ProvideHd.HandleProvideConfig)
		provideRoute.GET(`/app`, handler.ProvideHd.HandleProvideApp)
	}

	return r
}
