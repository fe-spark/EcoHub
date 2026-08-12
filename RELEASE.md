# v2.0.1

> **补丁版**：安全、稳定性与性能修复；正式版会覆盖 `ghcr.io/fe-spark/ecohub:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub:v2.0.1`
- `ghcr.io/fe-spark/ecohub:latest`

## 修复

- **UserUpdate 越权**：非管理员仅可修改本人账号，阻断横向改他人资料
- **CORS**：带凭证时仅允许与请求 Host 一致的 Origin；OPTIONS 预检正确 Abort，并设置 `Vary: Origin`
- **JWT 过期解析**：过期路径在返回 Claims 前做 nil 防护，避免 panic
- **采集任务 panic**：`RunTaskOnce` 异步执行增加 recover，防止单任务崩溃影响进程
- **映射规则写路径 DDL**：`Create/UpdateMappingRule` 移除运行时 `EnsureMappingRuleIndexes`，索引仅在初始化保证
- **VideoPlayer 小窗检测**：去掉常驻 rAF 循环，改为 IntersectionObserver + scroll/resize 节流

## 部署（v2.0.1）

```bash
# 推荐：安装脚本 + 发布版 Compose（默认 :latest）
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
cd ~/ecohub && docker compose pull && docker compose up -d

# 或固定版本：
#   image: ghcr.io/fe-spark/ecohub:v2.0.1
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。  
全部署方式见 [README-Deploy.md](./README-Deploy.md)。

---

# v2.0.0

> **正式版**：All-in-One 单镜像部署、Telegram 通知路由矩阵与更新列表增强、分类树仅从主站同步。正式版会覆盖 `ghcr.io/fe-spark/ecohub:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub:v2.0.0`
- `ghcr.io/fe-spark/ecohub:latest`

## 版本定位

`v2.0.0` 将 Web 与 Server 合并为单一 All-in-One 镜像，并完成 Telegram 通知策略、更新列表去噪/分类浏览与分类树主站约束等能力收敛，适合生产环境直接拉取 `:latest` 或固定 `v2.0.0`。

## 主要变更（相对 v1.1.x）

### 镜像与部署

- **All-in-One 单镜像**：`ecohub-web` / `ecohub-server` 融合为 `ghcr.io/fe-spark/ecohub`
- 多阶段构建 + Supervisord 同容器托管 Go API（8080）与 Next.js（3000）
- 发布版 Compose / 安装脚本默认使用 `ecohub:latest`

### Telegram 通知

- Severity（INFO/NOTICE/WARN/ERROR/CRITICAL）、Category（collect/cron/audit）与 Quiet Hours 免打扰（东八区；ERROR/CRITICAL 默认可穿透）
- `onlyNotifyOnUpdate`：无更新且无失败时跳过批次摘要
- Targets 路由（Chat / Forum Thread / 最低等级 / 分类订阅）；`ChatIDs` 与 Targets 对齐
- Bot 命令白名单与发送目标同源
- 更新列表按导航大类筛选浏览；callback 短下标编码规避 64 字节限制

### 更新列表与采集

- 跨源按「分集数量严格增加」去重，并展示触发源名称
- 影片 `update_stamp` 解析失败时用当前时间兜底
- 分类树只能来自主站（含未启用主站）；无主站不构建分类树，禁止附属站写入

### 管理端与其它

- 通知设置 UI 分区重构；系统设置 Tabs、主题 SSR 防闪烁、浅色边框对比度等
- 采集进度会话、素材中心仅用户上传、管理端写权限透传、工作台真实接口图表等

## 部署（v2.0.0）

```bash
# 推荐：安装脚本 + 发布版 Compose（默认 :latest）
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
cd ~/ecohub && docker compose pull && docker compose up -d

# 或固定版本：
#   image: ghcr.io/fe-spark/ecohub:v2.0.0
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

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
