测试版（Pre-release），Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.1-beta.0`。

### 升级指引

- **从已有版本升级**：执行 `docker compose pull && docker compose up -d` 即可。
- **兼容性说明**：本版本完全向下兼容现有 MySQL 与 Redis 数据结构，无破坏性变更。

---

### v2.5.1-beta.0 核心变更

#### 1. Worker 纯读节点支持与集群部署架构
- **Worker 纯读模式**：新增 `NODE_ROLE=worker` 环境变量支持（主节点默认为 `master`）。Worker 节点专注于高并发用户前台检索与播放请求，自动屏蔽后台爬虫、定时采集、日志轮转与 Telegram Bot 轮询等写任务，避免主库写锁竞争。
- **多容器进程管理分流**：新增 `supervisord-worker.conf` 与 `entrypoint.sh`，根据节点角色智能自适应启动对应的进程守护模型。

#### 2. 集群快照多节点实时 Pub/Sub 同步
- **Pub/Sub 事件广播**：主节点快照发生变更时，通过 Redis 频道 `ecohub:cluster:snapshot_event` 即时发布重载广播；Worker 节点监听并实现毫秒级内存快照热加载。
- **长轮询与故障自愈兜底**：引入 `CLUSTER_SYNC_INTERVAL` 配置，配合 Redis 快照版本号对比长轮询机制，防御网络瞬断导致的消息遗漏，保障主从节点间片库、热播列表与推荐池强一致性。

#### 3. 部署编排与文档完善
- **Docker Compose 多节点编排**：在 `deploy/release/compose.yml` 中补充 Master-Worker 读写分离多节点编排示范与负载均衡说明。
- **配置与中英文档完善**：同步更新 `README-Deploy.md` 及英文版部署文档中的集群配置指南。
