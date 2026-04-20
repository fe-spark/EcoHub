package router

import (
	"server/internal/config"
	"server/internal/handler"
	"server/internal/middleware"

	"github.com/gin-gonic/gin"
)

func SetupRouter() *gin.Engine {
	r := gin.Default()
	r.Use(middleware.Cors())

	r.Static(config.FilmPictureAccess, config.FilmPictureUploadDir)

	api := r.Group("/api")

	api.GET(`/index`, handler.IndexHd.Index)
	api.GET(`/proxy/video`, handler.ProxyHd.ProxyVideo)
	api.GET(`/config/basic`, handler.ManageHd.SiteBasicConfig)
	api.GET(`/navCategory`, handler.IndexHd.CategoriesInfo)
	api.GET(`/filmPlayInfo`, handler.IndexHd.FilmPlayInfo)
	api.GET(`/searchFilm`, handler.IndexHd.SearchFilm)
	api.GET(`/filmClassify`, handler.IndexHd.FilmClassify)
	api.GET(`/filmClassifySearch`, handler.IndexHd.FilmTagSearch)
	api.POST(`/login`, handler.UserHd.Login)
	api.POST(`/logout`, middleware.AuthToken(), handler.UserHd.Logout)

	manageRoute := api.Group(`/manage`)
	manageRoute.Use(middleware.AuthToken(), middleware.WriteAccess())
	{
		manageRoute.GET(`/index`, handler.ManageHd.ManageIndex)

		// 系统相关
		sysConfig := manageRoute.Group(`/config`)
		{
			sysConfig.GET(`/basic`, handler.ManageHd.SiteBasicConfig)
			sysConfig.POST(`/basic/update`, handler.ManageHd.UpdateSiteBasic)
			sysConfig.POST(`/basic/reset`, handler.ManageHd.ResetSiteBasic)
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
			collect.POST(`/del`, handler.CollectHd.FilmSourceDel)
			collect.GET(`/options`, handler.CollectHd.GetNormalFilmSource)
			collect.GET(`/collecting/state`, handler.CollectHd.CollectingState)
			collect.POST(`/stop`, handler.CollectHd.StopCollect)

			collect.GET(`/record/list`, handler.CollectHd.FailureRecordList)
			collect.POST(`/record/retry`, handler.CollectHd.CollectRecover)
			collect.POST(`/record/retry/all`, handler.CollectHd.CollectRecoverAll)
			collect.POST(`/record/clear/done`, handler.CollectHd.ClearDoneRecord)
			collect.POST(`/record/clear/all`, handler.CollectHd.ClearAllRecord)
		}

		// 定时任务相关
		collectCron := manageRoute.Group(`/cron`)
		{
			collectCron.GET(`/list`, handler.CronHd.FilmCronTaskList)
			collectCron.GET(`/find`, handler.CronHd.GetFilmCronTask)
			collectCron.POST(`/update`, handler.CronHd.FilmCronUpdate)
			collectCron.POST(`/change`, handler.CronHd.ChangeTaskState)
		}

		// spider 数据采集
		spiderRoute := manageRoute.Group(`/spider`)
		{
			spiderRoute.POST(`/start`, handler.SpiderHd.StarSpider)
			spiderRoute.POST(`/clear`, handler.SpiderHd.ClearAllFilm)
			spiderRoute.POST(`/update/single`, handler.SpiderHd.SingleUpdateSpider)
			spiderRoute.POST(`/class/cover`, handler.SpiderHd.CoverFilmClass)
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
			filmRoute.POST(`/class/update`, handler.FilmHd.UpdateFilmClass)
			filmRoute.POST(`/class/del`, handler.FilmHd.DelFilmClass)
		}

		// 文件管理
		fileRoute := manageRoute.Group(`/file`)
		{
			fileRoute.POST(`/upload`, handler.FileHd.SingleUpload)
			fileRoute.POST(`/upload/multiple`, handler.FileHd.MultipleUpload)
			fileRoute.POST(`/del`, handler.FileHd.DelFile)
			fileRoute.GET(`/list`, handler.FileHd.PhotoWall)
		}
	}

	provideRoute := api.Group(`/provide`)
	{
		provideRoute.GET(`/vod`, handler.ProvideHd.HandleProvide)
		provideRoute.GET(`/config`, handler.ProvideHd.HandleProvideConfig)
	}

	return r
}
