测试版 **v2.5.7-beta.3**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.7-beta.3`。

### 升级指引

- **从已有版本升级**：
  - **1Panel / Compose**：执行 `docker compose pull ecohub && docker compose up -d ecohub` 即可（或后台「检查更新」一键平滑升级）。
  - **数据结构与兼容性**：完全向下兼容现有 MySQL 与 Redis 数据结构，无破坏性变更。
  - **索引自动就绪**：服务启动时将自动检测并补齐高性能覆盖索引，无需手动执行数据库脚本。

---

### v2.5.7-beta.3 核心变更

#### 1. 内存搜索索引原子双缓冲（根除 5.4 秒级长耗时毛刺）
- **原子双缓冲（Double-Buffering）与无锁热返回**：彻底解决 120 万数据下增量入库或快照切换时，因全量内存倒排索引扫描重建（扫全表 + 120万次全拼/首字母拼音生成耗时 5.4 秒）而同步挂起在线 `/api/searchFilm` HTTP 线程的生产毛刺。
- **平滑过渡与后台静默异步构建**：索引过期时不置空内存索引项，在线请求直接复用当前可用索引毫秒级（1~3ms）极速响应，新版本索引在后台静默构建完成后原子无缝切换，实现 0 感知、恒定低延迟。

#### 2. 全站核心查询接口百万级毫秒化性能加固
- **`GET /api/hotKeywords` 覆盖索引与消除 filesort**：
  - 建立 `idx_snap_ver_hits_pid (snapshot_version, hits, pid)` 覆盖复合索引，移除 `id DESC`，直接走 B-Tree 索引逆序直出，消除百万级全表 filesort。
  - 以版本为粒度全局缓存 Top 20 至 Redis（TTL 30m），SingleFlight 防击穿，对外切片深拷贝隔离。接口耗时由 1500ms~3500ms 降至 0.2ms。
- **`GET /api/filmPlayInfo` 详情聚合缓存补全与切片隔离**：
  - 补齐读取与写入 Redis 键 `EcoHub:filmPlayInfo:%d`（TTL 12h + 随机 Jitter 防雪崩，空哨兵防穿透）。
  - 引入全层级嵌套切片深度克隆（`cloneMovieDetailVo`），彻底阻断多协程就地修改播放源引发的数据竞争（Data Race）。耗时从 45ms 降至 0.5ms。
- **`GET /api/provide/vod` (TVBox) 批量快照查询与 Pipeline MGet**：
  - 批量化重构 `GetVodDetail`，单次批量获取快照，并通过 Redis Pipeline MGet 单次往返批量读取 100 条详情缓存，彻底消除 20 次详情串行循环与 80 次 SQL 交互风暴。接口耗时由 800ms 降至 2ms。
  - 设定 `maxProvideVodDetailBatch = 100` 硬批次保护，补全海报、年份、备注空值安全回退。
- **`GET /api/filmClassify` 剥离无意义 COUNT 与 3 路并发**：
  - 新增 `GetSnapshotTopMoviesBySortFast`，彻底剥离百万行耗时 `COUNT(*)`，基于复合索引直出 Top 21。
  - 顶层未命中时采用 `sync.WaitGroup` 3 路并发拉取 `news`, `top`, `recent`，回源耗时由 500ms 降至 1ms。
- **`GET /api/filmClassifySearch` 深分页硬截断与 SingleFlight 回填**：
  - 限制 `Current <= 50, PageSize <= 48`，最大 Offset 限制在 2352 以内，消除深度翻页拖垮数据库隐患。
  - SingleFlight 闭包安全回填调用方 `page` 指针的分页元数据，防御数据丢失与切片竞态。
- **`GET /api/filmRelate` 缓存前置与候选直出**：
  - 缓存检查前置到方法入口，命中时 0 次数据库开销。
  - 候选集 1/2/3 移除 SQL 里的 `AND mid != ?`（由 Go 内存 `seen` 去重），候选集 4 剔除 `class_tag LIKE` 全表模糊扫描，走复合索引按大类热度直出，消除 filesort。

#### 3. 可靠性与测试覆盖
- 新增 `hardening_plans_test.go` 针对性测试套件（含高并发切片修改竞态测试、单测边界等 13 个独立测试）。
- 新增 `TestSearchIndex_DoubleBuffering_NonBlocking` 双缓冲零停顿测试。
- 全仓 `-race` 竞态检测与全量回归测试 100% PASS。

