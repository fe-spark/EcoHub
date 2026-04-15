# 服务器部署说明

当前仓库提供的 `docker-compose.yml` 可用于服务器部署，包含这些服务：

- `web`：Next.js 前端
- `server`：Go API 服务
- `mysql`：可选的 MySQL 服务
- `redis`：可选的 Redis 服务

是否启用 `mysql` 和 `redis`，取决于你的 `server/.env` 是否指向 Compose 内服务，或指向外部数据库与缓存。

## 前置条件

- Docker 20+
- Docker Compose 2+
- Docker 镜像拉取能力

## 1. 准备环境变量

分别复制服务端和前端的示例文件：

```bash
cp server/.env.example server/.env
cp web/.env.example web/.env.production
```

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

> 不要在生产环境把 `ENV` 设为 `dev`，也不要把 `IS_DEV_MODE` 设为 `true`。开发模式会在服务启动时清空 Redis，并重置数据库。

推荐先为 `JWT_SECRET` 生成一个随机高强度值，例如：

```bash
openssl rand -hex 32
```

生成后写入 `server/.env`：

```env
JWT_SECRET=your_generated_secret
```

如果你使用 Compose 内的数据库与缓存，推荐约定是：

- `MYSQL_HOST=mysql`
- `REDIS_HOST=redis`
- `server/.env` 中的数据库和 Redis 密码需要与 `docker-compose.yml` 保持一致

`web/.env.production` 至少需要配置：

- `API_URL`

Docker 默认推荐写成容器网络内地址：

```env
API_URL=http://server:8080
```

其中：

- `server:8080` 里的 `8080` 应与 `server/.env` 中的 `PORT` 保持一致
- `API_URL` 是 Next 容器访问后端的地址，Docker 默认推荐写 `http://server:8080`
- `web` 镜像构建阶段会显式把这个地址注入 `next build`，避免误用本地开发环境变量
- Compose 需要通过 `--env-file server/.env` 才能让 `ports` 读取到后端 `PORT`

## 2. 启动服务

```bash
docker compose --env-file server/.env up --build -d
```

对外访问入口以你的 Compose 发布方式或反向代理配置为准，后台路径仍然是 `/manage`。

### 只启动应用，不启动 Compose 内的 MySQL / Redis

如果你的服务器已经有现成的 MySQL / Redis，或者你准备接入外部数据库与缓存，可以只启动 `server` 与 `web`。

先修改 `server/.env`，把下面这些字段改成你的真实连接信息：

```env
PORT=8080
JWT_SECRET=change_me_to_a_long_random_string

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
- `web/.env.production` 中的 `API_URL` 推荐保持为 `http://server:8080`

启动命令改为只拉起应用服务：

```bash
docker compose --env-file server/.env up --build -d server web
```

这样：

- `server` 会按 `server/.env` 连接外部 MySQL / Redis
- 浏览器端和 SSR 都会先请求当前站点下的 `/api/*`，再由 Next rewrite 转发到 `API_URL`
- Compose 内的 `mysql` / `redis` 不会启动，也不会占用 `3306` / `6379`

### 启动 Compose 内的 MySQL / Redis

如果你希望直接使用仓库内置的 MySQL 与 Redis，需要显式把它们也加入启动命令：

```bash
docker compose --env-file server/.env up --build -d mysql redis server web
```

这种模式下请确认：

- `server/.env` 中的 `MYSQL_HOST=mysql`
- `server/.env` 中的 `REDIS_HOST=redis`
- `server/.env` 里的数据库和 Redis 凭据与 `docker-compose.yml` 保持一致

## 3. Compose 当前行为

### Web 服务

- 构建期和运行期都使用 `web/.env.production`
- 浏览器端和 SSR 都先请求当前站点下的 `/api/*`
- Next 会通过 rewrite 将 `/api/*` 转发到 `API_URL`
- `API_URL` 是前端唯一需要理解的后端地址配置
- 如果你修改了服务名、网络结构或 API 对内地址，需要同步调整 `docker-compose.yml`、`server/Dockerfile` 和 `web/Dockerfile`

### API 服务

- 读取 `server/.env`
- 使用 `server/Dockerfile` 和 `./server` 作为独立构建上下文
- Compose 默认使用 `${PORT:-8080}:${PORT:-8080}` 做端口映射
- 内置健康检查：`GET /api/index`

### MySQL / Redis 服务

- `mysql` 使用 `mysql:8.4`
- `redis` 使用 `redis:7.4-alpine`
- 两者都挂载了 Docker volume，便于持久化数据
- 如果你使用外部 MySQL / Redis，可以只启动 `server` 与 `web`
- 如果你使用 Compose 内的 `mysql` / `redis`，请显式执行包含 `mysql redis server web` 的启动命令
- 如需清理 Compose 内数据库与缓存数据，可执行 `docker compose --env-file server/.env down -v`

## 常用命令

```bash
docker compose --env-file server/.env ps
docker compose --env-file server/.env build server
docker compose --env-file server/.env logs -f web
docker compose --env-file server/.env logs -f server
docker compose --env-file server/.env restart web
docker compose --env-file server/.env restart server
docker compose --env-file server/.env down
```

如果只是执行 `docker compose down`，现在即使不额外传 `--env-file server/.env` 也能正常工作；但为了保持命令一致性，文档仍统一示例为带 `--env-file` 的写法。

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

如果你的数据库和 Redis 不在宿主机，而是在其他机器或容器网络中，请把 `server/.env` 里的 `MYSQL_HOST` 和 `REDIS_HOST` 改成实际地址。

## 生产建议

- 修改或禁用默认账号：管理员 `admin` / `admin`，访客 `guest` / `guest`
- 为 `JWT_SECRET` 使用单独的高强度值，可通过 `openssl rand -hex 32` 生成
- 优先通过反向代理暴露统一入口，而不是直接暴露 API 服务
- 为前端站点启用 HTTPS
- 将 `server/.env` 和 `web/.env.production` 纳入你的部署密钥管理，而不是提交到仓库

## 常见问题

- MySQL 或 Redis 启动失败
  - 检查本机 `3306` / `6379` 端口是否已被占用
  - 执行 `docker compose --env-file server/.env logs -f mysql`
  - 执行 `docker compose --env-file server/.env logs -f redis`
- 已有外部 MySQL / Redis，不想启动 Compose 内服务
  - 把 `server/.env` 中的 `MYSQL_HOST` / `REDIS_HOST` 改成你的实际地址
  - 启动时使用 `docker compose --env-file server/.env up --build -d server web`
- 想启用 Compose 内 MySQL / Redis
  - 把 `server/.env` 中的 `MYSQL_HOST` / `REDIS_HOST` 设为 `mysql` / `redis`
  - 启动时使用 `docker compose --env-file server/.env up --build -d mysql redis server web`
- 前端打开后接口全部失败
  - 检查 `web/.env.production` 中的 `API_URL` 是否仍然是 `http://server:8080`
  - 检查 `server` 容器是否已经启动并通过健康检查
- API 服务一启动就清空数据
  - 检查 `server/.env` 中是否误设了 `ENV=dev` 或 `IS_DEV_MODE=true`
