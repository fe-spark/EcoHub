package service

import (
	"fmt"
	"log"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider"
	"server/internal/utils"

	"github.com/robfig/cron/v3"
)

type InitService struct{}

var InitSvc = new(InitService)

func (s *InitService) DefaultDataInit() {
	clearStartupCaches()

	if !repository.ExistUserTable() {
		// 只有在用户表不存在时（视为首次运行或库重建），才执行完整的表迁移与初始数据灌入
		s.TableInit()
	} else {
		// 常规重启：仅执行 AutoMigrate 确保结构对齐，并加载内存缓存
		db.Mdb.AutoMigrate(
			&model.User{}, &model.SearchInfo{}, &model.FileInfo{}, &model.FailureRecord{},
			&model.MovieDetailInfo{}, &model.Category{}, &model.MoviePlaylist{},
			&model.MovieMatchKey{},
			&model.VirtualPictureQueue{}, &model.FilmSource{}, &model.SearchTagItem{},
			&model.CrontabRecord{}, &model.SiteConfigRecord{}, &model.MovieSourceMapping{},
			&model.Banner{}, &model.CronSourceRel{}, &model.MappingRule{}, &model.CategoryMapping{},
		)
	}

	// 映射引擎初始化 & 数据库标准数据对齐 (标准大类与排序标签)
	repository.InitMappingEngine()
	repository.InitMainCategories()
	repository.InitBuiltinAccounts()

	s.BasicConfigInit()
	s.BannersInit()
	s.SpiderInit()
}

func clearStartupCaches() {
	ctx := db.Cxt
	db.Rdb.Del(ctx,
		config.ActiveCategoryTreeKey,
		config.TVBoxConfigCacheKey,
		config.VirtualPictureKey,
	)
	repository.ClearIndexPageCache()

	patterns := []string{
		config.SearchTags + ":*",
		config.TVBoxList + ":*",
	}
	for _, pattern := range patterns {
		iter := db.Rdb.Scan(ctx, 0, pattern, config.MaxScanCount).Iterator()
		for iter.Next(ctx) {
			db.Rdb.Del(ctx, iter.Val())
		}
		if err := iter.Err(); err != nil {
			log.Printf("Redis startup cache cleanup failed for %s: %v", pattern, err)
		}
	}

	log.Println("[Init] Redis 可重建缓存已清理")
}

func (s *InitService) TableInit() {
	err := db.Mdb.AutoMigrate(
		&model.User{},
		&model.SearchInfo{},
		&model.FileInfo{},
		&model.FailureRecord{},
		&model.MovieDetailInfo{},
		&model.Category{},
		&model.MoviePlaylist{},
		&model.MovieMatchKey{},
		&model.VirtualPictureQueue{},
		&model.FilmSource{},
		&model.SearchTagItem{},
		&model.CrontabRecord{},
		&model.SiteConfigRecord{},
		&model.MovieSourceMapping{},
		&model.Banner{},
		&model.CronSourceRel{},
		&model.MappingRule{},
		&model.CategoryMapping{},
	)
	if err != nil {
		log.Println("Database AutoMigrate Failed:", err)
		return
	}

	// 初始化映射清洗引擎与标准大类 (由 DefaultDataInit 统一调用)

	// 专门处理表的默认或初始状态定义
	db.Mdb.Exec(fmt.Sprintf("alter table %s auto_Increment = %d", model.TableUser, config.UserIdInitialVal))
}

func (s *InitService) BasicConfigInit() {
	if repository.ExistSiteConfig() {
		return
	}
	bc := defaultBasicConfig()
	_ = repository.SaveSiteBasic(bc) // SaveSiteBasic 内部应处理 FirstOrCreate 逻辑
}

func defaultBasicConfig() model.BasicConfig {
	return model.BasicConfig{
		SiteName: "EcoHub",
		Logo:     "https://raw.githubusercontent.com/fe-spark/EcoHub/main/logo.png",
		Keyword:  "在线视频, 免费观影",
		Describe: "自动采集, 多播放源集成,在线观影网站",
		State:    true,
		Hint:     "网站升级中, 暂时无法访问 !!!",
	}
}

func (s *InitService) BannersInit() {
	if repository.ExistBannersConfig() {
		return
	}
	bl := model.Banners{
		model.Banner{Id: utils.GenerateSalt(), Name: "樱花庄的宠物女孩", Year: 2020, CName: "日韩动漫", Poster: "https://s2.loli.net/2024/02/21/Wt1QDhabdEI7HcL.jpg", Picture: "https://s2.loli.net/2024/02/21/Wt1QDhabdEI7HcL.jpg", PictureSlide: "https://img.bfzypic.com/upload/vod/20230424-43/06e79232a4650aea00f7476356a49847.jpg", Remark: "已完结"},
		model.Banner{Id: utils.GenerateSalt(), Name: "从零开始的异世界生活", Year: 2020, CName: "日韩动漫", Poster: "https://s2.loli.net/2024/02/21/UkpdhIRO12fsy6C.jpg", Picture: "https://s2.loli.net/2024/02/21/UkpdhIRO12fsy6C.jpg", PictureSlide: "https://img.bfzypic.com/upload/vod/20230424-43/06e79232a4650aea00f7476356a49847.jpg", Remark: "已完结"},
		model.Banner{Id: utils.GenerateSalt(), Name: "五等分的花嫁", Year: 2020, CName: "日韩动漫", Poster: "https://s2.loli.net/2024/02/21/wXJr59Zuv4tcKNp.jpg", Picture: "https://s2.loli.net/2024/02/21/wXJr59Zuv4tcKNp.jpg", PictureSlide: "https://img.bfzypic.com/upload/vod/20230424-43/06e79232a4650aea00f7476356a49847.jpg", Remark: "已完结"},
		model.Banner{Id: utils.GenerateSalt(), Name: "我的青春恋爱物语果然有问题", Year: 2020, CName: "日韩动漫", Poster: "https://s2.loli.net/2024/02/21/oMAGzSliK2YbhRu.jpg", Picture: "https://s2.loli.net/2024/02/21/oMAGzSliK2YbhRu.jpg", PictureSlide: "https://img.bfzypic.com/upload/vod/20230424-43/06e79232a4650aea00f7476356a49847.jpg", Remark: "已完结"},
	}
	_ = repository.SaveBanners(bl)
}

func (s *InitService) SpiderInit() {
	s.FilmSourceInit()
	s.CollectCrontabInit()
}

func (s *InitService) FilmSourceInit() {
	if repository.ExistCollectSourceList() {
		return
	}
	// 直接初始化采集源 - 使用 URI 哈希作为 ID 确保服务重启后顺序一致且支持主从切换
	l := []model.FilmSource{
		{Name: "HD(SN)", Uri: `https://suoniapi.com/api.php/provide/vod/from/snm3u8/`, ResultModel: model.JsonResult, Grade: model.SlaveCollect, SyncPictures: false, CollectType: model.CollectVideo, State: false, Interval: 500},
		{Name: "HD(OK)", Uri: `https://okzyapi.com/api.php/provide/vod/`, ResultModel: model.JsonResult, Grade: model.SlaveCollect, SyncPictures: false, CollectType: model.CollectVideo, State: false, Interval: 500},
		{Name: "光速(GS)", Uri: `https://api.guangsuapi.com/api.php/provide/vod/json`, ResultModel: model.JsonResult, Grade: model.SlaveCollect, SyncPictures: false, CollectType: model.CollectVideo, State: false, Interval: 500},
		{Name: "HD(HM)", Uri: `https://json.heimuer.xyz/api.php/provide/vod/`, ResultModel: model.JsonResult, Grade: model.SlaveCollect, SyncPictures: false, CollectType: model.CollectVideo, State: false, Interval: 500},
		{Name: "魔都(MD)", Uri: `https://www.mdzyapi.com/api.php/provide/vod/`, ResultModel: model.JsonResult, Grade: model.SlaveCollect, SyncPictures: false, CollectType: model.CollectVideo, State: false, Interval: 500},
		{Name: "HD(DB)", Uri: `https://caiji.dbzy.tv/api.php/provide/vod/from/dbm3u8/at/json/`, ResultModel: model.JsonResult, Grade: model.SlaveCollect, SyncPictures: false, CollectType: model.CollectVideo, State: false, Interval: 500},
		{Name: "红牛(HN)", Uri: `https://www.hongniuzy2.com/api.php/provide/vod/at/json`, ResultModel: model.JsonResult, Grade: model.SlaveCollect, SyncPictures: false, CollectType: model.CollectVideo, State: false, Interval: 500},
		{Name: "HD(FF)", Uri: `http://cj.ffzyapi.com/api.php/provide/vod/`, ResultModel: model.JsonResult, Grade: model.MasterCollect, SyncPictures: false, CollectType: model.CollectVideo, State: true, Interval: 500},
		{Name: "HD(LY)", Uri: `https://360zy.com/api.php/provide/vod/at/json`, ResultModel: model.JsonResult, Grade: model.SlaveCollect, SyncPictures: false, CollectType: model.CollectVideo, State: false, Interval: 500},
		{Name: "HD(IK)", Uri: `https://ikunzyapi.com/api.php/provide/vod/at/json`, ResultModel: model.JsonResult, Grade: model.SlaveCollect, SyncPictures: false, CollectType: model.CollectVideo, State: false, Interval: 500},
		{Name: "HD(LZ)", Uri: `https://cj.lziapi.com/api.php/provide/vod/`, ResultModel: model.JsonResult, Grade: model.SlaveCollect, SyncPictures: false, CollectType: model.CollectVideo, State: false, Interval: 500},
		{Name: "樱花(YH)", Uri: `https://m3u8.apiyhzy.com/api.php/provide/vod/`, ResultModel: model.JsonResult, Grade: model.SlaveCollect, SyncPictures: false, CollectType: model.CollectVideo, State: false, Interval: 500},
		{Name: "HD(BF)", Uri: `https://bfzyapi.com/api.php/provide/vod/`, ResultModel: model.JsonResult, Grade: model.SlaveCollect, SyncPictures: false, CollectType: model.CollectVideo, State: false, Interval: 500},
		{Name: "卧龙(WL)", Uri: `https://collect.wolongzy.cc/api.php/provide/vod/`, ResultModel: model.JsonResult, Grade: model.SlaveCollect, SyncPictures: false, CollectType: model.CollectVideo, State: false, Interval: 500},
	}
	if err := repository.BatchAddCollectSource(l); err != nil {
		log.Println("BatchAddCollectSource Error: ", err)
	}
}

func (s *InitService) CollectCrontabInit() {
	if repository.ExistTask() {
		if tasks := repository.GetAllFilmTask(); len(tasks) > 0 {
			for _, task := range tasks {
				s.registerTask(task)
			}
		}
	} else {
		// 初始任务预设
		s.createDefaultTasks()
	}

	spider.CronCollect.Start()
}

func (s *InitService) registerTask(task model.FilmCollectTask) {
	if !task.State {
		repository.UpdateFilmTask(task)
		return
	}

	var cid cron.EntryID
	var err error
	switch task.Model {
	case 0:
		cid, err = spider.AddAutoUpdateCron(task.Id, task.Spec)
	case 1:
		cid, err = spider.AddFilmUpdateCron(task.Id, task.Spec)
	case 2:
		cid, err = spider.AddFilmRecoverCron(task.Id, task.Spec)
	case 3:
		cid, err = spider.AddOrphanCleanCron(task.Id, task.Spec)
	}
	if err == nil {
		task.Cid = cid
		spider.RegisterTaskCid(task.Id, task.Cid)
		repository.UpdateFilmTask(task)
	}
}

func (s *InitService) createDefaultTasks() {
	task := model.FilmCollectTask{
		Id: utils.GenerateSalt(), Time: config.DefaultUpdateTime, Spec: config.DefaultUpdateSpec,
		Model: 0, State: true, Remark: fmt.Sprintf("每20分钟自动采集已启用站点最近 %d 小时内更新的影片", config.DefaultUpdateTime),
	}
	s.registerTask(task)

	recoverTask := model.FilmCollectTask{
		Id: utils.GenerateSalt(), Time: 0, Spec: config.EveryWeekSpec,
		Model: 2, State: true, Remark: "每周日凌晨4点清理采集失败的记录",
	}
	s.registerTask(recoverTask)

	orphanTask := model.FilmCollectTask{
		Id: utils.GenerateSalt(), Time: 0, Spec: config.EveryDaySpec,
		Model: 3, State: true, Remark: "每天凌晨0点清理无主影片的孤儿播放列表",
	}
	s.registerTask(orphanTask)
}
