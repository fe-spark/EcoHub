# 检索索引只常驻主站

本文只有中文版。

内存检索索引只保留当前主站的影片。切换主站时丢掉旧索引，按新主站重建。片库表不改，不迁移数据。

对应问题：[fe-spark/EcoHub#98](https://github.com/fe-spark/EcoHub/issues/98)。v2.7.9 空闲约 233MB，v3 `:latest` 空闲约 4.7GB。两边都没有采集任务、没有播放，MySQL 与 Redis 均在容器外。差额来自启动时把 `film_index` 整表封进进程。

## 规则摘要

前台首页、分类、搜索和每日更新只展示主站。内存索引与这个范围对齐：进程里只有当前主站。

采集收尾仍为全部采集站发布快照、搜索标签，并让「待检索」结束。只有把影片编号打进内存索引的那一步限定主站。

播放页的其他站线路、海报源封面、TVBox 列表继续查库，不读这份索引。

## 相关对象

| 对象 | 位置 | 重构后的职责 |
| --- | --- | --- |
| 主站 | `PickPrimarySourceForCategory`、`isPreferredCollectSource` | 已启用采集站中排序最前的一个。索引只装这一站 |
| 检索索引 | `activeFilmSearchMetas` | 常驻一份。身份是快照版本加主站 ID |
| 全量加载 | `loadSearchIndexParts`、`buildAndStoreFilmSearchMetaIndex`、`scanAndStoreActiveFilmSearchIndex` | 只读 `first_source_id` 等于当前主站的行 |
| 采集封口 | `scheduleSearchReseal`、`ResealSearchIndex` | 非主站编号不入队。入队后的增量与整表重建都只含主站 |
| 按编号写入 | `UpsertMidsToActiveFilmSearchIndex`、`loadSearchMetaUpserts` | 非主站编号不写入。已不在主站的编号从索引移除 |
| 按编号剔除 | `RemoveMidsFromActiveFilmSearchIndex` | 只从当前索引删除。索引未就绪时补加载的也是主站 |
| 切换主站 | `onPrimarySourceMaybeChanged` | 丢掉旧索引，再为新主站全量加载，并预热首页 |
| 后台影片列表 | `GetSearchPageReadModel` | 当前主站读索引。其他采集站查 MySQL |
| 多源聚合 | `SearchInventoryCounts` | 不再从内存索引取数，改为查库 |
| 跨站线路 | `LoadPeerPlaylists` | 不改。按匹配键查库 |
| 海报 | `posterPicturesFor` | 不改。按匹配键查库 |

## 现状与终态

| | 现状 | 终态 |
| --- | --- | --- |
| 启动 | `WHERE deleted_at IS NULL` 装入全部采集站 | 只装当前主站。没有主站时装空索引 |
| 索引身份 | 快照版本。版本不变就直接复用 | 快照版本 + 主站 ID。主站变了必须重建 |
| 切换主站 | `WarmSearchMetaIndex` 确认全库索引仍在，并预热首页 | 先丢弃旧索引，再只建新主站，然后预热首页 |
| 重建峰值 | 旧索引保留到新索引封口完成，内存叠成两份 | 先丢弃再加载。切换期间前台列表等待新索引 |
| 采集快照与搜索标签 | 每个采集站都写库 | 不变 |
| 采集封口入队 | `sealFinishedSource`、`finalizeStreamPublish` 把该站全部编号送进 `pending` | 只有主站编号进入 `pending` |
| 封口时索引为空 | `ResealSearchIndex` 调用 `RequestActiveFilmSearchIndexRebuild`，扫描整张 `film_index` | 只扫描当前主站。本轮没有主站编号时不扫描 |
| 封口增量 | `loadSearchMetaUpserts` 按编号读入，不看所属站 | 非主站编号不写入。主站编号仍增量封口 |
| 待检索 | 每站 `addSealing`，封口结束后 `MarkSealed` | 不变。非主站也要结束待检索 |
| 首页预热 | `warmPrimaryHome` 只在该站当时是主站时预热 | 不变 |
| 手动保存、刮削 | `UpsertActiveSnapshotsByMids` 把该 `mid` 写进索引 | 仅主站影片写入。其他站只更新快照 |
| 删除影片、更换采集地址、删除采集站 | `DeleteActiveSnapshotsByMids` 从索引剔除 | 索引里有则剔除。不因此装入其他站 |
| 片库清理后 | `ActivateRebuiltFilmListSnapshot` 重载全库索引 | 重载当前主站 |
| 前台 | 读全库索引后再滤到主站 | 索引内就是主站 |
| 后台其他站影片列表 | 在同一份索引里按站筛选 | 该站查 MySQL |
| 工作台影片数、多源聚合 | `SearchInventoryCounts` 扫内存索引 | 查库，各站都能统计 |
| 播放页多站线路、海报、TVBox | 查库 | 不变 |

## 规则

1. **只装主站。** `filmMetaLoadSQL` 与 `loadSearchIndexParts` 增加 `first_source_id = 当前主站`。播放线路只取这些 `mid`。`streamPlaySources` 在全量加载时不得再扫全表线路。没有已启用采集站时，索引为空，不扫全表。
2. **身份含主站。** `filmSearchMetaIndex` 记下主站 ID。`loadFilmSearchMetaIndex`、`searchIndexReady`、`searchIndexCurrent` 在快照版本相同但主站不同时不得复用旧索引，也不得把封口结果写回旧索引。
3. **切换先丢后建。** `onPrimarySourceMaybeChanged` 在通过现有代数和互斥检查后，清空 `activeFilmSearchMetas`，再按新主站调用全量加载。沿用 150ms 防抖、串行锁和 `IsPrimarySiteSwitching`。构建中拒绝再次切换。`WarmSearchMetaIndex` 不得再把「版本已存在」当成主站没变。
4. **收尾分两段。** 快照、搜索标签、TVBox 配置缓存、待检索状态对所有采集站保持原样。内存索引只接收主站编号。判断必须放在 `ResealSearchIndex` 决定整表重建之前。
5. **增量不带入其他站。** `updateSearchMetaIndexIncremental`、`patchSearchMetas`、`UpsertMidsToActiveFilmSearchIndex` 丢掉 `first_source_id` 不是当前主站的变更。主站影片被删或不再属于主站时，从索引移除。其他站采集、手动保存和刮削不得触发全库重建。
6. **后台按站分流。** 影片列表的采集站是主站时继续走内存索引。是其他站时查 `film_index`，筛选、排序和分页与现列表一致。不把该站装进常驻索引。
7. **多源聚合改查库。** 计数规则不变：一部影片自己有两个及以上采集站的线路，或同一聚合片名出现在另一个采集站，计入所属站。工作台整库数字仍只取主站；展开各站时各站都要有数。禁止为这个数字把其他站装回索引。`countMultiSourceBySource` 退出工作台数据源。
8. **播放与封面不走索引。** `LoadPeerPlaylists`、`posterPicturesFor` 保持按匹配键查库。测速优选读的是播放接口返回的线路。

## 采集收尾

单站抓页结束进入待检索，调用 `addSealing`。`sealFinishedSource` 先刷完该站尚未发布的窗口，再 `scheduleSearchReseal(takeSourceMIDs)`。批次最后 `flushAndFinalize` 再把剩余编号送进同一个队列。队列等全部采集队列结束后调用 `ResealSearchIndex`。索引不可用或仍为空时，现实现改为 `RequestActiveFilmSearchIndexRebuild`，由 `scanAndStoreActiveFilmSearchIndex` 扫描整张 `film_index`。

这段里，发布和进度不改，入内存索引的范围要改。

不改：

- `publishStreamWindowReal`：`UpsertSearchTagsByMids` 与 `publishFilmSnapshot`。
- `TouchActiveSnapshotsByMids`：只登记列表缓存失效，不碰检索索引。各站快照照常发布。
- `ClearTVBoxConfigCache`：订阅配置缓存照常清。
- `warmPrimaryHome`、`finalizeStreamPublish` 的 `warmupHome`：已经只在本轮含主站时预热首页。
- `isPreferredCollectSource` 在 `streamPublishBuffer` 上的用途：只决定窗口先刷主站，不决定索引成员。
- `addSealing`、`complete`、`MarkSealed`：非主站也要离开待检索。停止采集后的 `MarkStoppedAsSealing` 同样保留。
- `collect_single_film.go`、`spider_dispatch.go` 仍调用同一套 `sealFinishedSource` 与 `flushAndFinalize`，不另写收尾。

要改：

- `scheduleSearchReseal` 收到的编号先去掉非主站。非主站本轮 `pending` 为空，封口队列仍因 `sealing` 非空而 `complete`，并发出检索终态。
- `ResealSearchIndex` 在判断「索引为空则整表重建」之前去掉非主站编号。只采了非主站时不得调用 `RequestActiveFilmSearchIndexRebuild`。
- `loadSearchMetaUpserts` 读到的行若 `first_source_id` 不是当前主站，不进入 `upsertMap`；若该 `mid` 已在索引中，则进入 `drop`。
- `scanAndStoreActiveFilmSearchIndex` 与 `loadSearchIndexParts` 只扫描主站。封口失败后的补建不得扫回全库。
- 封口持锁写回前再次核对主站 ID。主站已变则放弃本轮结果，不把旧站或新站的编号写进错误的索引。

## 示例

采集站顺序为 A、B、C，且均已启用。主站为 A。进程内索引只有 A 的影片。

采集 B。每窗仍写 B 的快照和搜索标签。B 进入待检索后，编号不进入 `pending`。封口结束时 B 离开待检索，索引条数不变，不扫描 `film_index`。

采集 A。A 的编号进入 `pending`。索引已有 A 时按编号增量封口。索引为空时只扫描 A，不读入 B、C。

同时采集 A 和 B。快照都发布。封口队列里只有 A 的编号。A、B 都在封口结束后离开待检索。首页只在 A 这一侧预热一次。

打开 A 的一部影片时，播放页用匹配键找到 B、C 中的同一部，并把线路附在 A 的档案上。封面仍可取自海报源。

将 B 移到最前。旧的 A 索引立即丢弃，按 B 全量重建。完成前前台列表等待。完成后首页、分类、搜索、每日更新展示 B。

在 B 为主站时于后台保存或刮削 C 的影片。快照更新，C 不进入内存索引。打开后台影片列表并切到 C 时，这一页查 MySQL。

## 改动清单

加载与身份：

- `server/internal/repository/film/snapshot/active_read_model_search_load.go`：`filmMetaLoadSQL`、`loadSearchIndexParts`、`streamPlaySources` 的全量路径只取主站。`buildAndStoreFilmSearchMetaIndex` 换上的索引带主站 ID。没有主站时存空索引。
- `server/internal/repository/film/snapshot/active_read_model_search_seal.go`：`filmSearchMetaIndex` 增加主站 ID。`loadCurrentSearchIndex`、`WarmSearchMetaIndex` 按版本加主站判断。
- `server/internal/repository/film/snapshot/search_reseal.go`：`searchIndexReady`、`searchIndexCurrent` 同样比较主站 ID。
- `server/internal/repository/film/snapshot/active_read_model.go`：`LoadActiveFilmReadModel` 使用上述加载。启动不再扫全表。
- `server/internal/repository/film/snapshot/search_rebuild.go`：`scanAndStoreActiveFilmSearchIndex` 只扫主站。注释中的「全库扫描」改为「主站扫描」。
- `server/internal/repository/film/snapshot/snapshot_repo.go`：`EnsureActiveFilmListSnapshot`、`ActivateRebuiltFilmListSnapshot` 仍负责建立版本，加载结果只有主站。片库非空但主站为空时索引为空。
- `server/internal/repository/film/snapshot/active_read_model_search_meta.go`：`InvalidateActiveFilmSearchIndex` 重载结果只有主站。
- `server/internal/repository/film/snapshot/snapshot_upsert.go`：`rebuildActiveFilterOptions`、`RefreshActiveReadModelArtifacts` 重载结果只有主站。
- `server/internal/service/init_service.go`：`loadActiveFilmReadModel` 不另写查询。已加载且主站未变时仍跳过第二次加载。

采集收尾：

- `server/internal/spider/collect_batch_context.go`：`sealFinishedSource` 送出的编号限定主站。`flushAndFinalize` 交给 `finalizeStreamPublish` 的剩余编号同样限定。
- `server/internal/spider/collect_stream_publish.go`：`finalizeStreamPublish` 在 `scheduleSearchReseal` 之前去掉非主站编号。快照发布与首页预热条件不改。
- `server/internal/spider/collect_search_reseal_queue.go`：队列、等待、`addSealing`、`complete` 不改。入队编号已不含非主站。
- `server/internal/repository/film/snapshot/search_reseal.go`：`ResealSearchIndex`、`loadSearchMetaUpserts`、`patchSearchMetas` 按规则 4、5 处理。非主站编号不得触发 `RequestActiveFilmSearchIndexRebuild`。
- `server/internal/repository/film/snapshot/active_read_model_search_seal.go`：`updateSearchMetaIndexIncremental` 不接收非主站条目。
- `server/internal/spider/spider.go`、`server/internal/spider/collect_single_film.go`、`server/internal/spider/spider_dispatch.go`：仍走原收尾函数，不增加分支。

其他写入：

- `server/internal/repository/film/snapshot/active_read_model_search_meta.go`：`UpsertMidsToActiveFilmSearchIndex` 跳过非主站；索引未就绪时的补加载只装主站。`RemoveMidsFromActiveFilmSearchIndex` 只剔除，补加载同样只装主站。
- `server/internal/repository/film/snapshot/snapshot_upsert.go`：`UpsertActiveSnapshotsByMids` 对非主站只更新快照，不调用索引写入。`DeleteActiveSnapshotsByMids` 继续剔除。
- `server/internal/repository/film/writer/manual_writer.go`：保存影片仍调用 `UpsertActiveSnapshotsByMids`，是否进索引由该函数判断。
- `server/internal/service/tmdb_apply.go`、`server/internal/service/tmdb_auto.go`：刮削仍调用 `UpsertActiveSnapshotsByMids`，规则同上。
- `server/internal/repository/film/admin_repo.go`：删除影片、`FilmZero` 清空索引保持原调用。`FilmZero` 之后索引为空。
- `server/internal/service/collect_service.go`：更换地址、删除采集站产生的 `DeleteActiveSnapshotsByMids` 只剔除。`onPrimarySourceMaybeChanged` 改为丢弃后按新主站重建，不再调用只确认旧索引的 `WarmSearchMetaIndex`。
- `server/internal/service/collect_service_delete.go`、`server/internal/service/spider_category_sync.go`、`server/internal/service/index_service_detail.go`、`server/internal/service/index_service_relate.go`：删除或失效影片仍只剔除，不重载全库。
- `server/internal/spider/spider_cron.go`：`cleanGhostFilms` 在删过影片后仍调用 `ActivateRebuiltFilmListSnapshot`，重载结果只有主站。

读取与统计：

- `server/internal/repository/film/snapshot/active_read_model_manage_search.go`：`GetSearchPageReadModel` 在采集站不是主站时查 MySQL。
- `server/internal/repository/film/inventory_stats.go`：`GetLibraryInventory`、`GetSourceInventories` 的影片数与多源聚合改查库。
- `server/internal/repository/film/snapshot/search_multi_source.go`：`countMultiSourceBySource`、`patchMultiSourceCounts` 不再被工作台调用。索引内部若仍计算该字段，不得为此读入其他站。

不改文件：

- `server/internal/repository/film/shared/play_supplement.go`
- `server/internal/repository/film/shared/poster_display.go`
- `server/internal/spider/collect_finalizer.go`
- TVBox、MacCMS 的提供接口
- 前台页面与客户端

删除只为全库常驻服务的分支。不保留「全库索引 / 主站索引」双路径。

## 验证

- 仅主站有影片时，索引条数等于该站 `film_index` 行数。再写入另一个站后，索引条数不变。
- 只采集非主站：快照和搜索标签写入，待检索结束，不调用整表重建，索引条数不变。
- 采集主站：新片能被前台搜索到。与非主站同批时，索引只增加主站影片。
- 索引为空时只采集主站，加载条数等于主站，不含其他站。
- 封口进行中切换主站，本轮结果不写回旧索引。
- 切换主站后，索引只含新主站。切换过程中内存里不同时持有两份索引。
- 手动保存或刮削非主站影片后，索引条数不变，后台该站列表仍能查到。
- 播放页仍能挂上其他已启用站的线路。海报源与主站不同时，封面仍来自海报源。
- 工作台主站影片数等于该站档案数。多源聚合在只常驻主站时仍计入跨站同名。
- 现有检索封口、重建、增量写入测试改为只覆盖主站范围。全库常驻的断言删除，不另留兼容用例。涉及文件：`search_reseal_test.go`、`search_reseal_stress_test.go`、`search_rebuild_test.go`、`active_read_model_search_meta_test.go`、`film_search_read_model_test.go`、`collect_search_reseal_queue_test.go`、`collect_batch_snapshot_test.go`。
