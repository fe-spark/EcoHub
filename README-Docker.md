# 服务器部署说明

当前仓库提供的 `docker-compose.yml` 可用于服务器部署，包含这些服务：

- `web`：Next.js 前端
- `server`：Go API 服务
- `mysql`：可选的 MySQL 服务
- `redis`：可选的 Redis 服务

部署时请先明确你属于哪一种场景：

1. 宿主机或外部环境已经有可用的 MySQL / Redis
2. 宿主机没有 MySQL / Redis，准备直接使用 Compose 内置的 MySQL / Redis

## 前置条件

- Docker 20+
- Docker Compose 2+
- Docker 镜像拉取能力

## 当前 Compose 约定

- `web` 对外暴露 `3000`
- `server` 不再直接暴露宿主机端口，只在容器网络内监听 `8080`
- `web` 构建期和运行期都会收到 `API_URL=http://server:8080`
- 浏览器端访问当前站点下的 `/api/*`，再由 Next rewrite 转发到 `server`
- Compose 运行时不读取 `server/.env`、`web/.env.production`，也不需要 `--env-file`
- `docker-compose.yml` 里的 `JWT_SECRET` 只是占位值，正式部署前必须替换，可用 `openssl rand -hex 32` 生成

这意味着：

- 正常对外入口是 `web`，例如 `http://你的服务器:3000`
- 外部访问 API 也走 `web` 的 `/api/*`
- `server` 只提供给 `web` 和其他容器内部访问

## 场景一：宿主机已有 MySQL / Redis

适用场景：

- 你的服务器本机已经安装了 MySQL / Redis
- 或者你准备连接外部数据库、云数据库、远程 Redis
- 不希望启动 Compose 内置的 `mysql` / `redis`

### 1. 修改 `docker-compose.yml`

重点修改 `server.environment` 中这几项：

```yml
server:
  environment:
    PORT: 8080
    JWT_SECRET: your_generated_secret # 可用 openssl rand -hex 32 生成
    MYSQL_HOST: host.docker.internal
    MYSQL_PORT: 3306
    MYSQL_USER: your_mysql_user
    MYSQL_PASSWORD: your_mysql_password
    MYSQL_DBNAME: your_mysql_db
    REDIS_HOST: host.docker.internal
    REDIS_PORT: 6379
    REDIS_PASSWORD: your_redis_password
    REDIS_DB: 0
```

如何填写：

- 如果 MySQL / Redis 就运行在 Docker 宿主机上，可优先写 `host.docker.internal`
- 如果它们运行在其他机器上，请直接写真实 IP、域名或内网地址
- 如果 Redis 没有密码，可把 `REDIS_PASSWORD` 留空字符串
- `JWT_SECRET` 必须替换为你自己的高强度随机值，可用 `openssl rand -hex 32` 生成

### 2. 启动应用服务

```bash
docker compose up --build -d server web
```

### 3. 结果说明

- Compose 只会启动 `server` 和 `web`
- 不会启动内置 `mysql` / `redis`
- 不会占用宿主机 `3306` / `6379`
- `server` 会按你写入的地址连接宿主机或外部 MySQL / Redis

## 场景二：宿主机没有 MySQL / Redis

适用场景：

- 服务器上还没有数据库和缓存
- 希望直接使用仓库内置的 MySQL / Redis 容器

### 1. 保持或确认 `docker-compose.yml` 中默认值正确

当前默认配置就是给 Compose 内置服务准备的：

```yml
server:
  environment:
    PORT: 8080
    JWT_SECRET: your_generated_secret # 可用 openssl rand -hex 32 生成
    MYSQL_HOST: mysql
    MYSQL_PORT: 3306
    MYSQL_USER: eco
    MYSQL_PASSWORD: eco_local_password
    MYSQL_DBNAME: eco
    REDIS_HOST: redis
    REDIS_PORT: 6379
    REDIS_PASSWORD: redis_local_password
    REDIS_DB: 0
```

你至少要做两件事：

- 把 `JWT_SECRET` 改成你自己的高强度随机值，可用 `openssl rand -hex 32` 生成
- 如果你不想用默认数据库密码，也同步修改 `mysql.environment`、`redis.command` 和 `server.environment` 中对应的密码

### 2. 启动全部服务

```bash
docker compose up --build -d mysql redis server web
```

### 3. 结果说明

- Compose 会启动内置 `mysql`、`redis`、`server`、`web`
- `server` 会通过容器网络连接 `mysql:3306` 和 `redis:6379`
- 宿主机也会被映射出 `3306` 和 `6379`

如果宿主机本身已经有人占用了 `3306` 或 `6379`，这种模式会启动失败，你要么释放对应端口，要么改 Compose 里的宿主机映射端口。

## 启动后如何访问

- 前端首页：`http://你的服务器:3000`
- 后台入口：`http://你的服务器:3000/manage`
- 对外 API：`http://你的服务器:3000/api/*`
- TVBox / 影视仓配置：`http://你的服务器:3000/api/provide/config`

注意：

- 当前 Compose 默认不把 `server` 直接暴露到宿主机
- 如果你确实需要从宿主机单独访问后端，再额外给 `server` 增加一个不冲突的端口映射，例如 `18080:8080`

## 常用命令

```bash
docker compose ps
docker compose logs -f web
docker compose logs -f server
docker compose restart web
docker compose restart server
docker compose down
docker compose down -v
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

同时如果你使用 Compose 内置 MySQL / Redis，当前仓库已经为它们配置了 Docker volume：

- `eco-mysql-data`
- `eco-redis-data`

## 网络说明

当前 `server` 服务包含：

```yaml
extra_hosts:
  - "host.docker.internal:host-gateway"
```

这主要用于容器访问宿主机上的 MySQL / Redis。

如果你的数据库和 Redis 不在宿主机，而是在其他机器或容器网络中，请把 `docker-compose.yml` 中 `server.environment` 的 `MYSQL_HOST` 和 `REDIS_HOST` 改成实际地址。

## 生产建议

- 默认账号 `admin / admin`、`guest / guest` 仅用于初始化或演示，部署后应立即修改密码或直接禁用
- `JWT_SECRET` 必须使用单独的高强度随机值，可用 `openssl rand -hex 32` 生成
- 优先通过反向代理暴露统一入口，而不是直接暴露 API 服务
- 为前端站点启用 HTTPS
- 不要把真实生产密码直接提交到仓库；如果需要长期维护，建议把 `docker-compose.yml` 中的敏感值迁移到安全的部署系统或密钥管理方案

## 常见问题

- `server` 容器启动失败并提示缺少环境变量
  - 检查 `docker-compose.yml` 中 `server.environment` 是否把 `JWT_SECRET` 等必填项写全
  - 如果还没生成 `JWT_SECRET`，可先执行 `openssl rand -hex 32`
- 使用宿主机 MySQL / Redis 时连接失败
  - 先确认宿主机或外部数据库地址填写正确
  - 再确认数据库用户允许来自容器网络的连接
  - 最后检查宿主机防火墙是否放通
- 使用内置 MySQL / Redis 时启动失败
  - 检查宿主机 `3306` / `6379` 是否已被占用
  - 执行 `docker compose logs -f mysql`
  - 执行 `docker compose logs -f redis`
- 前端打开后接口全部失败
  - 检查 `server` 容器是否已经启动并通过健康检查
  - 检查 `web` 容器中的 `API_URL` 是否仍然指向 `http://server:8080`
