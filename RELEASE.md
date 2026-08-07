# v1.1.5-beta.4

> **预发布（prerelease）**：修复百万级库存启动卡死，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.4`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.4`

## 相对 beta.3

- **修复**：ContentKey 迁移改为 MySQL 批量 SQL（软删释放 + 每 10 万行分块 `UPDATE ... NOT EXISTS`），百万级库存由「逐行 SQL 数十分钟」降为「十余条 SQL 秒级完成」，不再启动卡死
- 迁移日志新增 `migrate start` 与分块 `migrate progress`
- SQLite 逐行路径仅保留给单测

## 破坏性变更（BREAKING）

### 主站 `ContentKey` 身份键

- **行为变更**：指纹优先 `vod_{源站 vod_id}`，不再用规范化片名做主站主键。
- **启动自动迁移**：`name_*`+`mid>0` → `vod_{mid}`（幂等），**无需清库**。曾误合并的第二部片需再采才会出现。
- **后台公告**：建议全量采集；迁移失败/残留则提示重置。
- **建议步骤**：部署启动 → 看后台公告 → **建议主站全量采集**。
- **不可混版本**；可选仍可重置站点后全量重建。

## 修复

- 主站更新列表反复推送相同影片（ContentKey 片名误合并）
- 百万+ 库存启动迁移卡死（改为 MySQL 批量分块迁移）
- 后台文案：「恢复默认值」更名为「重置站点数据」，明确为清库重建而非仅恢复配置字段
- 根目录 / server `.env.example` 与 compose 对齐 Telegram 代理与采集写阀变量

---

# v1.1.5-beta.3

> **预发布（prerelease）**：验证 ContentKey 自动迁移与后台公告，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.3`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.3`

## 相对 beta.2

- 启动/后台入口自动迁移 `name_*` → `vod_{mid}`（释放软删占用键；同步快照）
- 后台公告：迁移成功（建议全量，14 天或主站收尾后消失）/ 残留或失败（去重置）
- 写库兜底拦截未迁净库存

## 修复

- 主站更新列表反复推送相同影片（ContentKey 片名误合并）
- 后台文案：「恢复默认值」更名为「重置站点数据」，明确为清库重建而非仅恢复配置字段
- 根目录 / server `.env.example` 与 compose 对齐 Telegram 代理与采集写阀变量

## 部署（beta）

```bash
# 将 compose 中的镜像 tag 从 latest 改为 beta，例如：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.2
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.2
# 可选：.env 中配置 TELEGRAM_PROXY=http://host.docker.internal:7890
# 1. 停自动采集 / 定时采集
# 2. 拉取并启动 beta 镜像（勿与旧 server 混连同一库）
docker compose pull && docker compose up -d
# 3. 管理后台：网站配置 → 重置站点数据（密码确认）
# 4. 重新配置站点后，主站全量采集
```

默认账号：`admin / admin`（管理员）、`guest / guest`（只读）。正式部署请修改密码与 `JWT_SECRET`。

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
