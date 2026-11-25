package film

import (
	"fmt"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/film/catalog"
	"server/internal/model"
	"server/internal/film/cache"
	"server/internal/film/snapshot"
	"server/internal/film/writer"
	"server/internal/repobase"
	"server/internal/utils"
)

var (
	resetFilmTables = []string{
		model.TableFilmSourcePlaylist,
		model.TableMovieSourceMapping,
		model.TableMovieMatchKey,
		model.TableFilmTMDBScrape,
		model.TableFilmIndex,
	}
	resetCollectTables = []string{
		model.TableCollectRecordFilm,
		model.TableCollectRecord,
		model.TableCollectSourceStats,
		model.TableSearchTag,
		model.TableBannerSlot,
		model.TableCategoryMapping,
		model.TableSourceCategory,
		model.TableCategory,
		model.TableMappingRule,
		model.TablePrimaryState,
		model.TableFilmSource,
		model.TableDanmakuRecord,
	}
	resetConfigTables = []string{
		model.TableCrontabRecord,
		model.TableSiteConfig,
		model.TableTMDBConfig,
		model.TableProxyConfig,
		model.TableNotifyConfig,
		model.TableAnalyticsConfig,
		model.TableAccessDailyStats,
		model.TableAccessDailyTop,
		model.TableFileInfo,
	}
)

func truncateTableGroup(tables []string) error {
	for _, table := range tables {
		if err := truncateTable(db.Mdb, table); err != nil {
			return fmt.Errorf("truncate %s failed: %w", table, err)
		}
	}
	return nil
}

// FilmZero 全量清空业务与配置数据表（保留账号表 user 与迁移版本表 schema_migrations），并同步清理图库与全部缓存。
func FilmZero() error {
	ReportResetProgress(20, "正在清空影视与映射数据")
	if err := truncateTableGroup(resetFilmTables); err != nil {
		return err
	}

	ReportResetProgress(40, "正在清空采集与分类数据")
	if err := truncateTableGroup(resetCollectTables); err != nil {
		return err
	}

	ReportResetProgress(60, "正在清空配置与任务数据")
	if err := truncateTableGroup(resetConfigTables); err != nil {
		return err
	}

	if err := utils.ClearGalleryDir(); err != nil {
		return fmt.Errorf("clear gallery dir failed: %w", err)
	}

	ReportResetProgress(75, "正在清理缓存")
	clearAllRuntimeAndRedisCaches()
	return nil
}

// OnResetCollectStats 站点重置时清理采集统计合并器，由装配层注入。
var OnResetCollectStats func()

func clearAllRuntimeAndRedisCaches() {
	repobase.ResetConfigCacheGens()
	catalog.InvalidatePublicSourceCache()
	if OnResetCollectStats != nil {
		OnResetCollectStats()
	}
	writer.ResetSearchTagMemoryCaches()
	snapshot.ClearSnapshotState()
	snapshot.ClearAllSnapshotDynamicCaches()
	snapshot.ClearActiveFilmReadModel()
	snapshot.BumpSearchCacheVersion()
	RefreshMasterDataCaches()
	repobase.ClearIndexPageCache()
	repobase.InvalidatePrimarySourceCache()
	repobase.ResetCategoryNameCache()
	repobase.ResetCategoryStableKeyCache()
	repobase.TouchCategoryVersion()
	repobase.TouchRuleVersion()

	if db.Rdb != nil {
		db.Rdb.Del(
			db.Cxt,
			config.SiteConfigBasic,
			config.TMDBConfigKey,
			config.ProxyConfigKey,
			config.NotifyConfigKey,
			config.AnalyticsConfigKey,
			config.CategoryTreeKey,
			config.ActiveCategoryTreeKey,
			config.SnapshotActiveVersionKey,
			config.TVBoxConfigCacheKey,
			config.OrphanCleanCursorKey,
			config.MasterSwitchProtectKey,
			config.DailyUpdatesCatCacheKey,
		)
		cache.ClearPatterns(config.TVBoxNetworkConfigCacheKey + ":*")
	}
	RefreshInventoryStatsAsync()
}
