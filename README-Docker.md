# Docker 部署说明

本文只讲 Docker 部署。普通用户推荐「发布版部署」：不需要下载源码，也不需要分别启动前端和后端。

> **v2.0+**：发布镜像为 **All-in-One 单镜像** `ghcr.io/fe-spark/ecohub`（同容器内 Supervisord 托管 Go API `:8080` 与 Next.js `:3000`）。旧的 `ecohub-web` / `ecohub-server` 双镜像已废弃。

## 部署方式选择

| 场景 | 推荐方式 | 说明 |
| --- | --- | --- |
| 只想直接运行 EcoHub | 发布版部署 | 安装脚本生成 compose + `.env`，拉取已发布镜像，内置 MySQL / Redis |
| 想从当前源码构建 | 源码版部署 | 仓库根目录 `docker-compose.yml` 本地构建（开发仍可拆成 web/server 两服务） |
| 已有 MySQL / Redis | 外部数据库部署 | 改 `.env` 连接信息，不启内置 mysql/redis |

## 前置条件

- Docker 20+
- Docker Compose 2+
- 服务器可访问 GHCR（`ghcr.io`）与 Docker Hub

## 发布版部署（推荐）

发布版使用 [deploy/release/compose.yml](./deploy/release/compose.yml)。安装脚本会下载为 `~/ecohub/docker-compose.yml`，默认三个服务：

| 容器 | 作用 | 镜像 |
| --- | --- | --- |
| `Eco-hub` | 前台、后台、API、采集、鉴权、开放接口（All-in-One） | `ghcr.io/fe-spark/ecohub:latest` |
| `Eco-mysql` | 内置 MySQL | `mysql:8.4` |
| `Eco-redis` | 内置 Redis | `redis:7.4-alpine` |

### 1. 下载安装文件

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
```

默认写入 `~/ecohub`，生成 `docker-compose.yml`。若无 `.env`，会从仓库 [.env.example](./.env.example) 复制一份。

### 2. 修改配置

正式部署前至少修改：

- `JWT_SECRET`
- `MYSQL_ROOT_PASSWORD` / `MYSQL_PASSWORD`
- `REDIS_PASSWORD`

生成 `JWT_SECRET`：

```bash
openssl rand -hex 32
```

可选环境变量（见 `.env.example`）：

- `TG_PROXY`：Telegram 专用代理（国内访问 `api.telegram.org` 常用）
- `HTTPS_PROXY` / `HTTP_PROXY` / `ALL_PROXY`：采集等通用代理
- `COLLECT_PROFILE`：采集档位 `auto|light|standard|high`（默认 `auto` 按 CPU 核数）

### 3. 启动

```bash
cd ~/ecohub
docker compose up -d
```

默认访问：

- 前台：`http://你的服务器:3000`
- 后台：`http://你的服务器:3000/manage`
- API（经站点）：`http://你的服务器:3000/api/*`
- 后端直连：`http://你的服务器:18080/api/*`
- TVBox / 影视仓：`http://你的服务器:3000/api/provide/config`

### 4. 数据目录

```text
~/ecohub/data/mysql
~/ecohub/data/redis
~/ecohub/data/uploads
```

不要随意删除；删除后数据库、缓存与上传文件会丢失。

### 5. 更新

```bash
cd ~/ecohub
docker compose pull
docker compose up -d
```

固定版本可在 compose 中把镜像改为例如：

```yaml
image: ghcr.io/fe-spark/ecohub:v2.0.1
```

正式版 tag（无 `-beta` / `-rc`）会同步覆盖 `:latest`。变更说明见 [RELEASE.md](./RELEASE.md)。

### 6. 从 v1.x 双镜像升级

1. 备份 `data/`（或旧 volume）。
2. 用安装脚本或手动替换为当前 [deploy/release/compose.yml](./deploy/release/compose.yml)（单服务 `ecohub`）。
3. 确认 `.env` 中 JWT / MySQL / Redis 与旧库一致。
4. `docker compose pull && docker compose up -d`。

主站 `ContentKey` 等数据迁移由 server 启动时自动处理；禁止新旧版本混连同一库。

## 源码版部署

适合开发或自行构建镜像。使用仓库根目录 [docker-compose.yml](./docker-compose.yml)（仍为 `web` + `server` 两服务本地 build，便于热改；生产发布请用 All-in-One）。

### 1. 准备配置

```bash
cp .env.example .env
```

至少修改 `JWT_SECRET`、`MYSQL_*` 密码、`REDIS_PASSWORD`。

### 2. 使用内置 MySQL / Redis

```bash
docker compose up --build -d
```

访问方式与发布版相同（`3000` / `18080`）。

### 3. 连接外部 MySQL / Redis

修改根目录 `.env`：

```env
MYSQL_HOST=host.docker.internal
MYSQL_PORT=3306
MYSQL_USER=your_mysql_user
MYSQL_PASSWORD=your_mysql_password
MYSQL_DBNAME=your_mysql_db

REDIS_HOST=host.docker.internal
REDIS_PORT=6379
REDIS_PASSWORD=your_redis_password
REDIS_DB=0
```

只启动应用（发布版改 compose 去掉 mysql/redis 依赖；源码版可只起 `server` `web`）：

```bash
# 源码版示例
docker compose up --build -d server web
```

地址建议：

- 库在宿主机：`host.docker.internal`（Linux 需 compose `extra_hosts`，仓库已配）
- 库在其他机器：真实 IP / 域名
- Redis 无密码：`REDIS_PASSWORD` 留空

## 常用命令

**发布版（单应用容器 `ecohub` / `Eco-hub`）：**

```bash
docker compose ps
docker compose logs -f ecohub
docker compose logs -f mysql
docker compose logs -f redis
docker compose restart ecohub
docker compose down
```

**源码版（拆分服务）：**

```bash
docker compose logs -f web
docker compose logs -f server
docker compose restart web
docker compose restart server
```

删除容器但保留数据：

```bash
docker compose down
```

源码版删除默认 volume：

```bash
docker compose down -v
```

发布版数据在安装目录 `data/`；源码版默认 Docker volume。

## 端口说明

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `WEB_PUBLIC_PORT` | `3000` | 前台与后台入口 |
| `SERVER_PUBLIC_PORT` | `18080` | 后端直连接口（映射容器内 `SERVER_PORT`） |
| `SERVER_PORT` | `8080` | 容器内 Go API 监听端口 |

后端路径本身以 `/api` 开头，直连示例：`http://你的服务器:18080/api/health`。

浏览器访问 Web 端口下的 `/api/*` 时，由 Next 转发到同容器（发布版）或 `server` 服务（源码版）的 Go API。生产环境建议只暴露 Web 端口。

## 反向代理建议

```text
https://your-domain.com        -> :3000
https://your-domain.com/api/*  -> :3000/api/* -> 内部 :8080
```

不建议把 MySQL、Redis 或 `SERVER_PUBLIC_PORT` 直接暴露公网。

## 健康检查与排障

- 应用健康：`GET /api/health`（容器内 `8080`）
- 发布版 `ecohub` 依赖 mysql/redis healthy 后再起
- 启动连不上 MySQL/Redis 会不健康或退出

```bash
# 发布版
docker compose logs -f ecohub
docker compose logs -f mysql
docker compose logs -f redis
```

反复重启时重点查：

- `.env` 中数据库 / Redis 密码是否与服务一致
- `JWT_SECRET` 是否已改
- `WEB_PUBLIC_PORT` 是否被占用
- 是否能拉取 `ghcr.io/fe-spark/ecohub` 与基础镜像

## 安全建议

- 部署后立即修改默认账号 `admin / admin`、`guest / guest`
- 每个环境单独生成 `JWT_SECRET`
- 不要把生产密码提交进仓库
- 优先 HTTPS 暴露前端入口
- 不建议公网暴露 MySQL、Redis 或后端直连端口

## 相关文档

- [根目录总览](./README.md)
- [版本变更](./RELEASE.md)
- [服务端说明](./server/README.md)
- [前端说明](./web/README.md)
- [FAQ 与排障](./README-FAQ.md)
