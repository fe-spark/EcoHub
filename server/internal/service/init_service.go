package service

import (
	"context"
	"fmt"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/migration"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider"
	"server/internal/utils"

	"github.com/robfig/cron/v3"
)

type InitService struct{}

var InitSvc = new(InitService)

func (s *InitService) DefaultDataInit() {
	isNewDatabase := !repository.ExistUserTable()

	// 统一执行单一事实来源 AllModels 的幂等迁移
	s.TableInit()

	if isNewDatabase {
		db.Mdb.Exec(fmt.Sprintf("alter table %s auto_Increment = %d", model.TableUser, config.UserIdInitialVal))
	}

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

	// 网站基本信息初始化（首页轮播已移入内容管理）
	s.SiteWebConfigInit()
	if err := repository.EnsureDefaultPosterSourceTx(db.Mdb); err != nil {
		syslog.Errorf("[Init] EnsureDefaultPosterSourceTx 失败: %v", err)
	}
	// 定时任务启动前，从 Redis 备忘恢复活跃快照版本到内存。
	filmsnapshot.RestoreActiveSnapshotVersion()
	s.SpiderInit()
	s.ensureFilmListSnapshot()
	s.loadActiveFilmReadModel()
}

func (s *InitService) ensureFilmListSnapshot() {
	if err := filmsnapshot.EnsureActiveFilmListSnapshot(); err != nil {
		syslog.Errorf("[Init] 前台影片列表快照引导失败: %v", err)
	}
}

func (s *InitService) loadActiveFilmReadModel() {
	if err := filmsnapshot.LoadActiveFilmReadModel(""); err != nil {
		syslog.Errorf("[Init] 影片内存读模型加载失败: %v", err)
	}
}

func (s *InitService) TableInit() {
	err := db.Mdb.AutoMigrate(model.AllModels...)
	if err != nil {
		syslog.Errorf("Database AutoMigrate Failed: %v", err)
		return
	}
	if err := migration.RunAutoMigrations(db.Mdb); err != nil {
		syslog.Errorf("Database RunAutoMigrations Failed: %v", err)
	}

	db.Mdb.Exec(fmt.Sprintf("alter table %s auto_Increment = %d", model.TableUser, config.UserIdInitialVal))
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
		Notice:   model.DefaultNoticeConfig(),
	}
}

func (s *InitService) SpiderInit() {
	s.FilmSourceInit()
	go SpiderSvc.SyncMissingSourceCategories()
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
	// 使用 URI 哈希作为 ID，设置递增初始创建时间以保证默认采集站顺序稳定
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	list := []model.FilmSource{
		{Id: "3706668934", Name: "金鹰1(JY)", Uri: `https://jinyingzy.com/api.php/provide/vod`, Sort: 0, State: true, Interval: 200, Cd: 24, IsPosterSource: true},
		{Id: "1016684692", Name: "速博(SUBO)", Uri: `https://subocaiji.com/api.php/provide/vod`, Sort: 1, State: true, Interval: 200, Cd: 24},
		{Id: "1208629981", Name: "HD(SN)", Uri: `https://suoniapi.com/api.php/provide/vod/from/snm3u8/`, Sort: 2, State: true, Interval: 200, Cd: 24},
		{Id: "2608173413", Name: "金鹰2(JY)", Uri: `https://jyzyapi.com/api.php/provide/vod`, Sort: 3, State: true, Interval: 200, Cd: 24},
		{Id: "2761253814", Name: "红牛(HN)", Uri: `https://www.hongniuzy2.com/api.php/provide/vod/at/json`, Sort: 4, State: true, Interval: 200, Cd: 24},
		{Id: "2898990914", Name: "非凡(FF)", Uri: `http://cj.ffzyapi.com/api.php/provide/vod/`, Sort: 5, State: true, Interval: 200, Cd: 24},
		{Id: "3370810636", Name: "HD(LY)", Uri: `https://360zy.com/api.php/provide/vod/at/json`, Sort: 6, State: true, Interval: 200, Cd: 24},
		{Id: "3423682340", Name: "HD(IK)", Uri: `https://ikunzyapi.com/api.php/provide/vod/at/json`, Sort: 7, State: true, Interval: 200, Cd: 24},
		{Id: "4194624554", Name: "U酷(UKU)", Uri: `https://api.ukuapi88.com/api.php/provide/vod`, Sort: 8, State: true, Interval: 200, Cd: 24},
		{Id: "4247318859", Name: "光速(GS)", Uri: `https://api.guangsuapi.com/api.php/provide/vod/json`, Sort: 9, State: true, Interval: 200, Cd: 24},
		{Id: "531717376", Name: "樱花(YH)", Uri: `https://m3u8.apiyhzy.com/api.php/provide/vod/`, Sort: 10, State: true, Interval: 200, Cd: 24},
		{Id: "829678680", Name: "HD(BF)", Uri: `https://bfzyapi.com/api.php/provide/vod/`, Sort: 11, State: true, Interval: 200, Cd: 24},
	}
	for i := range list {
		list[i].Sort = i
		list[i].CreatedAt = baseTime.Add(time.Duration(i+1) * time.Second)
	}
	return list
}

func (s *InitService) CollectCrontabInit() {

	// 已有任务保持原样，缺的任务类型补默认任务。
	tasks := s.ensureDefaultTasks()
	for _, task := range tasks {
		s.registerTask(task)
	}

	spider.CronCollect.Start()
	// 注册首页轮播自动智能排片执行回调（供计划任务系统统一调度）
	spider.RegisterBannerAutoExecutor(func() error {
		cfg := repository.GetBannerConfig()
		if cfg.Mode != repository.BannerModeAuto {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, err := BannerAutoSvc.GenerateAutoBanners(ctx, "cron_schedule")
		return err
	})
}

// ensureDefaultTasks 已有任务保持原样。某个任务类型还没有记录时，补一条默认任务。
func (s *InitService) ensureDefaultTasks() []model.FilmCollectTask {
	existing := repository.GetAllFilmTask()
	existingModels := make(map[int]bool, len(existing))
	for _, t := range existing {
		existingModels[t.Model] = true
	}

	for _, dt := range defaultFilmTasks() {
		if existingModels[dt.Model] {
			continue
		}
		if err := repository.SaveFilmTask(dt); err != nil {
			syslog.Errorf("[Cron] 补齐默认任务失败 id=%s: %v", dt.Id, err)
			continue
		}
		existing = append(existing, dt)
		existingModels[dt.Model] = true
	}
	return existing
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
	case 4:
		cid, err = spider.AddLogCleanCron(task.Id, task.Spec)
	case 5:
		cid, err = spider.AddBannerAutoCron(task.Id, task.Spec)
	default:
		return
	}
	if err == nil {
		spider.RegisterTaskCid(task.Id, cid)
	} else {
		syslog.Errorf("Task [%s, model=%d] Add Cron Error: %v", task.Id, task.Model, err)
	}
}

func (s *InitService) createDefaultTasks() {
	for _, task := range defaultFilmTasks() {
		s.registerTask(task)
	}
}

func defaultFilmTasks() []model.FilmCollectTask {
	task := model.FilmCollectTask{
		Id: "sys_cron_auto_collect", Time: config.DefaultUpdateTime, Spec: config.DefaultUpdateSpec,
		Model: 0, State: false, Remark: "自动采集已启用站点更新的影片",
	}

	recoverTask := model.FilmCollectTask{
		Id: "sys_cron_recover_collect", Time: 0, Spec: config.EveryDaySpec,
		Model: 2, State: false, Remark: "定时重试采集失败的记录",
	}

	orphanTask := model.FilmCollectTask{
		Id: "sys_cron_orphan_clean", Time: 0, Spec: config.OrphanCleanSpec,
		Model: 3, State: false, Remark: "片库冗余数据与孤儿清理",
	}

	logCleanTask := model.FilmCollectTask{
		Id: "sys_cron_log_clean", Time: 0, Spec: "0 0 3 * * *",
		Model: 4, State: true, Remark: "自动清理过期运行日志",
	}

	bannerAutoTask := model.FilmCollectTask{
		Id: model.TaskIDBannerAuto, Time: 0, Spec: repository.DefaultBannerRefreshCron,
		Model: model.TaskModelBannerAuto, State: repository.GetBannerConfig().Mode == repository.BannerModeAuto, Remark: "首页轮播自动智能排片",
	}

	return []model.FilmCollectTask{task, recoverTask, orphanTask, logCleanTask, bannerAutoTask}
}
