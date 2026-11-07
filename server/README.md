# Server

`server/` 是 EcoHub 的 Go API：采集、片库、列表、缓存、开放接口和后台鉴权。

采集、主站、匹配、刮削和缓存行为见 [常见问题](../docs/README-FAQ.md)。计划任务冲突见 [计划任务运行冲突](../docs/cron-task-conflict.md)。部署变量见 [部署指南](../docs/README-Deploy.md)。

## 本地启动

要求 Go 1.27+、MySQL 8+、Redis 7+。

```bash
cd server
cp .env.example .env
go run ./cmd/server
```

```env
PORT=8080
JWT_SECRET=change_me_to_a_long_random_string
MYSQL_HOST=127.0.0.1
MYSQL_PORT=3306
MYSQL_USER=eco
MYSQL_PASSWORD=your_mysql_password
MYSQL_DBNAME=eco
REDIS_HOST=127.0.0.1
REDIS_PORT=6379
REDIS_PASSWORD=your_redis_password
REDIS_DB=0
```

`JWT_SECRET` 用 `openssl rand -hex 32` 生成。API 默认 `:8080`。MySQL 或 Redis 不可达会直接退出。首次启动会建表，并写入基础配置、管理员、默认站点和定时任务。

进程读 `server/.env`。发布版容器读编排注入的环境变量，宿主机 `SERVER_PORT` 映射到容器内 `8080`。

## 环境变量

| 变量 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `PORT` | 否 | `8080` | API 端口。发布版容器内固定 8080 |
| `JWT_SECRET` | 是 | 无 | 未设置会 panic |
| `ECOHUB_ADMIN_PASSWORD` | 否 | `admin` | 仅首次创建 `admin` 时写入 |
| `MYSQL_HOST` | 是 | 无 | 本机 `127.0.0.1`，Compose 服务名 `mysql`，容器访问宿主机 `host.docker.internal` |
| `MYSQL_PORT` | 是 | 无 | 通常 `3306` |
| `MYSQL_USER` | 是 | 无 | |
| `MYSQL_PASSWORD` | 否 | 空 | |
| `MYSQL_DBNAME` | 是 | 无 | |
| `REDIS_HOST` | 是 | 无 | 本机 `127.0.0.1`，Compose 服务名 `redis` |
| `REDIS_PORT` | 是 | 无 | 通常 `6379` |
| `REDIS_PASSWORD` | 否 | 空 | |
| `REDIS_DB` | 否 | `0` | 非数字时忽略 |
| `ECOHUB_LOG_DIR` | 否 | 模块根 `logs/` | 找不到 `go.mod` 时回退到进程工作目录 |

`MYSQL_HOST`、`MYSQL_PORT`、`MYSQL_USER`、`MYSQL_DBNAME` 任一为空会 panic。`REDIS_HOST`、`REDIS_PORT` 为空也会 panic。

`WEB_PORT`、`SERVER_PORT`、`MYSQL_ROOT_PASSWORD` 只给 Compose 用，server 进程不读。

## 启动时做什么

等待 MySQL 和 Redis，执行 `AutoMigrate`，清自身缓存，初始化分类、管理员、默认站点、默认采集源和定时任务，加载读模型，启动 cron。

初始化只创建超级管理员 `admin`。已有管理员时不改密码。普通用户和访客在账号管理里新建。

## 接口

公共：

- `/api/home`
- `/api/daily-updates`
- `/api/categories`
- `/api/categories/overview`
- `/api/films/play-info`
- `/api/films/related`
- `/api/films/search`
- `/api/films/filter`
- `/api/search/hot-keywords`
- `/api/stats/view`
- `/api/film/danmaku`（GET / POST）
- `/api/config/basic`
- `/api/provide/app`
- `/api/provide/tvbox`
- `/api/provide/vod`
- `/api/upload/pic/poster/*name`

登录：`POST /api/login`、`POST /api/logout`。后台：`/api/manage/*`。

登录态是 HttpOnly cookie `ecohub_auth_token`。`/api/manage/*` 和 `/api/logout` 校验 JWT 和 Redis 里的当前 token。JWT 过期但 Redis token 仍有效时会刷新 cookie。访客可以 GET，写操作由 `WriteAccess` 拒绝。

## 目录

```text
server/
├── cmd/server/
├── internal/config/
├── internal/router/
├── internal/middleware/
├── internal/handler/
├── internal/service/
├── internal/repository/film/   # shared cache query snapshot writer
├── internal/model/
├── internal/spider/            # converter fetcher progress scheduler
├── internal/migration/         # 空基线，新变更只在末尾追加
├── internal/infra/             # db syslog
├── internal/utils/
└── test/integration/
```

`internal/` 放单元测试。跨模块测试放 `test/integration/`。

## 命令

```bash
cd server
go run ./cmd/server
go test ./internal/...
go test ./test/...
go test ./...
```

本机 Go 缓存目录受限时：

```bash
GOCACHE=/tmp/ecohub-go-cache go test ./...
```

## 相关

[总览](../README.md) · [前端](../web/README.md) · [部署](../docs/README-Deploy.md) · [常见问题](../docs/README-FAQ.md)
