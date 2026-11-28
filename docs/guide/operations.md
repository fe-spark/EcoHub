# 运维与配置

中文 | [English](../en/guide/operations.md)

本文涵盖安装后的 `ecohub` 日常管理命令、密码与端口生效机制、自定义编排、外置数据库配置、完整环境变量表及常见故障排查。

## ecohub 命令

```bash
ecohub status
ecohub logs
ecohub logs -f
ecohub restart
ecohub update
ecohub stop
ecohub start
ecohub uninstall
ecohub uninstall --purge
```

| 命令 | 说明 |
| --- | --- |
| `ecohub install` | 下载编排、启动服务并输出地址；直接执行 `ecohub` 等同于此命令 |
| `ecohub status` | 输出访问地址和各容器状态 |
| `ecohub logs` | 查看最近 100 行日志；`logs -f` 持续跟踪，`logs ecohub` 只查看应用日志 |
| `ecohub start` | 按当前 `.env` 启动 |
| `ecohub stop` | 停止容器，保留数据 |
| `ecohub restart` | 重建容器，使 `.env` 的修改生效 |
| `ecohub update` | 重新下载编排，拉取 `.env` 中 `ECOHUB_IMAGE` 指定的镜像并重建容器 |
| `ecohub uninstall` | 删除容器，保留 `data/`、`.env` 和 `ecohub` 命令 |
| `ecohub uninstall --purge` | 额外删除 `data/mysql`、`data/redis`、`data/uploads`；保留 `.env` 和 `ecohub` 命令 |
| `ecohub help` | 显示用法 |

升级版本：将 `.env` 中的 `ECOHUB_IMAGE` 改为新 tag 后执行 `ecohub update`，或直接执行 `ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:<新版本> ecohub update`。

- `ecohub update` 每次都会覆盖 `docker-compose.yml`，如需保留编排调整请写入 [自定义编排](#自定义编排)。
- 后台的「立即升级并重启」固定拉取 `:latest`，不读取 `.env` 中固定的 tag。

## 修改密码和端口的生效方式

`ecohub start` 和 `ecohub restart` 只读取 `.env`，不会读取当前终端里的临时变量。

| 变量 | 修改 `.env` 后执行 `ecohub restart` | 说明 |
| --- | --- | --- |
| `WEB_PORT`、`SERVER_PORT` | 生效 | |
| `REDIS_PASSWORD` | 生效 | Redis 每次启动时按此值设置密码 |
| `JWT_SECRET` | 生效 | 所有已登录用户需要重新登录 |
| `ECOHUB_ADMIN_PASSWORD` | 不生效 | 仅在首次创建 `admin` 账号时写入，之后请在后台修改密码 |
| `MYSQL_PASSWORD`、`MYSQL_ROOT_PASSWORD` | 不生效，且会导致连接失败 | MySQL 仅在 `data/mysql` 首次初始化时设置密码 |

示例：安装完成后将 `.env` 中的 `MYSQL_PASSWORD` 由 `ecohub` 改为 `abc` 并执行 `ecohub restart`，应用会使用 `abc` 连接，而库中仍是 `ecohub`，导致连接失败并反复重启。

## 自定义编排

在安装目录（默认 `~/ecohub`）中新建 `docker-compose.override.yml`，写入需要覆盖的部分。`ecohub install` 和 `ecohub update` 不会创建、覆盖或删除该文件；存在时，所有 `ecohub` 命令都会自动叠加它，`ecohub status` 会提示「已叠加 docker-compose.override.yml」。

示例：Web 端口仅监听本机（配合反向代理），并且不开放 API 直连端口：

```yaml
services:
  ecohub:
    ports: !override
      - "127.0.0.1:${WEB_PORT:-3000}:3000"
```

保存后执行 `ecohub restart`：

- `!override` 表示整体替换原有 `ports`，省略时 Compose 会与原列表合并。
- `!override` 需要 Docker Compose v2.24.4 或更高版本，独立版 `docker-compose` v1 不支持。
- 不再需要时删除该文件并执行 `ecohub restart`。

## 自备 MySQL 和 Redis

1. 从编排中删除 `mysql`、`redis` 服务以及 `ecohub` 下的 `depends_on`；手动部署时不启动 `Eco-mysql`、`Eco-redis`。
2. 将 `MYSQL_*`、`REDIS_*` 指向自备实例。
3. 容器内 `127.0.0.1` 指向容器自身；数据库位于宿主机时，主机名使用 `host.docker.internal`。

示例：数据库位于宿主机时：

```env
MYSQL_HOST=host.docker.internal
REDIS_HOST=host.docker.internal
```

应用启动时自动建表并写入初始数据。数据库需要预先创建，可以为空库。

## 数据目录与日志

安装目录（默认 `~/ecohub`）下包含：

| 目录 | 内容 |
| --- | --- |
| `data/mysql` | MySQL 数据 |
| `data/redis` | Redis 数据 |
| `data/uploads` | 后台上传的图片，对应容器内 `/app/static/upload` |

应用运行日志同时输出到 `docker logs`（`ecohub logs`）和容器内的 `/app/server/logs`；容器内日志文件未挂载到宿主机，重建容器后会丢失。

## 环境变量

「读取方」一列中，Compose 表示仅供编排使用，应用表示由 Go 进程读取。

| 变量 | 默认值 | 读取方 | 说明 |
| --- | --- | --- | --- |
| `ECOHUB_IMAGE` | `ghcr.io/fe-spark/ecohub:latest`（安装脚本全新安装时写入 `:v3.0.7`） | Compose | 镜像 tag |
| `WEB_PORT` | `3000` | Compose | 宿主机 Web 端口，映射到容器内 `3000` |
| `SERVER_PORT` | `18080` | Compose | 宿主机 API 直连端口，映射到容器内 `8080` |
| `JWT_SECRET` | 仓库中的示例值 | 应用 | 登录签名密钥。必须改为随机字符串；修改后所有用户需要重新登录 |
| `ECOHUB_ADMIN_PASSWORD` | `admin` | 应用 | 仅在首次创建 `admin` 时写入 |
| `MYSQL_ROOT_PASSWORD` | `ecohub` | 内置 MySQL | 仅在 `data/mysql` 首次初始化时生效 |
| `MYSQL_HOST` | `mysql` | 应用 | 容器内不得使用 `127.0.0.1` |
| `MYSQL_PORT` | `3306` | 应用 | |
| `MYSQL_USER` | `eco` | 应用、内置 MySQL | |
| `MYSQL_PASSWORD` | `ecohub` | 应用、内置 MySQL | 内置 MySQL 仅在首次初始化时设置此密码 |
| `MYSQL_DBNAME` | `eco` | 应用、内置 MySQL | |
| `MYSQL_INNODB_BUFFER_POOL_SIZE` | `1G` | 内置 MySQL | InnoDB 缓冲池大小 |
| `MYSQL_INNODB_REDO_LOG_CAPACITY` | `512M` | 内置 MySQL | InnoDB 重做日志容量 |
| `REDIS_HOST` | `redis` | 应用 | |
| `REDIS_PORT` | `6379` | 应用 | |
| `REDIS_PASSWORD` | `ecohub` | 应用、内置 Redis | 修改 `.env` 后执行 `ecohub restart` 生效 |
| `REDIS_DB` | `0` | 应用 | |

- 出站网络代理统一在后台「网络代理」（`/manage/system/proxy`）配置（支持 `http://`、`https://`、`socks5://`、`socks5h://`），容器环境变量不影响应用出站请求；各模块作用域见 [常见问题](./faq.md#哪些请求走后台网络代理)。
- `NODE_NAME`、`ECOHUB_LOG_DIR`、`ECOHUB_TRUSTED_PROXIES` 等仅供 Go 进程使用的变量未由发布编排默认传入容器，写入 `.env` 不会生效；如需使用，可在编排 `environment` 中自行添加，含义见 [服务端说明](../../server/README.md#环境变量)。
- 发布镜像在构建时已将前端的 `API_URL` 固定为 `http://127.0.0.1:8080`，部署时无需也不应设置 `API_URL`。

## 故障排查

- **健康检查**：访问 `http://<服务器IP>:3000/api/config/basic`，返回 JSON 即表示前后端均已启动；映射了 `18080` 时也可通过 `http://<服务器IP>:18080/api/config/basic` 直接检查后端。
- **容器反复重启**：查看 `ecohub logs` 或 `docker logs Eco-hub`。常见原因为数据库密码与首次初始化不一致，或端口被占用。后端启动时依次等待 Redis 与 MySQL（各重试 30 次、间隔 2 秒），仍失败时进程退出并由容器内 supervisord 重新拉起。
- **前台可以打开但接口报错**：检查反向代理是否将整个站点指向 Web 端口。
- **端口被占用**：通常是旧容器仍在运行，使用 `docker ps -a` 找到后删除。

功能与数据行为相关的问题见 [常见问题](./faq.md)。

## 相关文档

[部署指南](./deploy.md) · [从 v2 升级到 v3](./v2-to-v3.md) · [常见问题](./faq.md) · [版本说明](../RELEASE.md)
