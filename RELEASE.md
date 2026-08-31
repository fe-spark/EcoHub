测试版（Pre-release），Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.0-beta.8`。

### 核心变更

#### 1. 快照删除与增量发布全链路精准淘汰优化
- **内存搜索索引失效封装与重载解耦**：将增量更新时的内存搜索索引失效逻辑抽象封装为 `InvalidateActiveFilmSearchIndex`，规范读模型内部原子状态管理，保证 Version 稳定处于有效状态。
- **快照删除缓存精准淘汰**：在 `DeleteActiveSnapshotsByMids` 中接入 `InvalidateIncrementalSnapshotCaches`，精确清理被删除影片的播放缓存并触发内存索引重置，避免粗粒度全量刷新导致全局缓存雪崩；同时消除重复触发读模型重载的冗余调用。
- **读模型单测隔离与回归覆盖**：补全测试用例中 `activeFilmSearchIndex` 与 `activeFilmReadModel` 的原子状态清理与还原，增加增量淘汰保留版本号的回归测试断言。

#### 2. 修复增量快照淘汰后全站无法播放（版本容错回退）
- **读模型版本容错回退**：`GetActiveReadModelVersion()` 将内存读模型空 Version 视为无效，回退到 Redis/DB 活跃快照版本，避免播放详情返回空数据。
- **播放详情与列表查询对齐**：`GetSnapshotByMid` / `GetSnapshotsByMidsOrdered` 在 version 为空时与分类列表一样解析活跃快照版本，避免首页能点、播放页全挂。

#### 3. 首页轮播海报联动横图自动兜底与自定义保护
- **横版幻灯图自动兜底**：当海报源只提供竖版高清海报而无横版幻灯图时，系统自动使用高清封面（`Picture`）对齐覆盖 `PictureSlide`，彻底解决客户端优先读取 `PictureSlide` 导致旧图残留未生效的问题。
- **自定义海报独立存储与锁定**：通过 `custom_picture` / `custom_picture_slide` / `is_custom_picture` 字段与底层采集原图彻底解耦；管理员自定义后自动加锁保护，切换为跟随海报源时即时无损还原。
