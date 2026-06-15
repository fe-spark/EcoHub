# Docker 部署说明

本文只讲 Docker 部署。普通用户推荐使用“发布版部署”，不需要下载源码，也不需要分别启动前端和后端。

## 部署方式选择

| 场景 | 推荐方式 | 说明 |
| --- | --- | --- |
| 只想直接运行 EcoHub | 发布版部署 | 执行安装脚本生成部署文件和 `.env`，自动下载已发布镜像，内置 MySQL / Redis |
| 想从当前源码构建镜像 | 源码版部署 | 使用仓库根目录 `docker-compose.yml`，从 `web/` 和 `server/` 本地构建镜像 |
| 已有 MySQL / Redis | 外部数据库部署 | 修改 `.env` 连接信息，只启动 `server` 和 `web` |

## 前置条件

- Docker 20+
- Docker Compose 2+
- 服务器可以访问 GitHub 镜像仓库和 Docker Hub

## 发布版部署（推荐）

发布版使用 [deploy/release/compose.yml](./deploy/release/compose.yml)。安装脚本会把它下载成 `~/ecohub/docker-compose.yml`，默认启动四个容器：

| 容器 | 作用 | 镜像 |
| --- | --- | --- |
| `Eco-web` | 前台、播放页、登录页、管理后台 | `ghcr.io/fe-spark/ecohub-web:v1.0.0` |
| `Eco-server` | Go API、采集、缓存、鉴权、开放接口 | `ghcr.io/fe-spark/ecohub-server:v1.0.0` |
| `Eco-mysql` | 内置 MySQL 数据库 | `mysql:8.4` |
| `Eco-redis` | 内置 Redis 缓存 | `redis:7.4-alpine` |

### 1. 下载安装文件

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
```

安装脚本默认写入 `~/ecohub`，会生成 `docker-compose.yml`。如果 `.env` 不存在，脚本会从根目录 [.env.example](./.env.example) 复制一份同结构配置。

### 2. 修改配置

发布版和源码版使用同一套 `.env` 配置结构。正式部署前至少修改 `JWT_SECRET`、`MYSQL_ROOT_PASSWORD`、`MYSQL_PASSWORD` 和 `REDIS_PASSWORD`。

生成 `JWT_SECRET`：

```bash
openssl rand -hex 32
```

### 3. 启动

```bash
docker compose up -d
```

默认访问：

- 前台：`http://你的服务器:3000`
- 后台：`http://你的服务器:3000/manage`
- API：`http://你的服务器:3000/api/*`
- 后端直连接口：`http://你的服务器:18080/api/*`
- TVBox / 影视仓配置：`http://你的服务器:3000/api/provide/config`

### 4. 数据目录

发布版默认把数据放在安装目录下：

```text
~/ecohub/data/mysql
~/ecohub/data/redis
~/ecohub/data/uploads
```

不要随意删除这些目录。删除后数据库、缓存和上传图片会丢失。

### 5. 更新

```bash
cd ~/ecohub
docker compose pull
docker compose up -d
```

升级到新版本时，执行上面的命令重新拉取镜像并重建容器即可。

## 源码版部署

源码版适合开发者或需要自己构建镜像的场景。它使用仓库根目录的 [docker-compose.yml](./docker-compose.yml)。

### 1. 准备配置

```bash
cp .env.example .env
```

正式部署前至少修改：

- `JWT_SECRET`
- `MYSQL_ROOT_PASSWORD`
- `MYSQL_PASSWORD`
- `REDIS_PASSWORD`

### 2. 使用内置 MySQL / Redis

```bash
docker compose up --build -d mysql redis server web
```

默认访问：

- 前台：`http://你的服务器:3000`
- 后台：`http://你的服务器:3000/manage`
- API：`http://你的服务器:3000/api/*`
- TVBox / 影视仓配置：`http://你的服务器:3000/api/provide/config`

### 3. 连接外部 MySQL / Redis

如果你已经有数据库和 Redis，修改根目录 `.env`：

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

只启动应用服务：

```bash
docker compose up --build -d server web
```

地址填写建议：

- 数据库在 Docker 宿主机：使用 `host.docker.internal`。
- 数据库在其他机器：填写真实 IP、域名或内网地址。
- Redis 无密码：`REDIS_PASSWORD` 留空字符串。

## 常用命令

```bash
docker compose ps
docker compose logs -f web
docker compose logs -f server
docker compose logs -f mysql
docker compose logs -f redis
docker compose restart web
docker compose restart server
docker compose down
```

删除容器但保留数据：

```bash
docker compose down
```

删除容器和源码版默认数据卷：

```bash
docker compose down -v
```

发布版默认使用安装目录下的 `data/` 持久化数据；源码版默认使用 Docker volume。

## 端口说明

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `WEB_PUBLIC_PORT` | `3000` | 前台和后台入口端口 |
| `SERVER_PUBLIC_PORT` | `18080` | 后端直连接口端口 |
| `SERVER_PORT` | `8080` | 后端容器内部监听端口，只供 Web 容器通过内网访问 |

后端接口地址本身就以 `/api` 开头，所以访问 `SERVER_PUBLIC_PORT` 直连后端时也要带 `/api/...`，例如 `http://你的服务器:18080/api/health`。

浏览器访问 Web 端口下的 `/api/*` 时，请求会先到前端容器，再转发到后端容器。`SERVER_PUBLIC_PORT` 只用于需要直接调后端接口的场景。

## 反向代理建议

生产环境建议只对外开放 Web 端口，并由 Nginx、Caddy 或其他反向代理处理 HTTPS：

```text
https://your-domain.com        -> web:3000
https://your-domain.com/api/*  -> web:3000/api/* -> server:8080
```

不建议把后端接口直接暴露到公网；对外统一访问 Web 域名下的 `/api/*`。

## 健康检查与排障

- `server` 健康检查：`/api/health`
- `web` 会等待 `server` 健康后启动
- `server` 启动时会连接 MySQL 和 Redis，连接失败会退出或保持不健康

排查启动问题时优先查看：

```bash
docker compose logs -f server
docker compose logs -f web
```

如果容器反复重启，重点检查：

- `.env` 中数据库和 Redis 密码是否一致
- `JWT_SECRET` 是否已配置
- `WEB_PUBLIC_PORT` 是否被宿主机其他服务占用
- 服务器是否可以下载 GitHub 镜像仓库和 Docker Hub 镜像

## 安全建议

- 部署后立即修改默认账号 `admin / admin`、`guest / guest`。
- `JWT_SECRET` 每个环境单独生成。
- 不要把真实生产密码提交到仓库。
- 优先通过 HTTPS 暴露前端入口。
- 不建议直接把 MySQL、Redis 或后端接口暴露到公网。

## 相关文档

- [根目录总览](./README.md)
- [服务端说明](./server/README.md)
- [前端说明](./web/README.md)
- [FAQ 与排障](./README-FAQ.md)
