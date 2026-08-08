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
