测试版 **v2.5.7-beta.4**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.7-beta.4`。

### 升级指引

- **从已有版本升级**：
  - **1Panel / Compose**：执行 `docker compose pull ecohub && docker compose up -d ecohub` 即可（或后台「检查更新」一键平滑升级）。
  - **数据结构与兼容性**：完全向下兼容现有 MySQL 与 Redis 数据结构，无破坏性变更。
  - **索引自动就绪**：服务启动时将自动检测并补齐高性能覆盖索引（包括新增的 `idx_snap_ver_series` 关联系列覆盖索引），无需手动执行数据库脚本。

---

### v2.5.7-beta.4 核心变更

#### 1. 搜索与相关推荐“彻底去分档”与化简过度设计
- **彻底去除分档模式**：移除 `SearchIndexMode`（`auto` / `topk` / `full` / `db`）及相关环境变量，统一采用全量连续评分体系（按匹配度、热度、年份、时间严格决胜）；
- **全量内存倒排索引作为唯一权威信源**：内存索引覆盖全量快照，未命中时直接在内存阻断（耗时仅 39µs）并写入 1 分钟 Redis 短缓存防击穿，**彻底根除全表扫描**（杜绝穿透 MySQL 触发持续 2~5 秒的 `LIKE %keyword%` 慢查询）；
- **管理端多维复合筛选精准透传**：对带有剧情、地区、语言等非紧凑索引维度的搜索，直接透传快照表查询，保证多维筛选准确。

#### 2. 核心查询毫秒/微秒级延迟优化
- **相关推荐（候选 2）倒排词条秒级召回**：
  - 彻底废除 120 万数据的 `strings.Contains` 线性全扫描（单次耗时约 300ms），重构为内存倒排词条精准召回，时间复杂度由 $O(N)$ 降至 $O(1)$，**耗时降至 0.01ms (10 微秒)**；
  - 倒排召回候选阶段前置增加 `seen` 过滤，避免同系列候选抢占 20 条配额并消除冗余数据库点查。
- **相关推荐（候选 1）系列覆盖索引**：
  - 在启动初始化中自动建立复合索引 `idx_snap_ver_series (snapshot_version, series_key, update_stamp)`，使同系列影片关联查询完全走索引覆盖，消除 filesort。
- **演职员多重切分重构**：
  - 将演职员多层循环与重复切片分配重构为单次线性扫描 `strings.FieldsFunc`，零额外切片分配。
- **排序全面升级为泛型 `slices.SortFunc`**：
  - 结合严格全序比对，移除 `sort.SliceStable` 反射与缓存开销，显著提升排序吞吐。

#### 3. 极致精简且安全的内存布局（内存占用减半）
- **紧凑 48 字节结构体**：
  - `filmSearchMemoryItem` 严格按 8 字节对齐，单条目从 96~128 字节减半至 **48 字节**，单库 120 万影片条目仅占约 57MB；
  - 采用标准 `int32`/`float32` 原生精度，无损对齐且杜绝截断与非标压缩；
- **单偏移量连续字符串池 (`StringPool`)**：
  - 单条目仅保存 1 个起始 `PoolOffset`，片名及拼音派生等 5 项字符串在 `StringPool` 中连续紧凑存储，指针切片零拷贝解析；
- **切片空闲容量彻底回收 (`slices.Clip`)**：
  - 索引构建完成后，对所有 Posting Lists、条目列表与字符串池执行 `slices.Clip`，彻底回收临时扩容切片的未用堆空间。

#### 4. 协程生命周期闭环与高并发防御加固
- **修复 `LoadActiveFilmReadModel` 协程假就绪缺陷**：
  - 消除异步构建协程误中双缓冲秒返分支问题，确保索引真正构建完成后才触发 WaitGroup `Done()` 与 GC，生命周期严格受控；
- **消除在线读流量并发协程堆积隐患**：
  - `getOrLoadFilmSearchMemoryIndex` 改用 SingleFlight 原生的 `DoChan(...)` 非阻塞通道复用，消除读流量触发的 Goroutine 膨胀与惊群震荡；
- **移除冗余孤儿协程**：
  - 移除 `InvalidateActiveFilmSearchIndex` 中的重复并发协程，重建入口由 `LoadActiveFilmReadModel` 统一管控；
- **全链路切片隔离**：
  - SingleFlight 结果切片统一通过 `cloneFilmListSnapshots` 进行浅层深拷贝隔离，彻底消除高并发分页修改引发的 Data Race，并在失败分支重置分页元数据保证状态一致；
- **废弃死代码清理**：
  - 彻底拔除历史废弃的 `matchesAdminSearch`、`matchesAdminSearchCategory`、`ItemName` 与 `pageEnd`。

#### 5. 质量与回归验证
- **静态检查**：`go vet ./...` 0 错误、0 警告；
- **并发竞态**：`go test -v -race ./internal/repository/film/...` 100% 通过，0 Data Race；
- **全仓测试**：`go test -count=1 ./...` 全库 18 个模块全部测试通过。

