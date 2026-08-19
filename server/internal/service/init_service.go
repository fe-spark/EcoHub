package service

import (
	"fmt"
	"log"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	"server/internal/spider"
	"server/internal/utils"

	"github.com/robfig/cron/v3"
)

type InitService struct{}

var InitSvc = new(InitService)

func (s *InitService) DefaultDataInit() {
	clearStartupCaches()

	if !repository.ExistUserTable() {
		s.TableInit()
	} else {
		db.Mdb.AutoMigrate(
			&model.User{}, &model.FilmIndex{}, &model.FilmListSnapshot{}, &model.FilmFilterOptionSnapshot{}, &model.FilmFilterIndexSnapshot{}, &model.FileInfo{}, &model.FailureRecord{},
			&model.MovieDetailInfo{}, &model.Category{}, &model.MoviePlaylist{},
			&model.MovieMatchKey{},
			&model.VirtualPictureQueue{}, &model.FilmSource{}, &model.CollectSourceStats{}, &model.SearchTagItem{},
			&model.CrontabRecord{}, &model.SiteConfigRecord{}, &model.MovieSourceMapping{},
			&model.Banner{}, &model.CronSourceRel{}, &model.MappingRule{}, &model.CategoryMapping{}, &model.SourceCategory{},
			&model.NotifyConfigRecord{},
			&model.NotifyChangeBatch{}, &model.NotifyChangeMid{},
		)
	}
	ensureMappingRuleIndexes()

	repository.InitMappingEngine()
	repository.InitMainCategories()
	repository.InitBuiltinAccounts()
	if err := utils.CreateBaseDir(); err != nil {
		syslog.Errorf("[Init] 素材目录创建失败 %s: %v", config.FilmPictureUploadDir, err)
		panic(fmt.Sprintf("素材目录创建失败 %s: %v", config.FilmPictureUploadDir, err))
	}
	if err := config.EnsureContainerUploadVolume(); err != nil {
		syslog.Warnf("[Init] %v", err)
	}
	// 一次性清理历史采集同步图库（素材中心仅保留用户上传）
	repository.PurgeSyncedGallery()

	// 网站基本信息初始化（首页轮播已移入内容管理）
	s.SiteWebConfigInit()
	s.SpiderInit()
	s.ensureFilmListSnapshot()
	s.loadActiveFilmReadModel()
}

func (s *InitService) ensureFilmListSnapshot() {
	if err := filmrepo.EnsureActiveFilmListSnapshot(); err != nil {
		syslog.Errorf("[Init] 前台影片列表快照引导失败: %v", err)
	}
}

func (s *InitService) loadActiveFilmReadModel() {
	if err := filmrepo.LoadActiveFilmReadModel(""); err != nil {
		syslog.Errorf("[Init] 影片内存读模型加载失败: %v", err)
	}
}

func clearStartupCaches() {
	ctx := db.Cxt
	iter := db.Rdb.Scan(ctx, 0, config.RedisProjectKeyPattern, config.MaxScanCount).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		// 保留 Bot 轮询跨实例领导锁，避免多实例同时启动时互相清锁导致双 getUpdates
		if key == config.NotifyBotPollerLockKey {
			continue
		}
		if err := db.Rdb.Del(ctx, key).Err(); err != nil {
			syslog.Errorf("[Init] Redis 键删除失败 %s: %v", key, err)
		}
	}
	if err := iter.Err(); err != nil {
		syslog.Errorf("[Init] Redis 模式清理失败 %s: %v", config.RedisProjectKeyPattern, err)
	}

	log.Printf("[Init] Redis 前缀 %s 相关键已清空", config.RedisKeyPrefix)
}

func (s *InitService) TableInit() {
	err := db.Mdb.AutoMigrate(
		&model.User{},
		&model.FilmIndex{},
		&model.FilmListSnapshot{},
		&model.FilmFilterOptionSnapshot{},
		&model.FilmFilterIndexSnapshot{},
		&model.FileInfo{},
		&model.FailureRecord{},
		&model.MovieDetailInfo{},
		&model.Category{},
		&model.MoviePlaylist{},
		&model.MovieMatchKey{},
		&model.VirtualPictureQueue{},
		&model.FilmSource{},
		&model.CollectSourceStats{},
		&model.SearchTagItem{},
		&model.CrontabRecord{},
		&model.SiteConfigRecord{},
		&model.MovieSourceMapping{},
		&model.Banner{},
		&model.CronSourceRel{},
		&model.MappingRule{},
		&model.CategoryMapping{},
		&model.SourceCategory{},
		&model.NotifyConfigRecord{},
		&model.NotifyChangeBatch{},
		&model.NotifyChangeMid{},
	)
	if err != nil {
		syslog.Errorf("Database AutoMigrate Failed: %v", err)
		return
	}
	ensureMappingRuleIndexes()

	db.Mdb.Exec(fmt.Sprintf("alter table %s auto_Increment = %d", model.TableUser, config.UserIdInitialVal))
}

func ensureMappingRuleIndexes() {
	if err := repository.EnsureMappingRuleIndexes(); err != nil {
		syslog.Errorf("Ensure mapping rule indexes failed: %v", err)
	}
}

// SiteWebConfigInit 初始化网站基本信息（首页轮播已移入内容管理，不再由初始化维护）
func (s *InitService) SiteWebConfigInit() {
	// 首次：写入默认基本信息
	if !repository.ExistSiteConfig() {
		if err := repository.SaveSiteBasic(defaultBasicConfig()); err != nil {
			syslog.Errorf("SiteWebConfigInit SaveSiteBasic Error: %v", err)
		}
		return
	}
	// 已初始化：回填网站配置的 Redis 缓存
	_ = repository.GetSiteBasic()
}

// defaultBasicConfig 默认网站基本信息
func defaultBasicConfig() model.BasicConfig {
	return model.BasicConfig{
		SiteName: "EcoHub",
		// 网站访问地址：Logo 跳转与 Telegram 播放链接；初始为空需在后台配置
		SiteURL: "",
		// 初始为空：前端未配置时用本地 /logo.png；后台配置后按配置原样加载
		Logo:     "",
		Keyword:  "在线视频, 免费观影",
		Describe: "自动采集, 多播放源集成,在线观影网站",
		State:    true,
		Hint:     "网站升级中, 暂时无法访问 !!!",
		Tip:      model.DefaultTipConfig(),
	}
}

func (s *InitService) SpiderInit() {
	s.FilmSourceInit()
	go func() {
		if err := SpiderSvc.SyncMasterCategoryTree(); err != nil {
			log.Printf("[Init] 主站分类同步跳过: %v", err)
		}
	}()
	s.CollectCrontabInit()
}

func (s *InitService) FilmSourceInit() {
	if repository.ExistCollectSourceList() {
		return
	}
	if err := repository.BatchAddCollectSource(defaultFilmSources()); err != nil {
		syslog.Errorf("BatchAddCollectSource Error: %v", err)
	}
}

func defaultFilmSources() []model.FilmSource {
	// 使用 URI 哈希作为 ID，确保重置后顺序一致且支持主从切换。
	return []model.FilmSource{
		{Name: "魔都(MD)", Uri: `https://www.mdzyapi.com/api.php/provide/vod/`, Grade: model.MasterCollect, State: true, Interval: config.DefaultSpiderInterval},
		{Name: "HD(SN)", Uri: `https://suoniapi.com/api.php/provide/vod/from/snm3u8/`, Grade: model.SlaveCollect, State: true, Interval: config.DefaultSpiderInterval},
		{Name: "红牛(HN)", Uri: `https://www.hongniuzy2.com/api.php/provide/vod/at/json`, Grade: model.SlaveCollect, State: true, Interval: config.DefaultSpiderInterval},
		{Name: "HD(FF)", Uri: `http://cj.ffzyapi.com/api.php/provide/vod/`, Grade: model.SlaveCollect, State: true, Interval: config.DefaultSpiderInterval},
		{Name: "HD(LY)", Uri: `https://360zy.com/api.php/provide/vod/at/json`, Grade: model.SlaveCollect, State: true, Interval: config.DefaultSpiderInterval},
		{Name: "HD(IK)", Uri: `https://ikunzyapi.com/api.php/provide/vod/at/json`, Grade: model.SlaveCollect, State: true, Interval: config.DefaultSpiderInterval},
		{Name: "光速(GS)", Uri: `https://api.guangsuapi.com/api.php/provide/vod/json`, Grade: model.SlaveCollect, State: true, Interval: config.DefaultSpiderInterval},
		{Name: "樱花(YH)", Uri: `https://m3u8.apiyhzy.com/api.php/provide/vod/`, Grade: model.SlaveCollect, State: true, Interval: config.DefaultSpiderInterval},
		{Name: "HD(BF)", Uri: `https://bfzyapi.com/api.php/provide/vod/`, Grade: model.SlaveCollect, State: true, Interval: config.DefaultSpiderInterval},
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
		if err := repository.UpdateFilmTask(task); err != nil {
			syslog.Errorf("UpdateFilmTask Error: %v", err)
		}
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
		if err := repository.UpdateFilmTask(task); err != nil {
			syslog.Errorf("UpdateFilmTask Error: %v", err)
		}
	}
}

func (s *InitService) createDefaultTasks() {
	for _, task := range defaultFilmTasks() {
		s.registerTask(task)
	}
}

func defaultFilmTasks() []model.FilmCollectTask {
	task := model.FilmCollectTask{
		Id: utils.GenerateSalt(), Time: config.DefaultUpdateTime, Spec: config.DefaultUpdateSpec,
		Model: 0, State: false, Remark: "自动采集已启用站点更新的影片",
	}

	recoverTask := model.FilmCollectTask{
		Id: utils.GenerateSalt(), Time: 0, Spec: config.EveryWeekSpec,
		Model: 2, State: false, Remark: "清理采集失败记录",
	}

	orphanTask := model.FilmCollectTask{
		Id: utils.GenerateSalt(), Time: 0, Spec: config.EveryDaySpec,
		Model: 3, State: false, Remark: "清理无主影片的孤儿播放列表",
	}

	return []model.FilmCollectTask{task, recoverTask, orphanTask}
}
