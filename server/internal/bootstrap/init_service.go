package bootstrap

import (
	"fmt"
	"server/internal/film/catalog"
	"server/internal/mediaproxy"
	"strings"
	"time"

	"server/internal/admin/upgrade"
	"server/internal/analytics"
	"server/internal/collect"
	"server/internal/config"
	filmrepo "server/internal/film"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/notify"
	"server/internal/repobase"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"
	"server/internal/user"
	"server/internal/utils"

	"github.com/robfig/cron/v3"
	"server/internal/collect/ledger"
)

type InitService struct{}

var InitSvc = new(InitService)

func (s *InitService) DefaultDataInit() error {
	isNewDatabase := !user.ExistUserTable()
	seedDefaultSources := isNewDatabase
	if !seedDefaultSources {
		hasAdmin, err := user.HasAdminUser()
		if err != nil {
			return fmt.Errorf("读取管理员失败: %w", err)
		}
		seedDefaultSources = !hasAdmin
	}

	if err := s.TableInit(); err != nil {
		return err
	}

	if isNewDatabase {
		if err := repobase.AlignUserAutoIncrement(); err != nil {
			return err
		}
	}
	if err := user.InitBuiltinAccounts(); err != nil {
		return err
	}
	if err := utils.CreateBaseDir(); err != nil {
		syslog.Errorf("[Init] 素材目录创建失败 %s: %v", config.FilmPictureUploadDir, err)
		panic(fmt.Sprintf("素材目录创建失败 %s: %v", config.FilmPictureUploadDir, err))
	}
	if err := config.EnsureContainerUploadVolume(); err != nil {
		syslog.Warnf("[Init] %v", err)
	}

	return s.seedRuntimeDefaults(seedDefaultSources, true)
}

// ReinitAfterReset 在数据重置清库后重新写入默认采集源、分类、站点配置、定时任务并刷新运行时状态。
func (s *InitService) ReinitAfterReset() error {
	if err := utils.CreateBaseDir(); err != nil {
		return fmt.Errorf("重建素材目录失败: %w", err)
	}
	mediaproxy.InvalidateMediaProxyMemoryCache()
	repobase.ClearProvideNetworkConfigCache()
	utils.ResetProxyTransports()
	upgrade.TriggerReleasePoll()
	return s.seedRuntimeDefaults(true, false)
}

func (s *InitService) seedRuntimeDefaults(seedSources, runBootSpiderInit bool) error {
	if seedSources {
		if err := collect.BatchAddCollectSource(defaultFilmSources()); err != nil {
			return fmt.Errorf("写入默认采集站失败: %w", err)
		}
	}
	if err := sourcecatalog.RefreshCollectSourceCatalog(); err != nil {
		return fmt.Errorf("刷新采集站目录失败: %w", err)
	}
	_ = sourcecatalog.CurrentPrimarySource()

	taxonomy.InitMappingEngine()
	catalog.InitMainCategories()

	if err := s.SiteWebConfigInit(); err != nil {
		return err
	}
	if err := collect.EnsureDefaultPosterSource(); err != nil {
		return fmt.Errorf("默认海报源初始化失败: %w", err)
	}
	if err := s.AnalyticsConfigInit(); err != nil {
		return err
	}
	filmsnapshot.RestoreActiveSnapshotVersion()
	if runBootSpiderInit {
		if err := s.SpiderInit(); err != nil {
			return err
		}
	} else {
		if err := s.CollectCrontabInit(); err != nil {
			return err
		}
	}
	notify.NotifySvc.EnsureBotPoller()
	if err := s.ensureFilmListSnapshot(); err != nil {
		return err
	}
	if err := s.loadActiveFilmReadModel(); err != nil {
		return err
	}
	filmrepo.RefreshInventoryStatsAsync()
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
	if err := repobase.RunSchemaMigrations(); err != nil {
		return fmt.Errorf("数据库版本迁移失败: %w", err)
	}
	if err := repobase.AutoMigrateAll(); err != nil {
		return fmt.Errorf("建表失败: %w", err)
	}
	if err := repobase.AlignUserAutoIncrement(); err != nil {
		return fmt.Errorf("用户表自增对齐失败: %w", err)
	}
	if err := catalog.EnsureFilmCategoryScoreIndex(); err != nil {
		return fmt.Errorf("排片评分索引失败: %w", err)
	}
	if err := catalog.EnsureFilmCidScoreIndex(); err != nil {
		return fmt.Errorf("排片分类评分索引失败: %w", err)
	}
	if err := catalog.EnsureFilmSourceDeletedIndex(); err != nil {
		return fmt.Errorf("采集站统计索引失败: %w", err)
	}
	if err := catalog.EnsureFilmCategoryDeletedIndex(); err != nil {
		return fmt.Errorf("分类统计索引失败: %w", err)
	}
	return nil
}

// SiteWebConfigInit 初始化网站基本信息。读失败直接返回，不写默认配置。
func (s *InitService) SiteWebConfigInit() error {
	exists, err := repobase.ExistSiteConfig()
	if err != nil {
		return fmt.Errorf("读取网站配置失败: %w", err)
	}
	if !exists {
		if err := repobase.SaveSiteBasic(defaultBasicConfig()); err != nil {
			return fmt.Errorf("写入默认网站配置失败: %w", err)
		}
		return nil
	}
	if _, err := repobase.GetSiteBasic(); err != nil {
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
	cfg, err := analytics.GetConfig()
	if err != nil {
		return fmt.Errorf("读取数据分析配置失败: %w", err)
	}
	analytics.SetEnabled(cfg.Enabled)
	return nil
}

func defaultFilmSources() []model.FilmSource {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	list := []model.FilmSource{
		{Id: "3706668934", Name: "金鹰1(JY)", Uri: `https://jinyingzy.com/api.php/provide/vod`, State: true, Cd: 24, IsPosterSource: true},
		{Id: "1016684692", Name: "速博(SUBO)", Uri: `https://subocaiji.com/api.php/provide/vod`, State: true, Cd: 24},
		{Id: "1208629981", Name: "HD(SN)", Uri: `https://suoniapi.com/api.php/provide/vod/from/snm3u8/`, State: true, Cd: 24},
		{Id: "2608173413", Name: "金鹰2(JY)", Uri: `https://jyzyapi.com/api.php/provide/vod`, State: true, Cd: 24},
		{Id: "2761253814", Name: "红牛(HN)", Uri: `https://www.hongniuzy2.com/api.php/provide/vod/at/json`, State: true, Cd: 24},
		{Id: "2898990914", Name: "非凡(FF)", Uri: `http://cj.ffzyapi.com/api.php/provide/vod/`, State: true, Cd: 24},
		{Id: "3370810636", Name: "HD(LY)", Uri: `https://360zy.com/api.php/provide/vod/at/json`, State: true, Cd: 24},
		{Id: "3423682340", Name: "HD(IK)", Uri: `https://ikunzyapi.com/api.php/provide/vod/at/json`, State: true, Cd: 24},
		{Id: "4194624554", Name: "U酷(UKU)", Uri: `https://api.ukuapi88.com/api.php/provide/vod`, State: true, Cd: 24},
		{Id: "4247318859", Name: "光速(GS)", Uri: `https://api.guangsuapi.com/api.php/provide/vod/json`, State: true, Cd: 24},
		{Id: "531717376", Name: "樱花(YH)", Uri: `https://m3u8.apiyhzy.com/api.php/provide/vod/`, State: true, Cd: 24},
		{Id: "829678680", Name: "HD(BF)", Uri: `https://bfzyapi.com/api.php/provide/vod/`, State: true, Cd: 24},
	}
	for i := range list {
		list[i].Sort = i
		list[i].CreatedAt = baseTime.Add(time.Duration(i+1) * time.Second)
	}
	return list
}

func (s *InitService) SpiderInit() error {
	if err := ledger.MarkInterruptedCollectRecords(); err != nil {
		syslog.Warnf("[Init] 标记中断采集记录失败: %v", err)
	}
	utils.GoSafe("启动清理过期采集记录", func() {
		if err := ledger.CleanOldCollectRecords(30); err != nil {
			syslog.Warnf("[Init] 清理过期采集记录失败: %v", err)
		}
	})
	utils.GoSafe("启动补全缺失来源分类", func() {
		collect.SpiderSvc.SyncMissingSourceCategories()
	})
	return s.CollectCrontabInit()
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
	collect.CronCollect.Start()
	return nil
}

// ensureDefaultTasks 已有任务保持原样，缺的默认任务补上。
func (s *InitService) ensureDefaultTasks() ([]model.FilmCollectTask, error) {
	existing, err := collect.GetAllFilmTask()
	if err != nil {
		return nil, fmt.Errorf("读取定时任务失败: %w", err)
	}

	existingModels := make(map[int]bool, len(existing))
	for _, t := range existing {
		existingModels[t.Model] = true
	}

	for _, dt := range defaultFilmTasks() {
		if existingModels[dt.Model] {
			continue
		}
		if err := collect.SaveFilmTask(dt); err != nil {
			return nil, fmt.Errorf("补齐默认任务失败 id=%s: %w", dt.Id, err)
		}
		existing = append(existing, dt)
		existingModels[dt.Model] = true
	}
	return existing, nil
}

func (s *InitService) registerTask(task model.FilmCollectTask) error {
	if !task.State {
		if err := collect.UpdateFilmTask(task); err != nil {
			return fmt.Errorf("保存定时任务 %s 失败: %w", task.Id, err)
		}
		return nil
	}

	var cid cron.EntryID
	var err error
	switch task.Model {
	case model.TaskModelAutoCollect:
		cid, err = collect.AddAutoUpdateCron(task.Id, task.Spec)
	case model.TaskModelOrphanClean:
		cid, err = collect.AddOrphanCleanCron(task.Id, task.Spec)
	case model.TaskModelLogClean:
		cid, err = collect.AddLogCleanCron(task.Id, task.Spec)
	case model.TaskModelCollectClean:
		cid, err = collect.AddCollectRecordCleanCron(task.Id, task.Spec)
	default:
		return nil
	}
	if err != nil {
		return fmt.Errorf("注册定时任务 %s 失败: %w", task.Id, err)
	}
	collect.RegisterTaskCid(task.Id, cid)
	return nil
}

func defaultFilmTasks() []model.FilmCollectTask {
	task := model.FilmCollectTask{
		Id: "sys_cron_auto_collect", Time: config.DefaultUpdateTime, Spec: config.DefaultUpdateSpec,
		Model: model.TaskModelAutoCollect, State: true, Remark: "采集已启用站点的更新",
	}

	orphanTask := model.FilmCollectTask{
		Id: "sys_cron_orphan_clean", Time: 0, Spec: config.OrphanCleanSpec,
		Model: model.TaskModelOrphanClean, State: true, Remark: "清理片库冗余和孤儿数据",
	}

	logCleanTask := model.FilmCollectTask{
		Id: "sys_cron_log_clean", Time: 0, Spec: "0 0 3 * * *",
		Model: model.TaskModelLogClean, State: true, Remark: "清理过期运行日志",
	}

	collectCleanTask := model.FilmCollectTask{
		Id: model.TaskIDCollectClean, Time: 0, Spec: "0 30 3 * * *",
		Model: model.TaskModelCollectClean, State: true, Remark: "定期清理超过 30 天的采集运行记录与快照",
	}

	return []model.FilmCollectTask{task, orphanTask, logCleanTask, collectCleanTask}
}
