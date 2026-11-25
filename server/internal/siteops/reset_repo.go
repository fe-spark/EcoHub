package siteops

import (
	"fmt"

	"server/internal/collect"
	"server/internal/config"
	"server/internal/film"
	"server/internal/film/cache"
	"server/internal/film/catalog"
	"server/internal/film/snapshot"
	"server/internal/film/writer"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"
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
		if err := repobase.TruncateTable(db.Mdb, table); err != nil {
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

func clearAllRuntimeAndRedisCaches() {
	repobase.ResetConfigCacheGens()
	catalog.InvalidatePublicSourceCache()
	collect.ResetCollectStatsCoalescer()
	writer.ResetSearchTagMemoryCaches()
	snapshot.ClearSnapshotState()
	snapshot.ClearAllSnapshotDynamicCaches()
	snapshot.ClearActiveFilmReadModel()
	snapshot.BumpSearchCacheVersion()
	film.RefreshMasterDataCaches()
	repobase.ClearIndexPageCache()
	sourcecatalog.InvalidatePrimarySourceCache()
	taxonomy.ResetCategoryNameCache()
	taxonomy.ResetCategoryStableKeyCache()
	taxonomy.TouchCategoryVersion()
	taxonomy.TouchRuleVersion()

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
	film.RefreshInventoryStatsAsync()
}
