测试版（Pre-release），Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.0-beta.9`。

### 核心变更

#### 1. 后台影片管理百万级数据极速检索与只读模型重构
- **内存倒排搜索索引复用**：后台影片管理（`/manage/film`）片名检索全面复用前台 `filmSearchMemoryIndex` 内存倒排索引，支持汉字分词、全拼、首字母实时模糊评分召回，彻底消除底层 `film_index` 表无索引 `LIKE '%xxx%'` 导致的百万行双重全表扫描与文件排序（`Using filesort`），搜索响应时间从数秒降至 5ms 以内。
- **快照读模型与轻量字段投影**：无关键词的列表与组合筛选优先走轻量只读快照表 `FilmListSnapshot`，仅查询管理表格所需的精简字段，避开 `actor` / `blurb` 等长文本大字段，大幅降低磁盘 I/O 开销。
- **后台筛选选项快照 Redis 缓存**：对后台分类及标签选项树（`GetAdminFilterOptionSnapshots`）增加版本级 Redis 缓存，消除每次翻页与检索时重复全表查询选项表的开销。
- **优雅降级与全场景兼容**：在快照未生成或初始冷启动场景下平滑回退至底层数据查询，确保管理功能始终高可用。

#### 2. 快照删除与增量发布全链路精准淘汰优化
- **内存搜索索引失效封装与重载解耦**：将增量更新时的内存搜索索引失效逻辑抽象封装为 `InvalidateActiveFilmSearchIndex`，规范读模型内部原子状态管理，保证 Version 稳定处于有效状态。
- **快照删除缓存精准淘汰**：在 `DeleteActiveSnapshotsByMids` 中接入 `InvalidateIncrementalSnapshotCaches`，精确清理被删除影片的播放缓存并触发内存索引重置，避免粗粒度全量刷新导致全局缓存雪崩。

#### 3. 首页轮播海报联动横图自动兜底与自定义保护
- **横版幻灯图自动兜底**：当海报源只提供竖版高清海报而无横版幻灯图时，系统自动使用高清封面（`Picture`）对齐覆盖 `PictureSlide`，彻底解决客户端优先读取 `PictureSlide` 导致旧图残留未生效的问题。
- **自定义海报独立存储与锁定**：通过 `custom_picture` / `custom_picture_slide` / `is_custom_picture` 字段与底层采集原图彻底解耦；管理员自定义后自动加锁保护，切换为跟随海报源时即时无损还原。

#### 4. Web 与 Android 端防盗链海报兼容适配
- **Web 端全局防盗链策略**：全局设置 `referrer: "no-referrer"`，彻底解决 B站/豆瓣等第三方防盗链图源在网页端显示破损的问题。
- **Android 端图源 Referer 头智能注入**：Android 客户端新增 `FormatUtil.imageHeaders`，按图源 Host 动态注入对应的合法 Referer 校验头，保障全端封面与海报正常渲染。



