# v1.1.5-beta.2

> **预发布（prerelease）**：用于验证 v1.1.5 破坏性变更，**不会**覆盖 `ghcr.io/...:latest`。正式版将标记为 `v1.1.5`。

镜像（GHCR）：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.2`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.2`

## 相对 beta.1

- 旧版库存：进入后台顶部公告提示重置（写库仍有兜底拦截）
- Docker `.env` / compose 注入 `TELEGRAM_PROXY` 与 `COLLECT_*`
- 启动日志 `version=`；空 ContentKey 写入丢弃

## 破坏性变更（BREAKING）

### 主站 `ContentKey` 身份键

- **行为变更**：主站影片内容指纹由 `name_{规范化片名哈希}` 改为优先 `vod_{源站 vod_id}`（前缀表示源站影片 ID，与字段名 `FilmIndex.Mid` 无关；单主站模型下数值通常等于全局 mid）。
- **原因**：源站常见近似片名并存且集数不同（如「烬九州第四季」与「烬九州：第四季」），旧键会合并为同一 mid，增量采集时播放结构互相覆盖，Telegram 更新列表反复刷同一批片。
- **不做代码自动迁移**：升级后**不会**在启动时改写历史 `content_key`。旧库存（`name_*`）与新写入（`vod_*`）不兼容，**必须**在后台手动清空重建。
- **后台公告**：若仍存在旧版 `name_*` 库存，进入管理后台顶部提示重置；写库另有兜底拦截。
- **升级后必做（管理后台）**：
  1. 停采集 / 定时任务  
  2. 部署本版本并启动  
  3. 打开 **系统管理 → 网站配置 → 危险操作 →「重置站点数据」**（原「恢复默认值」），输入管理密码确认  
  4. 按需改回站点名、Logo、采集源等配置  
  5. **主站全量采集**（建议随后附属站按需采集）  
- **不可混版本**：禁止新旧 server 同时连同一库并采集。  
- 重置会清空影视库存、快照、播放源、映射、失败记录等，并恢复默认配置/账号/采集源/定时任务/轮播/主站分类，操作不可逆。

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
