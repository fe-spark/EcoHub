正式版 **v2.5.1**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.1` 与 `ghcr.io/fe-spark/ecohub:latest`。

### 升级指引

- **从已有版本升级**：执行 `docker compose pull && docker compose up -d` 即可（或后台「检查更新」一键平滑升级）。
- **兼容性说明**：本版本完全向下兼容现有 MySQL 与 Redis 数据结构，无破坏性变更。

---

### v2.5.1 核心变更

#### 1. 多节点集群 Worker 纯读节点与快照实时同步
- **Worker 纯读节点支持与守护模型**：支持 `NODE_ROLE=worker` 纯读模式，自适应容器守护模型（关闭采集、定时任务与 Telegram 机器人轮询等单写任务，专注于高并发读请求分流）。
- **集群快照多节点实时 Pub/Sub 同步**：基于 Redis Pub/Sub 事件广播与长轮询自适应毫秒级快照热重载，Master 写节点快照重建后 Worker 节点毫秒级无感知刷新。
- **集群测试用例与部署编排**：新增完整集群配置与快照同步自动化测试用例，提供 Docker Compose Master-Worker 读写分离编排与部署文档。

#### 2. 访问分析增强与 SSR 真实客户端 IP 透传
- **SSR 客户端真实 IP 与 UA 透传**：Next.js 服务端渲染向 Go 后端请求时，自动从 HTTP 上下文提取外部访客真实 `X-Forwarded-For`、`X-Real-IP` 及原始 User-Agent，彻底解决网页端访问被误识别为服务器自身公网 IP 的问题。
- **网页端业务动作推入实时明细流水**：将网页端搜索（search）、点播（play）与分类筛选（classify）打点推入实时访问明细流水，与外部 API 请求统一展示，并支持智能识别搜索关键词与对应规范路径。
- **Action 常量枚举重构**：消除分散的 Magic String，统一由 `ActionSearch` / `ActionPlay` / `ActionBrowse` / `ActionClassify` 枚举管理。

#### 3. 控制台全量 HTTP 请求日志恢复与安全增强
- **常规请求控制台日志恢复**：恢复状态码 `< 400` 且耗时正常请求的控制台输出，统一使用 `INFO` 级别，便于实时排查流量与请求情况。
- **完整 URI 与 Query 回显**：日志输出由原本仅记录路径重构为输出完整请求 URI，精准呈现查询参数（如搜索词、分类 ID、影视 ID 等）。
- **CRLF 注入防御与 Rune 边界截断**：全面净化请求 URI 中的换行字符（`\r\n`），超长 URI 采用 UTF-8 字符（rune）级截断与省略号保护，杜绝控制台字符截裂。
- **自动化测试覆盖**：新增 AccessLog 中间件单元测试覆盖，使用 `t.Cleanup` 保证环境配置隔离。

#### 4. 定时采集默认周期调整
- **默认间隔调优**：增量采集任务默认间隔由 20 分钟平稳调整为 30 分钟，减轻源站请求压力与数据库并发负担；前端表单、工具函数及帮助文案保持严格同步。
