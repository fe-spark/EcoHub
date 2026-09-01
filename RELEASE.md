测试版（Pre-release），Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.0-beta.13`。

### 核心变更

#### 1. 热播影视详情降级查表与自定义海报解析修复
- **修正模型查表**：`resolveFilmMetas` 第二层降级查询切换为持久化实体 `model.MovieDetailInfo` 并反序列化 `Content` JSON，彻底解决原业务结构体查表报错被静默忽略的问题，确保精准反查自定义海报与元数据。

#### 2. 增量快照缓存精准淘汰 Pipeline 分批优化
- **分批下发管道**：在 `InvalidateIncrementalSnapshotCaches` 中为 Redis Pipeline 引入 1000 条 Chunk 分批执行机制，有效平抑超大批次变更时的 Redis 缓冲区开销与瞬时延迟。

#### 3. 首页多分类大区并发构建异常容错加固
- **Goroutine 异常兜底**：在 `IndexPage` 及 `overlayDynamicCategoryMovies` 的并发子协程中统一注入 `defer recover()` 异常捕获与错误日志上报，杜绝子协程偶发异常导致主请求中断。
