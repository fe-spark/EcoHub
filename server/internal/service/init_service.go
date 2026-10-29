package service

import (
	"fmt"
	"strings"
	"time"

	"server/internal/access"
	"server/internal/config"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider"
	"server/internal/utils"

	"github.com/robfig/cron/v3"
)

type InitService struct{}

var InitSvc = new(InitService)

func (s *InitService) DefaultDataInit() error {
	isNewDatabase := !repository.ExistUserTable()

	if err := s.TableInit(); err != nil {
		return err
	}

	if isNewDatabase {
		if err := repository.AlignUserAutoIncrement(); err != nil {
			return err
		}
	}

	repository.InitMappingEngine()
	repository.InitMainCategories()
	if err := repository.InitBuiltinAccounts(); err != nil {
		return err
	}
	if err := utils.CreateBaseDir(); err != nil {
		syslog.Errorf("[Init] 素材目录创建失败 %s: %v", config.FilmPictureUploadDir, err)
		panic(fmt.Sprintf("素材目录创建失败 %s: %v", config.FilmPictureUploadDir, err))
	}
	if err := config.EnsureContainerUploadVolume(); err != nil {
		syslog.Warnf("[Init] %v", err)
	}

	if err := s.SiteWebConfigInit(); err != nil {
		return err
	}
	if err := repository.EnsureDefaultPosterSource(); err != nil {
		return fmt.Errorf("默认海报源初始化失败: %w", err)
	}
	if err := s.AnalyticsConfigInit(); err != nil {
		return err
	}
	filmsnapshot.RestoreActiveSnapshotVersion()
	if err := s.SpiderInit(); err != nil {
		return err
	}
	NotifySvc.EnsureBotPoller()
	if err := s.ensureFilmListSnapshot(); err != nil {
		return err
	}
	if err := s.loadActiveFilmReadModel(); err != nil {
		return err
	}
	return nil
}

func (s *InitService) ensureFilmListSnapshot() error {
	if err := filmsnapshot.EnsureActiveFilmListSnapshot(); err != nil {
		return fmt.Errorf("前台影片列表快照引导失败: %w", err)
	}
	return nil
}

func (s *InitService) loadActiveFilmReadModel() error {
	// 片库非空时，列表快照引导已经按同一版本加载完成。再调一次会清掉索引并重扫。
	if rm := filmsnapshot.GetActiveFilmReadModel(); rm != nil && strings.TrimSpace(rm.Version) != "" {
		return nil
	}
	if err := filmsnapshot.LoadActiveFilmReadModel(""); err != nil {
		return fmt.Errorf("影片内存读模型加载失败: %w", err)
	}
	return nil
}

func (s *InitService) TableInit() error {
	if err := repository.AutoMigrateAll(); err != nil {
		return fmt.Errorf("数据库结构迁移失败: %w", err)
	}
	if err := repository.RunSchemaMigrations(); err != nil {
		return fmt.Errorf("数据库版本迁移失败: %w", err)
	}
	if err := repository.AlignUserAutoIncrement(); err != nil {
		return fmt.Errorf("用户表自增对齐失败: %w", err)
	}
	return nil
}

// SiteWebConfigInit 初始化网站基本信息。读失败直接返回，不写默认配置。
func (s *InitService) SiteWebConfigInit() error {
	exists, err := repository.ExistSiteConfig()
	if err != nil {
		return fmt.Errorf("读取网站配置失败: %w", err)
	}
	if !exists {
		if err := repository.SaveSiteBasic(defaultBasicConfig()); err != nil {
			return fmt.Errorf("写入默认网站配置失败: %w", err)
		}
		return nil
	}
	if _, err := repository.GetSiteBasic(); err != nil {
		return fmt.Errorf("回填网站配置失败: %w", err)
	}
	return nil
}

// defaultBasicConfig 默认网站基本信息
func defaultBasicConfig() model.BasicConfig {
	return model.BasicConfig{
		SiteName: "EcoHub",
		// 网站访问地址：Logo 跳转与播放链接；初始为空需在后台配置
		SiteURL: "",
		// 初始为空：前端未配置时用本地 /logo.png；后台配置后按配置原样加载
		Logo:     "",
		Keyword:  "在线视频, 免费观影",
		Describe: "自动采集, 多播放源集成,在线观影网站",
		State:    true,
		Hint:     "网站升级中, 暂时无法访问 !!!",
		Danmaku:  model.DefaultDanmakuConfig(),
		Tip:      model.DefaultTipConfig(),
		Notice:   model.DefaultNoticeConfig(),
	}
}

// AnalyticsConfigInit 初始化数据分析配置并同步内存原子状态
func (s *InitService) AnalyticsConfigInit() error {
	cfg, err := repository.GetAnalyticsConfig()
	if err != nil {
		return fmt.Errorf("读取数据分析配置失败: %w", err)
	}
	access.SetEnabled(cfg.Enabled)
	return nil
}

func (s *InitService) SpiderInit() error {
	if err := s.FilmSourceInit(); err != nil {
		return err
	}
	go SpiderSvc.SyncMissingSourceCategories()
	return s.CollectCrontabInit()
}

func (s *InitService) FilmSourceInit() error {
	exists, err := repository.ExistCollectSourceList()
	if err != nil {
		return fmt.Errorf("读取采集站失败: %w", err)
	}
	if exists {
		return nil
	}
	if err := repository.BatchAddCollectSource(defaultFilmSources()); err != nil {
		return fmt.Errorf("写入默认采集站失败: %w", err)
	}
	return nil
}

func defaultFilmSources() []model.FilmSource {
	// 使用 URI 哈希作为 ID，设置递增初始创建时间以保证默认采集站顺序稳定
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	list := []model.FilmSource{
		{Id: "3706668934", Name: "金鹰1(JY)", Uri: `https://jinyingzy.com/api.php/provide/vod`, Sort: 0, State: true, Interval: 50, Cd: 24, IsPosterSource: true},
		{Id: "1016684692", Name: "速博(SUBO)", Uri: `https://subocaiji.com/api.php/provide/vod`, Sort: 1, State: true, Interval: 50, Cd: 24},
		{Id: "1208629981", Name: "HD(SN)", Uri: `https://suoniapi.com/api.php/provide/vod/from/snm3u8/`, Sort: 2, State: true, Interval: 50, Cd: 24},
		{Id: "2608173413", Name: "金鹰2(JY)", Uri: `https://jyzyapi.com/api.php/provide/vod`, Sort: 3, State: true, Interval: 50, Cd: 24},
		{Id: "2761253814", Name: "红牛(HN)", Uri: `https://www.hongniuzy2.com/api.php/provide/vod/at/json`, Sort: 4, State: true, Interval: 50, Cd: 24},
		{Id: "2898990914", Name: "非凡(FF)", Uri: `http://cj.ffzyapi.com/api.php/provide/vod/`, Sort: 5, State: true, Interval: 50, Cd: 24},
		{Id: "3370810636", Name: "HD(LY)", Uri: `https://360zy.com/api.php/provide/vod/at/json`, Sort: 6, State: true, Interval: 50, Cd: 24},
		{Id: "3423682340", Name: "HD(IK)", Uri: `https://ikunzyapi.com/api.php/provide/vod/at/json`, Sort: 7, State: true, Interval: 50, Cd: 24},
		{Id: "4194624554", Name: "U酷(UKU)", Uri: `https://api.ukuapi88.com/api.php/provide/vod`, Sort: 8, State: true, Interval: 50, Cd: 24},
		{Id: "4247318859", Name: "光速(GS)", Uri: `https://api.guangsuapi.com/api.php/provide/vod/json`, Sort: 9, State: true, Interval: 50, Cd: 24},
		{Id: "531717376", Name: "樱花(YH)", Uri: `https://m3u8.apiyhzy.com/api.php/provide/vod/`, Sort: 10, State: true, Interval: 50, Cd: 24},
		{Id: "829678680", Name: "HD(BF)", Uri: `https://bfzyapi.com/api.php/provide/vod/`, Sort: 11, State: true, Interval: 50, Cd: 24},
	}
	for i := range list {
		list[i].Sort = i
		list[i].CreatedAt = baseTime.Add(time.Duration(i+1) * time.Second)
	}
	return list
}

func (s *InitService) CollectCrontabInit() error {
	tasks, err := s.ensureDefaultTasks()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if err := s.registerTask(task); err != nil {
			return err
		}
	}
	spider.CronCollect.Start()
	return nil
}

// ensureDefaultTasks 已有任务保持原样。某个任务类型还没有记录时，补一条默认任务。
func (s *InitService) ensureDefaultTasks() ([]model.FilmCollectTask, error) {
	existing, err := repository.GetAllFilmTask()
	if err != nil {
		return nil, fmt.Errorf("读取定时任务失败: %w", err)
	}
	existingModels := make(map[int]bool, len(existing))
	for _, t := range existing {
		existingModels[t.Model] = true
	}

	for _, dt := range defaultFilmTasks() {
		if existingModels[dt.Model] {
			if dt.Model == model.TaskModelFailureClean {
				if t, err := repository.GetFilmTaskById(model.TaskIDFailureClean); err == nil && t.Remark == "清理超过 7 天的成功和最终失败记录" {
					t.Remark = dt.Remark
					_ = repository.UpdateFilmTask(t)
				}
			}
			continue
		}
		if err := repository.SaveFilmTask(dt); err != nil {
			return nil, fmt.Errorf("补齐默认任务失败 id=%s: %w", dt.Id, err)
		}
		existing = append(existing, dt)
		existingModels[dt.Model] = true
	}
	return existing, nil
}

func (s *InitService) registerTask(task model.FilmCollectTask) error {
	if !task.State {
		if err := repository.UpdateFilmTask(task); err != nil {
			return fmt.Errorf("保存定时任务 %s 失败: %w", task.Id, err)
		}
		return nil
	}

	var cid cron.EntryID
	var err error
	switch task.Model {
	case model.TaskModelAutoCollect:
		cid, err = spider.AddAutoUpdateCron(task.Id, task.Spec)
	case model.TaskModelRetryCollect:
		cid, err = spider.AddFilmRecoverCron(task.Id, task.Spec)
	case model.TaskModelOrphanClean:
		cid, err = spider.AddOrphanCleanCron(task.Id, task.Spec)
	case model.TaskModelLogClean:
		cid, err = spider.AddLogCleanCron(task.Id, task.Spec)
	case model.TaskModelFailureClean:
		cid, err = spider.AddFailureRecordCleanCron(task.Id, task.Spec)
	default:
		return nil
	}
	if err != nil {
		return fmt.Errorf("注册定时任务 %s 失败: %w", task.Id, err)
	}
	spider.RegisterTaskCid(task.Id, cid)
	return nil
}

func defaultFilmTasks() []model.FilmCollectTask {
	task := model.FilmCollectTask{
		Id: "sys_cron_auto_collect", Time: config.DefaultUpdateTime, Spec: config.DefaultUpdateSpec,
		Model: model.TaskModelAutoCollect, State: true, Remark: "采集已启用站点的更新",
	}

	recoverTask := model.FilmCollectTask{
		Id: "sys_cron_recover_collect", Time: 0, Spec: config.EveryDaySpec,
		Model: model.TaskModelRetryCollect, State: true, Remark: "重试待处理的失败记录",
	}

	orphanTask := model.FilmCollectTask{
		Id: "sys_cron_orphan_clean", Time: 0, Spec: config.OrphanCleanSpec,
		Model: model.TaskModelOrphanClean, State: true, Remark: "清理片库冗余和孤儿数据",
	}

	logCleanTask := model.FilmCollectTask{
		Id: "sys_cron_log_clean", Time: 0, Spec: "0 0 3 * * *",
		Model: model.TaskModelLogClean, State: true, Remark: "清理过期运行日志",
	}

	failureCleanTask := model.FilmCollectTask{
		Id: model.TaskIDFailureClean, Time: 0, Spec: "0 30 3 * * *",
		Model: model.TaskModelFailureClean, State: true, Remark: "清理已成功记录及超过 7 天的失败记录",
	}

	return []model.FilmCollectTask{task, recoverTask, orphanTask, logCleanTask, failureCleanTask}
}
