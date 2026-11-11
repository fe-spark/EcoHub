# 服务端说明

中文 | [English](./README_EN.md)

`server/` 为 EcoHub 的 Go 后端，负责采集、片库、列表与缓存、订阅接口和后台鉴权。

功能行为（主站、匹配、刮削、缓存）见 [常见问题](../docs/README-FAQ.md)，计划任务的执行顺序见 [计划任务运行冲突](../docs/cron-task-conflict.md)，Docker 部署所用的变量见 [部署指南](../docs/README-Deploy.md#环境变量)。

## 本地启动

环境要求：Go 1.27+、MySQL 8+、Redis 7+。

如无可用数据库，可使用仓库根目录的开发编排 `docker-compose.yml` 仅启动数据库容器（该编排仅用于本地开发，与发布编排的区别见 [开发指南](../docs/README-Dev.md#开发编排)）。数据库映射到本机 `127.0.0.1:3306` 和 `127.0.0.1:6379`，账号和密码与 `server/.env.example` 一致：

```bash
docker compose up -d mysql redis
```

然后执行：

```bash
cd server
cp .env.example .env
go run ./cmd/server
```

`.env.example` 内容如下：

```env
PORT=8080
JWT_SECRET=ecohub_2026!local@dev_secret$001
ECOHUB_ADMIN_PASSWORD=admin
MYSQL_HOST=127.0.0.1
MYSQL_PORT=3306
MYSQL_USER=eco
MYSQL_PASSWORD=ecohub
MYSQL_DBNAME=eco
REDIS_HOST=127.0.0.1
REDIS_PORT=6379
REDIS_PASSWORD=ecohub
REDIS_DB=0
```

- 进程启动时读取当前目录的 `.env`，因此须在 `server/` 目录中运行。已存在的环境变量不会被 `.env` 覆盖。
- API 默认监听 `:8080`。
- 首次启动时建表，并写入网站配置、管理员 `admin`、12 个预设采集站和 4 条计划任务。
- 对外部署时须将 `JWT_SECRET` 替换为随机字符串，可通过 `openssl rand -hex 32` 生成。

### 在容器中运行后端

开发编排中的 `server` 服务使用 `server/Dockerfile` 从源码构建，仅包含 Go 后端，容器名为 `Eco-server`：

```bash
docker compose up -d --build server
```

- 在仓库根目录执行。`server` 未声明 `depends_on`，不会自动启动数据库，须先执行 `docker compose up -d mysql redis`。
- API 映射到本机 `18080`（`SERVER_PORT`），例如 `http://127.0.0.1:18080/api/config/basic`。
- 容器内使用服务名 `mysql`、`redis` 连接数据库，变量取自仓库根目录的 `.env`（不存在时使用编排中的默认值），不读取 `server/.env`。

## 环境变量

| 变量 | 默认值 | 必填 | 说明 |
| --- | --- | --- | --- |
| `PORT` | `8080` | 否 | API 监听端口。发布镜像里固定 `8080` |
| `JWT_SECRET` | 无 | 是 | 登录签名密钥。未设置时进程 panic |
| `ECOHUB_ADMIN_PASSWORD` | `admin` | 否 | 仅在首次创建 `admin` 时写入 |
| `MYSQL_HOST` | 无 | 是 | 本机运行时为 `127.0.0.1`，Compose 中为服务名 `mysql`，容器访问宿主机时为 `host.docker.internal` |
| `MYSQL_PORT` | 无 | 是 | 通常 `3306` |
| `MYSQL_USER` | 无 | 是 | |
| `MYSQL_PASSWORD` | 空 | 否 | |
| `MYSQL_DBNAME` | 无 | 是 | |
| `REDIS_HOST` | 无 | 是 | 本机运行时为 `127.0.0.1`，Compose 中为服务名 `redis` |
| `REDIS_PORT` | 无 | 是 | 通常 `6379` |
| `REDIS_PASSWORD` | 空 | 否 | |
| `REDIS_DB` | `0` | 否 | 非数字时忽略，使用 `0` |
| `ECOHUB_LOG_DIR` | 模块根目录下的 `logs/` | 否 | 运行日志目录。向上未找到 `go.mod` 时使用当前目录下的 `logs/` |
| `NODE_NAME` | 主机名 | 否 | 数据分析中记录的服务节点名。主机名超过 12 个字符时取最后 6 个字符。用于区分多实例部署 |
| `GIN_MODE` | `release` | 否 | 未设置时以 release 模式运行 |

以下必填项为空时进程直接 panic：`MYSQL_HOST`、`MYSQL_PORT`、`MYSQL_USER`、`MYSQL_DBNAME` 任一为空，`REDIS_HOST`、`REDIS_PORT` 任一为空，或未设置 `JWT_SECRET`。

`WEB_PORT`、`SERVER_PORT`、`MYSQL_ROOT_PASSWORD`、`ECOHUB_IMAGE` 仅供 Compose 使用，Go 进程不读取。

## 启动流程

1. 依次连接 Redis 和 MySQL。各重试 30 次，间隔 2 秒；仍无法连接时 panic 退出。
2. 建表并补齐表结构（`AutoMigrate`），然后按顺序执行 `internal/migration/` 中尚未执行的迁移。
3. 新库或不存在管理员时，写入 12 个预设采集站。
4. 初始化分类、管理员 `admin`、上传目录、网站配置、默认海报源、数据分析配置。
5. 注册计划任务，加载快照和内存中的列表数据，等待搜索索引构建完成。
6. 开始监听端口，并在后台预热主站首页缓存。

`admin` 已存在时不会修改其密码。其他账号在后台「账号管理」中创建。

## 接口

前台接口（后台开启「私有化访问」后需要登录或订阅密钥）：

| 方法 | 路径 |
| --- | --- |
| GET | `/api/home` |
| GET | `/api/daily-updates` |
| GET | `/api/categories` |
| GET | `/api/categories/overview` |
| GET | `/api/films/play-info` |
| GET | `/api/films/related` |
| GET | `/api/films/search` |
| GET | `/api/films/filter` |
| GET | `/api/search/hot-keywords` |
| POST | `/api/stats/view` |
| GET、POST | `/api/film/danmaku` |

订阅接口（开启「私有化访问」后须携带订阅密钥 `?key=` 或请求头 `X-Provide-Key`）：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/provide/app` | EcoHub 客户端 |
| GET | `/api/provide/tvbox` | TVBox / 影视仓 |
| GET | `/api/provide/vod` | MacCMS |

不受私有化访问限制的接口：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/config/basic` | 网站基本配置，也用作健康检查 |
| POST | `/api/login` | 登录 |
| POST | `/api/logout` | 退出登录，需已登录 |
| GET | `/api/upload/pic/poster/*name` | 上传的图片 |

后台接口均位于 `/api/manage/*` 下，完整列表见 `internal/router/router.go`。

### 鉴权

- 登录成功后，后端下发 HttpOnly cookie `ecohub_auth_token`，有效期 10 天。同时支持 `Authorization: Bearer <token>` 请求头。
- `/api/manage/*` 和 `/api/logout` 同时校验 JWT 和 Redis 中保存的当前 token。同一账号在其他位置登录后，旧 token 失效。JWT 过期后须重新登录。
- 访客账号仅能发送 GET 请求，写操作由 `WriteAccess` 拒绝并返回 `403`。
- 部分操作（网站配置、账号管理、升级等）仅限超级用户，由 `AdminAccess` 控制。

## 目录结构

```text
server/
├── cmd/server/                 # 入口 main.go
├── internal/access/            # 访问统计（数据分析）
├── internal/config/            # 环境变量、常量、采集并发档位
├── internal/handler/           # HTTP 处理
├── internal/infra/             # db（MySQL、Redis 连接）、syslog（运行日志）
├── internal/middleware/        # 鉴权、私有化访问、访问日志、CORS
├── internal/migration/         # 结构迁移：一条空基线，新变更只在末尾追加
├── internal/model/             # 表结构和数据模型
├── internal/repository/        # 数据访问；film/ 下分 cache、query、shared、snapshot、writer
├── internal/router/            # 路由
├── internal/service/           # 业务逻辑；upgrade/ 是后台「立即升级并重启」
├── internal/spider/            # 采集；子目录 converter、fetcher、progress、scheduler
├── internal/testutil/          # 测试辅助
├── internal/utils/             # 通用工具
└── test/integration/           # 跨模块集成测试
```

单元测试位于 `internal/` 各包中，跨模块测试位于 `test/integration/`。

## 常用命令

```bash
cd server
go run ./cmd/server
go build ./...
go test ./internal/...
go test ./test/...
go test ./...
```

本机 Go 缓存目录不可写时：

```bash
GOCACHE=/tmp/ecohub-go-cache go test ./...
```

## 相关文档

[总览](../README.md) · [前端说明](../web/README.md) · [部署指南](../docs/README-Deploy.md) · [常见问题](../docs/README-FAQ.md)
