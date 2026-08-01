# v1.1.0

EcoHub 是一个前后端分离的影视聚合系统，面向个人影视库、资源站聚合、TVBox / 影视仓配置输出和后台采集管理场景。

本项目仅用于学习和技术交流，不提供影视资源存储。

## 版本定位

`v1.1.0` 是 EcoHub 的采集引擎重构与稳定性升级版本，重点优化了 Spider 采集引擎的数据库 IO 安全防护、坏站自动熔断隔离、429 限流指数退避与单元测试全覆盖。

## 能做什么

- 前台影视站点浏览：首页、分类、搜索、详情、播放和观看历史。
- 多资源站采集与内容归并，以“单主站 + 多附属站”的方式管理影片数据。
- 附属站播放源补充，将多个来源的播放列表聚合到同一影片详情下。
- 后台站点管理：网站配置、首页封面、账号管理、图片素材和系统日志。
- 后台内容管理：影片录入、影片列表筛选、详情维护、删除和单片同步更新。
- 后台采集管理：采集站点维护、主站/附属站配置、批量启停、手动采集和任务终止。
- 分类与映射管理：维护主站分类框架、显示状态、排序和分类映射规则。
- 失败记录管理：查看采集失败明细，执行单条/批量重试，清理重试结果或全部记录。
- 计划任务管理：维护自动更新、采集重试和清理类计划任务，支持手动立即执行。
- 开放接口输出：提供 TVBox / 影视仓配置接口和 MacCMS 兼容查询接口。
- Docker Compose 部署：支持 Web、API、MySQL、Redis 组合部署，也支持外部数据库和缓存服务。

## 做了什么 (v1.1.0 更新明细)

- **Spider 采集引擎架构重构**：重构并发落库、熔断判定与多站隔离，精简冗余闭包与写库模板代码。
- **水龙头写库控频与 IO 防护**：采用全局写调度器 (`collectWriteScheduler`)，控制并发写事务数 (`inflight=2`) 与写吞吐 (12 页/秒)，彻底规避高并发落库引发的磁盘 IO 爆满与 MySQL 死锁。
- **429 限流指数退避算法**：遭遇源站 WAF 返回 429 时，保护冷却时间按 1s -> 2s -> 4s -> 8s -> 16s 指数退避，防止频繁硬撞拖慢批次。
- **坏站自动熔断与隔离**：单站连续 10 页拉取/落库失败自动中断上下文并释放资源，彻底隔离坏站与死站。
- **开局抖动与空响应重试**：将开局元数据/页数拉取重试提升至 3 次，并增加空 Body 响应退避休眠，防止因网络微小抖动造成整站流产。
- **反压日志限频采样**：反压恢复日志增加 CAS 3 秒原子限频，大幅降低高并发采集下的日志输出开销。
- **单元测试全覆盖**：为 `internal/spider` 模块新增关键逻辑单元测试，14 项测试全量通过。

## 默认入口

- 前台站点：`http://127.0.0.1:3000`
- 管理后台：`http://127.0.0.1:3000/manage`
- API：`http://127.0.0.1:3000/api/*`
- TVBox / 影视仓配置：`http://127.0.0.1:3000/api/provide/config`
- MacCMS 兼容接口：`http://127.0.0.1:3000/api/provide/vod`

## 默认账号

| 类型 | 账号 | 密码 | 权限 |
| --- | --- | --- | --- |
| 管理员 | `admin` | `admin` | 可读可写 |
| 访客 | `guest` | `guest` | 只读 |

默认账号只适合初始化和演示。正式部署后请立即修改密码，并配置高强度 `JWT_SECRET`。

## 部署方式

推荐使用发布版 Docker Compose。用户不需要分别运行 `ecohub-web` 和 `ecohub-server`，Compose 会自动拉取已发布镜像，并启动内置 MySQL / Redis：

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
cd ~/ecohub
docker compose up -d
```

正式部署前请先修改 `.env` 中的 `JWT_SECRET`、MySQL 密码和 Redis 密码，发布版和源码版保持同一套 Compose 环境变量。

默认数据目录：

```text
~/ecohub/data/mysql
~/ecohub/data/redis
~/ecohub/data/uploads
```

源码部署可使用仓库根目录的 `docker-compose.yml`：

```bash
cp .env.example .env
docker compose up --build -d mysql redis server web
```

也可以分别启动后端和前端进行本地开发：

```bash
cd server
cp .env.example .env
go run ./cmd/server
```

```bash
cd web
cp .env.example .env.local
npm install
npm run dev
```

## 注意事项

- EcoHub 不内置、不存储影视资源，只负责采集索引、聚合播放源和提供管理能力。
- 主站负责影片主数据，附属站负责补充播放列表。
- 生产环境请使用独立 MySQL / Redis 持久化数据。
- 对外部署前请修改默认账号密码、JWT 密钥、端口和数据库连接信息。
