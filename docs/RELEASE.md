测试版（Pre-release），Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.0-beta.7`。

### 核心变更

#### 1. 修复增量快照淘汰后全站无法播放（核心修复）
- **读模型版本容错回退**：`GetActiveReadModelVersion()` 将内存读模型空 Version 视为无效，回退到 Redis/DB 活跃快照版本。`ClearActiveFilmReadModel` / `init` 写入的是非空指针但 `Version=""`，原先的 nil 判断永远不触发，播放详情随即返回空数据（「当前影片播放数据不存在或已失效」）。
- **播放详情与列表查询对齐**：`GetSnapshotByMid` / `GetSnapshotsByMidsOrdered` 在 version 为空时与分类列表一样解析活跃快照版本，避免首页能点、播放页全挂。
- **增量淘汰不再清空版本**：`InvalidateIncrementalSnapshotCaches` 只重置内存搜索索引并立刻写回当前快照版本。beta.5 在采集/保存后直接 `ClearActiveFilmReadModel()`，是全站无法播放的直接触发点。

#### 2. 首页轮播海报联动横图自动兜底与自定义保护
- **横版幻灯图自动兜底**：当海报源只提供竖版高清海报而无横版幻灯图时，系统自动使用高清封面（`Picture`）对齐覆盖 `PictureSlide`，彻底解决客户端优先读取 `PictureSlide` 导致旧图残留未生效的问题。
- **自定义海报独立存储与锁定**：通过 `custom_picture` / `custom_picture_slide` / `is_custom_picture` 字段与底层采集原图彻底解耦；管理员自定义后自动加锁保护，切换为跟随海报源时即时无损还原。

#### 3. 采集站海报源单源互斥与全链路安全闭环
- **海报源全链路自动兜底**：采集源的新增、更新与删除全面接入 `EnsureDefaultPosterSourceTx` 事务校验，当外部海报源被关闭或移除时，底层自动将主站恢复为默认海报图源。
- **管理端 Tooltip 文案精炼**：精简轮播管理与影片编辑页的提示说明，去除冗余描述，操作直观干练。
