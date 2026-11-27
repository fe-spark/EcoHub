package migration

import (
	"context"
	"fmt"
	"time"

	"server/internal/infra/syslog"
	"server/internal/model"

	"gorm.io/gorm"
)

// Migration 定义单个数据库版本迁移。
type Migration struct {
	Version string
	Name    string
	Run     func(db *gorm.DB) error
}

// migrations 按版本号递增执行。已发布版本不要改 Run，后续升级在末尾追加。
var migrations = []Migration{
	{
		Version: "20261009_baseline",
		Name:    "当前版本基线，无结构变更",
		Run:     migrateBaseline,
	},
	{
		Version: "20261106_mapping_rule_source",
		Name:    "分类规则绑定采集站",
		Run:     migrateMappingRuleSource,
	},
	{
		Version: "20261109_drop_banner_config",
		Name:    "删除一键排片配置表",
		Run:     migrateDropBannerConfig,
	},
	{
		Version: "20261109_banner_slot",
		Name:    "轮播按站点存储，删除旧轮播表",
		Run:     migrateBannerSlot,
	},
	{
		Version: "20261114_collect_record_reset",
		Name:    "采集记录改为账单，清空后按新表重建",
		Run:     migrateCollectRecordReset,
	},
	{
		Version: "20261118_film_index_dedup",
		Name:    "清理 film_index 历史重复与残缺冗余索引",
		Run:     migrateFilmIndexDedup,
	},
	{
		Version: "20261126_home_recommend",
		Name:    "新增首页推荐分组与条目表",
		Run: func(gdb *gorm.DB) error {
			return gdb.AutoMigrate(&model.HomeRecommendGroup{}, &model.HomeRecommendItem{})
		},
	},
	{
		Version: "20261126_home_recommend_builtin",
		Name:    "首页推荐分组增加内置板块标识字段",
		Run: func(gdb *gorm.DB) error {
			return gdb.AutoMigrate(&model.HomeRecommendGroup{})
		},
	},
	{
		Version: "20261126_film_index_optimize",
		Name:    "清理 film_index 废弃索引并重建分类排片复合索引列序",
		Run:     migrateFilmIndexOptimize,
	},
	{
		Version: "20261126_schema_cleanup",
		Name:    "重建 collect_record_films 聚簇主键表并清理 home_recommend_group 废弃列",
		Run:     migrateSchemaCleanup,
	},
	{
		Version: "20261127_home_recommend_inited",
		Name:    "采集站增加首页推荐初始化标记",
		Run: func(gdb *gorm.DB) error {
			return gdb.AutoMigrate(&model.FilmSource{})
		},
	},
}

func migrateBaseline(*gorm.DB) error {
	return nil
}

// RunAutoMigrations 顺序执行尚未记录的迁移，并写入 schema_migrations。
func RunAutoMigrations(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if db.Dialector != nil && db.Dialector.Name() == "mysql" {
		sqlDB, err := db.DB()
		if err != nil {
			return fmt.Errorf("acquire migration db pool failed: %w", err)
		}
		ctx := context.Background()
		if db.Statement != nil && db.Statement.Context != nil {
			ctx = db.Statement.Context
		}
		// 单独用一条物理连接持有 MySQL 会话级咨询锁，避免将 *sql.Conn 传入迁移流程触发 DropTable 嵌套 Connection 报错 (invalid db)。
		lockConn, err := sqlDB.Conn(ctx)
		if err != nil {
			return fmt.Errorf("acquire migration lock connection failed: %w", err)
		}
		defer lockConn.Close()

		var locked int
		if err := lockConn.QueryRowContext(ctx, "SELECT GET_LOCK('ecohub_schema_migration', 60)").Scan(&locked); err != nil {
			return fmt.Errorf("acquire migration lock failed: %w", err)
		}
		if locked != 1 {
			return fmt.Errorf("acquire migration lock timeout (locked=%d)", locked)
		}
		defer func() {
			_, _ = lockConn.ExecContext(ctx, "SELECT RELEASE_LOCK('ecohub_schema_migration')")
		}()
	}
	return runAutoMigrationsLocked(db)
}

func runAutoMigrationsLocked(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.SchemaMigration{}) {
		if err := db.AutoMigrate(&model.SchemaMigration{}); err != nil {
			return fmt.Errorf("init schema_migrations table failed: %w", err)
		}
	}

	var applied []string
	if err := db.Model(&model.SchemaMigration{}).Pluck("version", &applied).Error; err != nil {
		return fmt.Errorf("load applied migrations failed: %w", err)
	}

	appliedSet := make(map[string]bool, len(applied))
	for _, v := range applied {
		appliedSet[v] = true
	}

	newApplied := 0
	for _, m := range migrations {
		if appliedSet[m.Version] {
			continue
		}
		if m.Run == nil {
			return fmt.Errorf("migration %s has no runner", m.Version)
		}

		syslog.Infof("[Migration] 正在执行版本迁移 %s (%s)...", m.Version, m.Name)
		if err := m.Run(db); err != nil {
			syslog.Errorf("[Migration] %s 执行失败: %v", m.Version, err)
			return fmt.Errorf("migration %s failed: %w", m.Version, err)
		}

		record := model.SchemaMigration{
			Version:   m.Version,
			Name:      m.Name,
			AppliedAt: time.Now(),
		}
		if err := db.Create(&record).Error; err != nil {
			return fmt.Errorf("record migration %s failed: %w", m.Version, err)
		}
		syslog.Infof("[Migration] 成功完成版本迁移 %s", m.Version)
		newApplied++
	}

	if newApplied > 0 {
		syslog.Infof("[Migration] 本次成功执行 %d 项版本迁移，数据库当前已就绪 (累计 %d 项)", newApplied, len(appliedSet)+newApplied)
	} else {
		syslog.Infof("[Migration] 数据库版本化迁移已是最新 (已应用 %d 项历史迁移，无需执行)", len(appliedSet))
	}
	return nil
}
