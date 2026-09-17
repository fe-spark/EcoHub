预发布 **v2.7.0-beta.3**，镜像 `ghcr.io/fe-spark/ecohub:v2.7.0-beta.3`。

### 升级指引

- 后台检查更新，或 `docker compose pull ecohub && docker compose up -d ecohub`。
- 启动时自动执行数据库性能索引迁移，无需手动干预。

---

### v2.7.0-beta.3

- 重置站点数据闭环：重置时连带清空强依赖影片的轮播图与采集失败记录，彻底杜绝首页 404 死链与孤儿数据。
- 每日更新接口提速：新增 `(update_stamp, mid)` 与 `(pid, update_stamp, mid)` 覆被复合索引，消除全量 Filesort 文件排序。
- 每日更新缓存与并发收敛：分类统计与前 5 页分页接入短缓存与 Singleflight，拦截高频穿透，保持实时集数覆盖。
- 读模型快照与数据重置时闭环淘汰每日更新缓存。

---

预发布 **v2.7.0-beta.2**，镜像 `ghcr.io/fe-spark/ecohub:v2.7.0-beta.2`。

### 升级指引

- 后台检查更新，或 `docker compose pull ecohub && docker compose up -d ecohub`。
- 升级后对附属站再采一轮，同名串源才会重绑。

---

### v2.7.0-beta.2

- 同名跨类按豆瓣/名称/类别/标签/年份/备注形态打分绑定播放源。
- 错槽只清同一部写到别人独占键上的旧行。
- 快照发布、重置、主站切换会清详情与列表缓存。
- 单站采集失败不再重复释放占用。
- 管理端按队列展示采集进度。

---

正式版 **v2.6.8**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.6.8` 与 `ghcr.io/fe-spark/ecohub:latest`。

### 升级指引

- **平滑升级**：支持后台「检查更新」一键平滑升级，或通过 `docker compose pull ecohub && docker compose up -d ecohub` 快速更新。
- **历史数据归并（可选）**：历史附属站未对齐主键的旧播放列表可通过工具脚本一键归并：

```bash
docker exec -it ecohub /app/migrate_slave_playlist_keys --dry-run
docker exec -it ecohub /app/migrate_slave_playlist_keys
```

- **追集生效**：升级后对附属采集站再跑一轮采集即可自动对齐最新剧集。

---

### v2.6.8 核心变更

- **同名跨类隔离**：分类不编入 `mid`，通过匹配键大类隔离同名影片（如短剧与动漫），避免版本覆盖。
- **副站追集主键对齐**：附属站唯一命中一部影片时（含副站大类标错），播放列表自动对齐写入该片主键。
- **采集写入防锁优化**：附属站播放列表采用原地更新策略，避免多源并发写同一影片时产生死锁与堵塞。
- **多播放源站点隔离**：附属站剧集按 `source_id` 独立分列展示，不污染主站默认源。
- **详情展示最新剧集**：同站点存在多份列表时优先展示集数最多的一份，检测到变动即时刷新详情缓存。
- **采集卡慢与索引修复**：纠正 `movie_match_key` 索引定义并增加短路机制，消除几十万行全表扫描，采集恢复毫秒级。
- **版本化数据库迁移**：引入 `schema_migrations` 机制，表结构与历史补丁仅在升级时执行一次，避免启动重复开销。
- **采集站上限放宽**：取消 12 个采集站硬限制，改为高负载风险弹窗预警。
