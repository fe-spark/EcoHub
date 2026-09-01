正式版 **v2.5.0**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.0` 与 `ghcr.io/fe-spark/ecohub:latest`。

### 升级指引

- **从已有版本升级**：执行 `docker compose pull && docker compose up -d` 即可（或后台「检查更新」一键平滑升级）。
- **兼容性说明**：本版本完全向下兼容现有 MySQL 与 Redis 数据结构，无破坏性变更。

---

### v2.5.0 核心变更

#### 1. 百万级片库内存架构极致优化（String Arena 扁平化与 2-gram 倒排索引重构）
- **String Arena 扁平化内存池**：重构内存搜索模型，将百万级记录的 500 万独立堆字符串对象汇聚至单一连续字节池 `StringPool []byte`，结构体改用偏移量与长度索引，常驻内存降低 75%（降至 ~200MB），GC 标记 CPU 开销由 25% 骤降至 < 2%。
- **Map 桶容量收敛与 Base-Offset 并行构建**：倒排索引预分配按实际 2-gram 词频上限（65536）收敛，多协程分块构建无锁合并，索引构建速度提升至 200ms 内。
- **纯内存倒排打分切片**：移除片名模糊检索下对地区/语言标签的后置全量快照反查开销，检索直接走内存倒排索引打分与切片，大幅降低大词搜索的 I/O 抖动与数据库压力。

#### 2. 首页数据与分类大区极速加载与 MySQL 索引优化
- **组合索引精准命中**：分类热播列表（`GetSnapshotHotMovieListByCategoryReadModel`）与动态推荐池（`GetSnapshotHotPoolByCategoryReadModel`）移除破坏索引排序的范围过滤条件，完美命中 `idx_snap_pid_hits` 组合索引，彻底消除百万数据全表 Filesort，单次查询从 500ms 降至 < 0.5ms。
- **全分类大区多协程并发构建**：`IndexPage` 内部遍历分类与 `overlayDynamicCategoryMovies` 动态池抽样全面重构为多 Goroutine 并发加载，分类查询从串行耗时累加转为并行加载，冷启动接口响应时间从 9400ms 降至 20ms 以内（缓存命中时保持 < 3ms）。
- **Goroutine 异常兜底保护**：在并发子协程中统一注入 `defer recover()` 异常捕获与错误日志上报，杜绝子协程偶发异常导致主请求中断。

#### 3. 全局海报源（Poster Source）联动与素材资产全链路升级
- **影视与轮播自定义海报独立存储**：自定义海报与海报源解耦独立存储，新增防冲刷保护，杜绝自动采集覆盖自定义封面；增量快照缓存支持精准淘汰。
- **轮播图海报联动与自动兜底**：轮播图跟随海报源时横版幻灯图兜底同步高清海报；管理后台轮播表单与选片组件全面重构模块化。
- **后台素材选择器域名自动补齐**：后台素材选择弹窗（`ImagePicker`）、影片编辑（`film/add`）、轮播管理（`banners`）、网站 Logo 与赞赏渠道等全面支持完整域名回显与自动修复。

#### 4. 采集系统稳定性与架构解耦
- **采集批次上下文隔离**：引入采集批次隔离机制，采集任务停止即时取消写入队列，保障生命周期自闭环。
- **快照大事务解耦与 Pipeline 分批下发**：解耦快照生成与大事务处理，并在 `InvalidateIncrementalSnapshotCaches` 中为 Redis Pipeline 引入 1000 条 Chunk 分批执行机制，平抑超大批次变更时的 Redis 缓冲区开销。

#### 5. 前台 SSR 渲染稳定性与类型加固
- **影片卡片年份防御**：修复 `FilmList` 组件中 `buildFilmMetaTags` 在部分接口返回 `number` 类型年份时调用 `.slice()` 引发未捕获 `TypeError` 导致 Next.js SSR 500 崩溃的问题。
- **首页焦点图辅助函数简化与类型加固**：重构并简化 `HomeHero` 焦点图中的画质识别、类型标签解析与剧情简介提取逻辑，全面防御空值与非字符串数据类型。

#### 6. TVBox / MacCMS / 多端适配优化
- **跨协议与相对路径智能补全**：修复 `normalizeMediaURL` 误将 `//img.xxx.com` 识别为绝对路径拼接 baseURL 的缺陷，自动根据当前服务协议补齐 `https:` 或 `http:`；对 `/` 开头的相对路径自动结合 Host 补全域名，保障 TVBox、影视仓及 Android/鸿蒙端播放器封面稳定展示。
