# v1.1.5-beta.11

> **预发布（prerelease）**：采集站管理交互收敛、批量总进度与 2C2G 采集默认提速，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.11`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.11`

## 相对 beta.10

- **采集站页交互收敛**：
  - 去掉页头常驻「终止全部 / 清理失效源 / 新增」按钮堆叠
  - 批量采集启动后出现**总进度条**，进行中可终止任务；总进度文案按阶段展示（采集中 / 收尾 / 完成），不再显示无意义的 `0/N 站`
  - 工具栏批量操作顺序：批量采集 → 批量启用 → 批量禁用 → 批量删除（标准按钮样式）
  - 「清理失效采集源 / 新增采集站」改为工具栏次级文字链接；附属站网格末尾虚线瓦片新增
- **采集站数量上限 12**：前后端一致校验；达上限隐藏新增入口
- **批量删除**：勾选后一键删除；主站与采集中站点自动跳过并提示原因
- **采集默认提速（2C2G 速度档）**：
  - 写阀默认：inflight 3 / 24 页/秒 / burst 8 / 单站队列 48 / 全局 144
  - 页并发：`COLLECT_PAGE_WORKERS=6`，单站 solo 时 `10` 吃满写阀
  - 站点并发：默认同时跑 6 站（`COLLECT_SOURCE_CONCURRENCY`，0=不限制）；主站优先派发
  - 默认请求间隔 100ms
- **收尾进度不误超时**：`page_done` / `waiting_publish` / `finalizing` 不再按单站 stale 标 failed
- **移除** `diagnose-notify` 诊断命令（脚本仍在 `server/scripts/`，需时可自行恢复）

## 部署（beta.11）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.11
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.11
docker compose pull && docker compose up -d
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.10

> **预发布（prerelease）**：采集站管理 UI 重构、进度生命周期与失效源清理，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.10`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.10`

## 相对 beta.9

- **采集站管理页重构**：表格改为主采集站横条 + 附属采集站卡片网格；启用/禁用用左边条与状态胶囊区分；同排卡片等高、底部操作沉底对齐
- **主站 / 附属站分类展示**：主采集站单卡操作条，附属采集站多卡网格；全局操作（清理失效源、终止全部、新增采集站）上移页头，工具栏仅保留批量选择操作
- **采集进度体验**：
  - 点击开始采集立即展示 **0% / 排队中**（单站与批量均同步标记 `starting`，前端乐观更新）
  - 终态进度（done/failed/stopped）在仍有活跃采集时全部保留，**全部结束后统一消失**，避免部分卡片进度条先没
  - 进度区恢复阶段文案：等待收尾 / 收尾发布中 等（`resolveCollectStatusText`）
- **失效源清理**：批量检测采集站接口连通性，弹窗确认后批量删除失效源
- **文案统一**：采集相关 UI 统一为「采集站 / 主采集站 / 附属采集站」；侧栏菜单采集中心排在内容管理之前
- **工作台**：运行概览 / 当前主采集站移至工作台展示（轻量轮询），采集站页专注列表与批量操作
- **初始化**：移除首页封面/轮播示例预设数据，新环境不再自动写入

## 修复

- 批量采集启动后进度条迟迟不出现（改为同步 `PrepareBatchCollectStart`）
- 失败站进度单独超时消失导致网格参差、有的有进度有的没有
- 卡片进度区缺失「等待收尾 / 收尾发布中」阶段展示
- 清理失效源 loading 使用 antd 6 废弃的 `tip`，改为 `description`

## 部署（beta.10）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.10
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.10
docker compose pull && docker compose up -d
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.9

> **预发布（prerelease）**：数据重置重构（异步 + 真实进度 + 独立入口 + 影响面统计），**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.9`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.9`

## 相对 beta.8

- **数据重置重构**：
  - 重置改为**异步执行**：点击确认后立即返回「数据重置已发起」，后台真实执行；前端轮询进度接口，按**关键节点**展示真实进度（停止任务 → 清空影视库存 → 清空派生数据 → 清空分类映射 → 清理缓存），失败时透出真实错误
  - 重置范围收敛为纯采集数据：不再重建分类（分类属采集数据，全量采集时自动从主站同步，消除外部网络依赖）、不再重置任何配置类数据（网站配置、轮播、映射规则、采集源、定时任务）与账号
  - 修复清表后采集统计内存缓冲回写旧数据（`ResetCollectStatsCoalescer`）
  - 入口迁移为侧边栏**独立顶级菜单「数据重置」**（红色警示样式），页面提供**影响面统计**（影视库存 / 列表快照 / 分类 / 失败记录），重置完成后自动刷新归零
- **采集站点列表**：各列设置合理宽度，站点 URI 超长截断 + 悬停查看完整地址
- **数据重置进度/统计接口**：`GET /manage/spider/clear/progress`、`GET /manage/spider/clear/stats`

## 修复

- 数据重置清空影视数据后，内存采集统计缓冲可能回写旧 `last_collect_time`
- 数据重置不再因主站分类树拉取失败而受影响（分类重建改由全量采集自动完成）

## 部署（beta.9）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.9
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.9
docker compose pull && docker compose up -d
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.8

> **预发布（prerelease）**：更新列表防抖与共享匹配键去噪，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.8`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.8`

## 相对 beta.7

- **附属站共享匹配键去噪**：同一影片多条目共享匹配键（如「XXX英语」「XXX国语」同豆瓣 ID）按落库后写覆盖去重，多条目并存不再误判为结构变化；某 key 本次无内容（源站改名/条目消失残留）不进列表
- **防源站剧集抖动**：更新列表仅计「新增集数/新增线路」（集数标签无序多重集比较）；集数相同（仅链接/顺序变化）不通知；集数回退（源站/CDN 返回不稳定，如 16↔18）不通知且不覆盖已存内容，DB 保持已见最大集数，杜绝连载剧发布期同一 mid 每批反复上报；主站写路径同步保护
- **批次回调错误分类**：批次加载区分不存在/过期/为空，回调提示不再一律「列表已过期」（多实例共用 Bot Token 被另一实例消费时提示「批次不存在」）
- **诊断工具判定对齐**：`diagnose-notify` 输出区分「新增集数/线路」「集数相同（仅顺序/链接）」「集数回退（抖动）」，并按 live key 补查历史行，避免把已存在行误判为「首次写入」

## 修复

- 连续批量采集更新列表反复出现同一影片（共享匹配键多条目 + 源站剧集抖动两类根因）
- 附属站「英语/国语」等双条目共享豆瓣键导致的每批重复通知
- 连载剧发布期源站返回集数不稳定导致的震荡上报
- 更新列表回调「刚收到就过期」误提示（区分不存在/过期/为空）

## 部署（beta.8）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.8
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.8
docker compose pull && docker compose up -d
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.7

> **预发布（prerelease）**：修复连续采集更新列表反复推送相同影片，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.7`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.7`

## 相对 beta.6

- **附属站更新列表去噪**：同一 `match_key` 命中多个 mid（如「烬九州：第二季」与「烬九州第二季」标点双包）时只通知最优 mid，避免一次变更推两条近似片名
- **FirstInsert 通知**：写库前若该附属站已为该 mid 写过 playlist，仅扩展 match key 不再当「新片上线」刷屏
- **播放源摘要刷新收窄**：只刷新本批有 playlist 写入的 mid，不再把页内全部匹配片刷进 finalizer（消除「每次 mid_count=76」类误导日志与多余快照压力）
- **诊断工具**：`server/cmd/diagnose-notify`（`scripts/diagnose-notify.sh`）分析当天变更批次、库/源站结构对比与双次拉取抖动

## 修复

- 连续批量采集 TG「更新列表」反复出现同一批/近似片名影片
- 附属站 match_key 一对多 mid 导致 stamp/通知扩散
- 收尾 PlaySummary / 快照范围过宽

## 部署（beta.7）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.7
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.7
docker compose pull && docker compose up -d
```

可选：库存中标点双包片（同一季两个 `vod_*`）仍可能并存于前台，可按 `movie_match_key` 多 mid 分组人工合并；通知侧已不再双推。

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.6

> **预发布（prerelease）**：Telegram 通知健壮性与系统日志结构化升级，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.6`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.6`

## 相对 beta.5

- **Telegram Bot 轮询多实例安全**：Redis 领导锁 + getUpdates Conflict 退避，多 EcoHub 实例共享 Redis 时仅 leader 长轮询；修复并发启停的 WaitGroup 竞态（潜在进程 panic）
- **系统日志结构化级别**：级别在写入时确定（INFO/WARN/ERROR），服务端落盘打结构化标签、重启可恢复；前端不再按正文关键词猜测；控制台（stdout）保持无标签原始输出
- **批量源配置变更聚合通知**：批量启用/禁用聚合为「批量 N 个」，超长按页拆分不丢源；部分源失败仍推送已生效变更（限流按源集合指纹，不同源集合互不限流）
- **定时任务事件补齐**：模型 0/1/2 任务级 `cron_task_done` / `cron_task_failed` 全覆盖（含未配置源、类型废弃等结构性失败）
- **0 页采集进度对齐**：无新内容时批量 `waiting_publish` / 单站 `page_done`，前端显示「无新内容」而非卡「即将开始采集」
- **变更批次过期判定**：改 Go 侧比较，修复时区差异导致回调「刚收到就过期」

## 修复

- 并发调用 `EnsureBotPoller` 可能触发 `sync: WaitGroup misuse` panic（改为按代句柄等待）
- 批量启用/禁用中途失败时静默丢失已生效源的变更通知
- Telegram webhook 未清除错误被误判为多实例冲突（现保持领导权并重新清除）
- 变更批次 `expire_at` SQL 时区比较误判过期
- 0 页站点采集列表状态与生命周期不一致

## 部署（beta.6）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.6
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.6
docker compose pull && docker compose up -d
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.5

> **预发布（prerelease）**：取消启动 bulk ContentKey 迁移，改为写路径懒兼容，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.5`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.5`

## 相对 beta.4 / beta.3

- **取消**启动 / 写库前 bulk 迁移 `name_*` → `vod_*`（不再卡启动、不再挡写库）
- **写路径兼容**：主站按 `mid`（= 源站 vod_id）冲突更新；重采即懒升 `content_key`（业务无变更也会升）；未再采的旧行保持 `name_*`，展示/播放不受影响
- 新片仍用 `vod_{源站id}`，避免片名误合并
- 软删占键自动释放；**活跃行**占着目标 `content_key` 时写库失败并打日志（不抢键）
- 历史误合并片需再次采集对应 vod 后才会拆成多条

## 破坏性变更（BREAKING）

### 主站 `ContentKey` 身份键

- **行为变更**：新写入优先 `vod_{源站 vod_id}`。
- **旧库存**：不改写；随采集懒升。
- **建议**：升级后正常增量即可；若曾出现片名误合并，对相关片或主站做一次全量采集。

## 修复

- 启动迁移卡死 / 迁移体验差（改为不迁旧数据）
- 主站更新列表反复推送相同影片（ContentKey 片名误合并）
- 后台文案：「恢复默认值」→「重置站点数据」
- `.env.example` / compose 对齐 Telegram 代理与采集写阀变量

## 部署（beta.5）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.5
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.5
# 可选：TELEGRAM_PROXY=http://host.docker.internal:7890
# 1. 建议先停自动/定时采集
# 2. 拉取并启动（无需清库、无需启动迁移等待）
docker compose pull && docker compose up -d
# 3. 正常增量采集即可；曾误合并的片建议主站全量采一次
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.4

预发布：曾用 MySQL 批量 bulk 迁移 ContentKey；**beta.5 起取消 bulk 迁移**，改写路径懒兼容。镜像：`…:v1.1.5-beta.4`。

---

# v1.1.5-beta.3

预发布：ContentKey 自动迁移与后台公告。镜像：`…:v1.1.5-beta.3`。

---

# v1.1.5-beta.1

预发布：ContentKey 改为 `vod_{vod_id}`；后台「重置站点数据」文案。镜像：`…:v1.1.5-beta.1`。

---

# v1.1.4

## 修复

- 布局级导航 loading 在同链/未进入 pending 时整页卡死（同链短路 + 路径到达 settle + 会话代次）
- Header 首页/同分类/同搜索重复点击触发无意义导航
- 前台 Pagination 大号 token 污染后台 `/manage` 分页
- `buildBackendApiUrl` 在带子路径 `API_URL` 时丢失 pathname，与 rewrite 不一致
- 分类筛选 pending 用 query 字符串全等判断，参数键序/空值不一致时卡到超时

## 优化

- 前台导航改为布局级 content loading，避免页面卸载后 transition 无法 settle
- 筛选页列表区 loading 与语义化 query 到达判定
- FilmList / Hero / 热榜统一走布局级播放跳转 loading
- SSR API 日志仅在 development 输出详情

## 新增

- `PublicContentLoading` 布局级内容加载与 `useContentNavigate`
- `SiteLogo`：未配置用本地 `/logo.png`，已配置原样加载（失败不兜底）
- `api-base`：`API_URL` 规范化（带/不带 `/api` 均可）

## 修改

- 后台站点配置初始 `logo` 置空（由前端未配置时走本地默认）
- 文档补充 Docker `API_URL` 与直连后端 `/api` 两种访问模型说明

## 部署

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
cd ~/ecohub && docker compose up -d
```

默认账号：`admin / admin`（管理员）、`guest / guest`（只读）。正式部署请修改密码与 `JWT_SECRET`。
