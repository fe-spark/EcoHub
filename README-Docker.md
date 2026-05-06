# Docker 部署说明

仓库根目录提供 `docker-compose.yml`，可用于部署 EcoHub 的前端、后端、MySQL 和 Redis。

## 服务组成

| 服务 | 说明 | 默认镜像 / 构建 |
| --- | --- | --- |
| `web` | Next.js 前端，对外入口 | `./web/Dockerfile` |
| `server` | Go API 服务 | `./server/Dockerfile` |
| `mysql` | 可选 MySQL 8.4 | `mysql:8.4` |
| `redis` | 可选 Redis 7.4 | `redis:7.4-alpine` |

## 前置条件

- Docker 20+
- Docker Compose 2+
- 能拉取基础镜像

## 默认约定

- `web` 对外暴露 `3000`。
- `server` 默认监听 `8080`，对外映射为 `18080`，同时供 `web` 通过容器网络访问。
- 内置 `mysql` / `redis` 默认不向宿主机暴露端口；只在 Compose 网络内供 `server` 访问。
- `web` 的 `API_URL` 默认为 `http://server:${SERVER_PORT:-8080}`。
- 浏览器端访问当前站点 `/api/*`，由 Next 转发到 `server`。
- Compose 会自动读取根目录 `.env`，不读取 `server/.env` 或 `web/.env.local`。
- `mysql` 和 `redis` 已配置 Docker volume：`eco-mysql-data`、`eco-redis-data`。

## 配置入口

先复制根目录配置模板：

```bash
cp .env.example .env
```

正式部署前至少修改 `.env` 中这些值：

- `JWT_SECRET`
- MySQL root 密码、业务库用户和密码
- Redis 密码

对外端口只需要填写宿主机端口号；`SERVER_PORT` 是后端容器内部监听端口，`web` 的 `API_URL` 会跟随它：

```env
WEB_PUBLIC_PORT=3000
SERVER_PUBLIC_PORT=18080
SERVER_PORT=8080
```

内置 MySQL / Redis 默认不对宿主机暴露端口。如果需要直连调试，请临时在 `docker-compose.yml` 的对应服务中添加 `ports`，并优先绑定 `127.0.0.1`。

生成 `JWT_SECRET`：

```bash
openssl rand -hex 32
```

不要把真实生产密码提交到仓库。

## 场景一：使用 Compose 内置 MySQL / Redis

适合服务器上还没有数据库和缓存的情况。

### 1. 确认配置

`.env` 中应保持容器服务名：

```env
MYSQL_HOST=mysql
REDIS_HOST=redis
```

如果修改数据库或 Redis 密码，只改根目录 `.env`。

### 2. 启动全部服务

```bash
docker compose up --build -d mysql redis server web
```

### 3. 访问

- 前台：`http://你的服务器:3000`
- 后台：`http://你的服务器:3000/manage`
- API：`http://你的服务器:3000/api/*`
- 直连后端：`http://你的服务器:18080`
- TVBox / 影视仓配置：`http://你的服务器:3000/api/provide/config`

如果宿主机已有服务占用 `3000` 或 `18080`，修改 `.env` 中对应的 `*_PUBLIC_PORT`。MySQL / Redis 默认不发布端口，不会占用宿主机 `3306` / `6379`。

## 场景二：连接外部 MySQL / Redis

适合已有宿主机数据库、远程数据库、云数据库或独立 Redis 的情况。

### 1. 修改后端连接信息

在根目录 `.env` 中修改：

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

地址填写建议：

- 数据库在 Docker 宿主机：优先使用 `host.docker.internal`。
- 数据库在其他机器：填写真实 IP、域名或内网地址。
- Redis 无密码：`REDIS_PASSWORD` 留空字符串。

### 2. 只启动应用服务

```bash
docker compose up --build -d server web
```

这种方式不会启动 Compose 内置的 `mysql` 和 `redis`。

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
docker compose down -v
```

`docker compose down -v` 会删除 MySQL / Redis volume，数据会丢失。

## 持久化建议

内置 MySQL / Redis 已配置 volume：

- `eco-mysql-data`
- `eco-redis-data`

海报和图库文件默认在 API 容器内：

```text
/app/static/upload/gallery
```

生产环境建议挂载到宿主机：

```yaml
services:
  server:
    volumes:
      - /path/to/gallery:/app/static/upload/gallery
```

## 反向代理建议

生产环境建议只暴露 `web`，由反向代理统一处理 HTTPS 和域名：

```text
https://your-domain.com        -> web:3000
https://your-domain.com/api/*  -> web:3000/api/* -> server:${SERVER_PORT:-8080}
```

如果不需要外部直连后端，可以移除或限制 `server` 的 `SERVER_PUBLIC_PORT:SERVER_PORT` 端口映射。

## 健康检查

- `server` 健康检查：`/api/health`
- `web` 依赖 `server` 健康后启动
- `server` 启动时由应用自身连接 MySQL 和 Redis，数据库不可达时会退出或保持不健康

排查启动问题时优先查看：

```bash
docker compose logs -f server
docker compose logs -f web
```

## 安全建议

- 部署后立即修改默认账号 `admin / admin`、`guest / guest`。
- `JWT_SECRET` 必须每个环境单独生成。
- 不要在公开仓库中保留生产数据库密码和 Redis 密码。
- 优先通过 HTTPS 暴露前端入口。
- 不建议直接把数据库、Redis 或后端 API 暴露到公网。

## 相关文档

- [根目录总览](./README.md)
- [服务端说明](./server/README.md)
- [前端说明](./web/README.md)
- [FAQ 与排障](./README-FAQ.md)
