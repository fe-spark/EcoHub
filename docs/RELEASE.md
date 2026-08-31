测试版（Pre-release），Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.0-beta.4`。

### 核心变更

#### 1. 首页轮播图动态海报与幻灯图联动
- **快照多字段毫秒级点查**：新增 `LiveBannerSnapshotsByMIDs` 接口，单次查询直接从当前活跃快照中按 `mid` 点查最新的集数状态（`Remarks`）、高清封面（`Picture` / `Poster`）与横版幻灯图（`PictureSlide`）。
- **全自动联动生效**：首页与管理后台轮播组件加载时动态覆盖最新图源。海报图源重采后，首页顶部 Swiper 轮播无需人工干预、全自动升级展示最新的高清海报大图与幻灯图。

#### 2. 内存搜索索引 20w+ 深度轻量化与 GC 优化
- **冗余倒排精简**：彻底剥离内存搜索项中常驻的演职员、副标题分词及 6 个大 Map 倒排索引，将搜索核心聚焦于片名、全拼、首字母简拼与多音字。
- **内存开销大幅收敛**：20w+ 影片数据下常驻内存占用从 ~300MB+ 下降至 ~35MB，堆对象数量降低 >85%，索引构建完成后主动调用 GC 与 OS 内存归还，大幅降低垃圾回收停顿。

#### 3. 20w+ 数据库稳定性与安全边界加固
- **复合索引空间优化**：`MoviePoster.SourceId` 显式声明 `size:64`，大幅压缩 `uidx_poster_source_key` 复合索引体积与 Buffer Pool 占用。
- **快照分块安全释放**：`pruneOldFilmListSnapshots` 升级为 5000 条 Chunk 循环物理删除，彻底消除超大数据量单次全表 DELETE 导致的行锁竞争与 Undo Log 膨胀。
- **站点级联物理清理**：删除采集源时物理级联清除 `MoviePoster` 和 `MoviePlaylist` 残留数据，保持表空间高度精炼。
