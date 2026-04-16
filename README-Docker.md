# 服务器部署说明

当前仓库提供的 `docker-compose.yml` 可用于服务器部署，包含这些服务：

- `web`：Next.js 前端
- `server`：Go API 服务
- `mysql`：可选的 MySQL 服务
- `redis`：可选的 Redis 服务

是否启用 `mysql` 和 `redis`，取决于你是否准备使用 Compose 内置数据库与缓存，还是接入外部服务。

## 前置条件

- Docker 20+
- Docker Compose 2+
- Docker 镜像拉取能力

## 1. 准备本地运行配置

如果你还需要单独进入目录运行服务，可以先复制示例文件：

```bash
cp server/.env.example server/.env
```

其中 `server/.env` 只用于你在 `server/` 目录本地执行 `go run ./cmd/server` 时自动加载，Docker Compose 运行不会读取它。

至少需要确认 `server/.env` 中这些字段：

- `PORT`
- `JWT_SECRET`
- `MYSQL_HOST`
- `MYSQL_PORT`
- `MYSQL_USER`
- `MYSQL_PASSWORD`
- `MYSQL_DBNAME`
- `REDIS_HOST`
- `REDIS_PORT`
- `REDIS_PASSWORD`
- `REDIS_DB`

推荐先为 `JWT_SECRET` 生成一个随机高强度值，例如：

```bash
openssl rand -hex 32
```

生成后写入 `server/.env`：

```env
JWT_SECRET=your_generated_secret
```

Compose 运行时的环境变量直接写在 `docker-compose.yml` 中：

- `server` 运行在固定端口 `8080`
- `server` 会直接收到占位用 `JWT_SECRET`，部署前必须替换成你自己的随机密钥
- `web` 构建期和运行期都会收到 `API_URL=http://server:8080`
- `server` 默认连接 Compose 内的 `mysql` 和 `redis`
- Compose 运行不依赖 `server/.env`、`web/.env.production` 或 `--env-file`

## 2. 启动服务

```bash
docker compose up --build -d
```

对外访问入口以你的 Compose 发布方式或反向代理配置为准，后台路径仍然是 `/manage`。

### 只启动应用，不启动 Compose 内的 MySQL / Redis

如果你的服务器已经有现成的 MySQL / Redis，或者你准备接入外部数据库与缓存，可以只启动 `server` 与 `web`。

先修改根目录 `docker-compose.yml` 中 `server.environment` 里的这些字段，把它们替换成你的真实连接信息：

```env
PORT=8080
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

说明：

- 如果 MySQL / Redis 运行在 Docker 宿主机上，优先使用 `host.docker.internal`
- 如果运行在其他服务器或云数据库，请直接填写实际 IP、域名或内网地址
- 如果 MySQL / Redis 运行在别的 Docker 网络里，请填写对应容器名或服务名，并确保网络互通
- Compose 中 `web` 的 `API_URL` 默认保持为 `http://server:8080`

启动命令改为只拉起应用服务：

```bash
docker compose up --build -d server web
```

这样：

- `server` 会按你写在 `docker-compose.yml` 里的值连接外部 MySQL / Redis
- 浏览器端和 SSR 都会先请求当前站点下的 `/api/*`，再由 Next rewrite 转发到 `API_URL`
- Compose 内的 `mysql` / `redis` 不会启动，也不会占用 `3306` / `6379`

### 启动 Compose 内的 MySQL / Redis

如果你希望直接使用仓库内置的 MySQL 与 Redis，需要显式把它们也加入启动命令：

```bash
docker compose up --build -d mysql redis server web
```

这种模式下请确认：

- `docker-compose.yml` 中 `server.environment.MYSQL_HOST=mysql`
- `docker-compose.yml` 中 `server.environment.REDIS_HOST=redis`
- `docker-compose.yml` 里的数据库和 Redis 凭据保持一致

## 3. Compose 当前行为

### Web 服务

- 构建期和运行期都由 Compose 直接注入 `API_URL=http://server:8080`
- 浏览器端和 SSR 都先请求当前站点下的 `/api/*`
- Next 会通过 rewrite 将 `/api/*` 转发到 `API_URL`
- `API_URL` 是前端唯一需要理解的后端地址配置
- 如果你修改了服务名、网络结构或 API 对内地址，需要同步调整 `docker-compose.yml`、`server/Dockerfile` 和 `web/Dockerfile`

### API 服务

- 运行时环境由 Compose 直接注入
- 使用 `server/Dockerfile` 和 `./server` 作为独立构建上下文
- Compose 固定使用 `8080:8080` 做端口映射
- 内置健康检查：`GET /api/index`

### MySQL / Redis 服务

- `mysql` 使用 `mysql:8.4`
- `redis` 使用 `redis:7.4-alpine`
- 两者都挂载了 Docker volume，便于持久化数据
- 如果你使用外部 MySQL / Redis，可以只启动 `server` 与 `web`
- 如果你使用 Compose 内的 `mysql` / `redis`，请显式执行包含 `mysql redis server web` 的启动命令
- 如需清理 Compose 内数据库与缓存数据，可执行 `docker compose down -v`

## 常用命令

```bash
docker compose ps
docker compose build server
docker compose logs -f web
docker compose logs -f server
docker compose restart web
docker compose restart server
docker compose down
```

## 持久化建议

海报和图库文件默认存放在 API 容器内部的：

```text
/app/static/upload/gallery
```

生产环境建议挂载卷，例如：

```yaml
services:
  server:
    volumes:
      - /path/to/gallery:/app/static/upload/gallery
```

## 网络说明

当前 `server` 服务包含：

```yaml
extra_hosts:
  - "host.docker.internal:host-gateway"
```

这主要用于容器访问宿主机上的 MySQL / Redis。

如果你的数据库和 Redis 不在宿主机，而是在其他机器或容器网络中：

- 本地直跑 `server` 时，请把 `server/.env` 里的 `MYSQL_HOST` 和 `REDIS_HOST` 改成实际地址
- Compose 部署时，请把 `docker-compose.yml` 中 `server.environment` 的 `MYSQL_HOST` 和 `REDIS_HOST` 改成实际地址

## 生产建议

- 修改或禁用默认账号：管理员 `admin` / `admin`，访客 `guest` / `guest`
- 为 `JWT_SECRET` 使用单独的高强度值，可通过 `openssl rand -hex 32` 生成
- 优先通过反向代理暴露统一入口，而不是直接暴露 API 服务
- 为前端站点启用 HTTPS
- 不要把真实生产密码直接提交到仓库；如果需要长期维护，建议把 `docker-compose.yml` 中的敏感值迁移到安全的部署系统或密钥管理方案
- 当前仓库里的 `JWT_SECRET` 仅是占位值，正式部署前必须替换

## 常见问题

- MySQL 或 Redis 启动失败
  - 检查本机 `3306` / `6379` 端口是否已被占用
  - 执行 `docker compose logs -f mysql`
  - 执行 `docker compose logs -f redis`
- 已有外部 MySQL / Redis，不想启动 Compose 内服务
  - 把 `docker-compose.yml` 中 `server.environment` 的 `MYSQL_HOST` / `REDIS_HOST` 改成你的实际地址
  - 启动时使用 `docker compose up --build -d server web`
- 想启用 Compose 内 MySQL / Redis
  - 确认 `docker-compose.yml` 中 `server.environment` 的 `MYSQL_HOST` / `REDIS_HOST` 为 `mysql` / `redis`
  - 启动时使用 `docker compose up --build -d mysql redis server web`
- 前端打开后接口全部失败
  - 检查 `docker-compose.yml` 中 `web` 的 `API_URL` 是否仍然是 `http://server:8080`
  - 检查 `server` 容器是否已经启动并通过健康检查
