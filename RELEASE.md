测试版（Pre-release），Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.0-beta.5`。

### 核心变更

#### 1. 影视与轮播自定义封面独立存储与防冲刷锁定
- **双层字段解耦架构**：在 `film_index`、`film_list_snapshot` 及 `banners` 表中引入 `custom_picture` / `custom_picture_slide` / `is_custom_picture` 独立存储字段，彻底与源站/海报源底层原图（`picture` / `picture_slide`）解耦。
- **采集防冲刷保护**：管理员手动自定义封面后系统自动加锁（`is_custom_picture = true`），后续日常采集、海报图源重采与批量更新坚决不覆盖用户自定义图片。
- **无损回退还原原图**：管理员在后台重新切换为“跟随海报源”时，系统自动清空自定义字段并无损恢复底层海报源/主站高清封面，彻底解决“自定义后原图丢失无法找回”的问题。

#### 2. 首页轮播海报联动与多态展示优化
- **管理端海报多态可视化**：轮播列表直观区分展示「海报源联动」与「自定义锁定」状态标签；
- **智能打底与实时叠加**：保存轮播项时若选择跟随海报源，自动查询片库快照打底；前台展示时通过 `OverlayBannerLiveRemarks` 实时叠加最新快照状态与幻灯图，兼顾灵活性与一致性。

#### 3. 增量快照全链路缓存精准淘汰与单站海报源兜底
- **精准批量淘汰**：在增量快照发布流程（`InvalidateIncrementalSnapshotCaches`）中通过 Redis Pipeline 批量精准删除被修改影片的 `EcoHub:filmPlayInfo:<mid>` 缓存，并原子重置内存读模型与搜索索引，避免全量大面积缓存击穿。
- **采集站海报源安全闭环**：添加、更新或删除采集源时，全自动执行 `EnsureDefaultPosterSourceTx` 兜底检查，当外部海报源关闭或移除时，自动将主站恢复为默认海报图源，保障系统始终具备稳定的海报抓取能力。
