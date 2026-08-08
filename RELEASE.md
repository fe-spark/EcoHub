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
