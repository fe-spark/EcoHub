测试版（Pre-release），Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.1-beta.1`。

### 升级指引

- **从已有版本升级**：执行 `docker compose pull && docker compose up -d` 即可。
- **兼容性说明**：本版本完全向下兼容现有 MySQL 与 Redis 数据结构，无破坏性变更。

---

### v2.5.1-beta.1 核心变更

#### 1. 恢复控制台全量 HTTP 请求日志打印与安全增强
- **常规请求控制台日志恢复**：恢复状态码 `< 400` 且耗时正常请求的控制台输出，统一使用 `INFO` 级别，便于实时排查流量与请求情况。
- **完整 URI 与 Query 回显**：日志输出由原本仅记录路径重构为输出完整请求 URI，精准呈现查询参数（如搜索词、分类 ID、影视 ID 等）。
- **CRLF 注入防御与 Rune 边界截断**：全面净化请求 URI 中的换行字符（`\r\n`），超长 URI 采用 UTF-8 字符（rune）级截断与省略号保护，杜绝控制台字符截裂。
- **自动化测试覆盖**：新增 AccessLog 中间件单元测试覆盖，使用 `t.Cleanup` 保证环境配置隔离。

#### 2. 集群架构与功能延续（承接 v2.5.1-beta.0）
- **Worker 纯读节点支持与守护模型**：支持 `NODE_ROLE=worker` 纯读模式，多容器进程自适应守护。
- **集群快照多节点实时 Pub/Sub 同步**：Redis 事件广播与长轮询毫秒级快照热重载。
- **多节点部署编排与文档**：Docker Compose Master-Worker 读写分离编排及部署文档。
