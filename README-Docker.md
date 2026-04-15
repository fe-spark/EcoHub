# Docker 部署说明

当前仓库提供的 `docker-compose.yml` 会启动两个服务：

- `web`：Next.js 前端
- `server`：Go API 服务

MySQL 和 Redis 不在 Compose 内，需要你自行准备外部实例。

## 前置条件

- Docker 20+
- Docker Compose 2+
- 可访问的 MySQL 8+
- 可访问的 Redis 7+

## 1. 准备环境变量

复制根目录示例文件：

```bash
cp .env.example .env
```

至少需要确认这些字段：

- `SERVER_PORT` 或 `LISTENER_PORT`
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

## 2. 启动服务

```bash
docker compose up --build -d
```

对外访问入口以你的 Compose 发布方式或反向代理配置为准，后台路径仍然是 `/manage`。

## 3. Compose 当前行为

### Web 服务

- 构建时通过 `build.args.API_URL` 写入后端对内地址
- 该值会覆盖本地默认的 `http://127.0.0.1:$SERVER_PORT` 推导结果，并供浏览器端和 SSR 共用
- 如果你修改了服务名、网络结构或 API 对内地址，需要同步调整 `docker-compose.yml` 和 `web/Dockerfile`

### API 服务

- 读取根目录 `.env`
- 启动前会等待 Redis 和 MySQL 可用
- 内置健康检查：`GET /api/index`

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

如果你的数据库和 Redis 不在宿主机，而是在其他机器或容器网络中，请把 `.env` 里的 `MYSQL_HOST` 和 `REDIS_HOST` 改成实际地址。

## 生产建议

- 修改或禁用默认账号：管理员 `admin` / `admin`，访客 `guest` / `guest`
- 为 `JWT_SECRET` 使用单独的高强度值
- 优先通过反向代理暴露统一入口，而不是直接暴露 API 服务
- 为前端站点启用 HTTPS
- 将 `.env` 纳入你的部署密钥管理，而不是提交到仓库

## 常见问题

- 容器连不上宿主机数据库
  - 检查 `.env` 中的 `MYSQL_HOST`
  - Linux 环境确认 `host.docker.internal` 是否可用
- 前端打开后接口全部失败
  - 检查 `web` 镜像构建时写入的 `API_URL` 是否仍指向正确的 API 对内地址
- API 服务一启动就清空数据
  - 检查 `.env` 中是否误设了 `ENV=dev` 或 `IS_DEV_MODE=true`
