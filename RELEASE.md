测试版（Pre-release），Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.0-beta.12`。

### 核心变更

#### 1. 首页数据（`/api/index`）百万级数据极速加载与 MySQL 索引优化
- **完美匹配 `idx_snap_pid_hits` 组合索引**：分类热播列表（`GetSnapshotHotMovieListByCategoryReadModel`）与动态推荐池（`GetSnapshotHotPoolByCategoryReadModel`）移除破坏索引排序的范围过滤条件，直接按 `WHERE snapshot_version = ? AND pid = ? ORDER BY hits DESC, id DESC LIMIT 50` 命中 B-Tree 索引精准扫描，彻底消除多达数十次的百万数据全表 Filesort，单次查询从 500ms 降至 < 0.5ms。
- **全分类大区多协程并发构建**：`IndexPage` 内部遍历分类与 `overlayDynamicCategoryMovies` 动态池抽样全面重构为 `sync.WaitGroup` 多 Goroutine 并发加载，分类查询从串行耗时累加转为并行加载，冷启动接口响应时间从 9400ms 降至 20ms 以内（缓存命中时保持 < 3ms）。
