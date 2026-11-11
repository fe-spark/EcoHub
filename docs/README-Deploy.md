# EcoHub 部署指南

中文 | [English](./README-Deploy_EN.md)

EcoHub 以 All-in-One 镜像 `ghcr.io/fe-spark/ecohub` 发布：同一容器内运行 Go API（容器内端口 `8080`）和 Next.js 前端（容器内端口 `3000`）。另需一个 MySQL 和一个 Redis。

镜像 tag 说明：

| tag | 指向 | 适用场景 |
| --- | --- | --- |
| `:v3.0.3` 等 | 固定的正式版 | 推荐。升级时手动修改 tag |
| `:latest` | 最新正式版，每次发布正式版时更新 | 始终跟随最新版本 |
| 以 `-beta` 结尾的 tag | 测试版 | 不得用于生产环境。正式版发布后自动删除 |

本文涉及的文件：[编排 deploy/release/compose.yml](../deploy/release/compose.yml) · [变量模板 .env.example](../.env.example) · [版本说明](./RELEASE.md)。从 v2 升级前，请先阅读 [从 v2 升级到 v3](./v2-to-v3.md)。

## 准备

- Docker 20+ 和 Docker Compose（`docker compose` 插件或独立的 `docker-compose` 均可；使用 `docker run` 手动部署时不需要 Compose）。
- 建议 2 核 CPU、2 GB 内存及以上。
- 服务器能够访问 `ghcr.io`（应用镜像）和 `docker.io`（MySQL、Redis 镜像）。

对外端口：

| 端口 | 默认值 | 用途 |
| --- | --- | --- |
| Web 端口 | `3000` | 前台、后台和订阅地址均通过此端口访问 |
| API 直连端口 | `18080` | 映射到容器内 `8080`，直接访问后端 `/api/*`。订阅地址也可使用此端口，见 [访问地址](#访问地址)。不需要时可移除 |

为 `JWT_SECRET` 生成随机密钥：

```bash
openssl rand -hex 32
```

## 方式一：安装脚本（推荐）

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.3 sh
```

脚本执行以下操作：

1. 在安装目录（默认 `~/ecohub`）中创建 `data/mysql`、`data/redis`、`data/uploads`。
2. 下载编排文件并保存为 `docker-compose.yml`，下载 `.env.example`。不存在 `.env` 时由模板生成；已存在时保留。不会创建或修改 `docker-compose.override.yml`。
3. 将写在 `sh` 前的变量写入 `.env`（见下文）。全新安装且未指定镜像时，写入 `ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.3`。
4. 将 `ecohub` 命令安装到 `/usr/local/bin`；该目录不可写时安装到 `~/.local/bin`，并提示将其加入 `PATH`。
5. 记录安装目录（写入 `~/.ecohub/install-dir`），之后的 `ecohub` 命令均操作该目录。
6. 启动容器，输出前台、后台地址、初始账号和三条订阅地址。

### 第一次安装就设好密码

变量必须写在 `sh` 之前；写在 `curl` 之前的变量不会传入脚本。示例：

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.3 \
    JWT_SECRET="$(openssl rand -hex 32)" \
    ECOHUB_ADMIN_PASSWORD='请改成强密码' \
    MYSQL_ROOT_PASSWORD='请改成强密码' \
    MYSQL_PASSWORD='请改成强密码' \
    REDIS_PASSWORD='请改成强密码' \
    sh
```

`install` 和 `update` 只会将以下 8 个变量写入 `.env`：`ECOHUB_IMAGE`、`WEB_PORT`、`SERVER_PORT`、`JWT_SECRET`、`ECOHUB_ADMIN_PASSWORD`、`MYSQL_ROOT_PASSWORD`、`MYSQL_PASSWORD`、`REDIS_PASSWORD`。未传入的变量保持不变，`.env` 中的其他行也会保留。

未传入密码时，`.env` 使用模板中的公开默认值（`JWT_SECRET` 为仓库中的示例值，数据库密码为 `ecohub`，管理员密码为 `admin`）。对公网开放前必须修改。

如需更换安装目录，将 `ECOHUB_INSTALL_DIR=~/ecohub-v3` 一并写在 `sh` 之前。

### 修改密码和端口的生效方式

| 变量 | 修改 `.env` 后执行 `ecohub restart` | 说明 |
| --- | --- | --- |
| `WEB_PORT`、`SERVER_PORT` | 生效 | |
| `REDIS_PASSWORD` | 生效 | Redis 每次启动时按此值设置密码 |
| `JWT_SECRET` | 生效 | 所有已登录用户需要重新登录 |
| `ECOHUB_ADMIN_PASSWORD` | 不生效 | 仅在首次创建 `admin` 账号时写入，之后请在后台修改密码 |
| `MYSQL_PASSWORD`、`MYSQL_ROOT_PASSWORD` | 不生效，且会导致连接失败 | MySQL 仅在 `data/mysql` 首次初始化时设置密码 |

示例：安装完成后将 `.env` 中的 `MYSQL_PASSWORD` 由 `ecohub` 改为 `abc` 并执行 `ecohub restart`。应用使用 `abc` 连接，而 MySQL 中保存的仍是 `ecohub`，导致连接失败，容器反复重启。

`ecohub start` 和 `ecohub restart` 只读取 `.env`，不会将当前终端中的变量写入 `.env`。

### ecohub 命令

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
| `ecohub install` | 下载编排、启动服务并输出地址。直接执行 `ecohub` 等同于此命令 |
| `ecohub status` | 输出访问地址和各容器状态 |
| `ecohub logs` | 查看最近 100 行日志。`logs -f` 持续跟踪，`logs ecohub` 只查看应用日志 |
| `ecohub start` | 按当前 `.env` 启动 |
| `ecohub stop` | 停止容器，保留数据 |
| `ecohub restart` | 重建容器，使 `.env` 的修改生效 |
| `ecohub update` | 重新下载编排，拉取 `.env` 中 `ECOHUB_IMAGE` 指定的镜像并重建容器 |
| `ecohub uninstall` | 删除容器，保留 `data/`、`.env` 和 `ecohub` 命令 |
| `ecohub uninstall --purge` | 额外删除 `data/mysql`、`data/redis`、`data/uploads`。`.env` 和 `ecohub` 命令仍保留 |
| `ecohub help` | 显示用法 |

升级到新版本：将 `.env` 中的 `ECOHUB_IMAGE` 改为新 tag 后执行 `ecohub update`，或直接执行 `ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:<新版本> ecohub update`。

`ecohub update` 每次都会重新下载 `docker-compose.yml`，对该文件的直接修改会被覆盖。如需修改编排，请写入 [自定义编排](#自定义编排) 中的 `docker-compose.override.yml`。

后台的「立即升级并重启」拉取的是 `:latest`，不读取 `.env` 中固定的 tag。

### 自定义编排

在安装目录（默认 `~/ecohub`）中新建 `docker-compose.override.yml`，写入需要修改的部分。`ecohub install` 和 `ecohub update` 不会创建、覆盖或删除该文件；该文件存在时，`ecohub` 的所有命令（`start`、`restart`、`update`、`status` 等）都会叠加它，`ecohub status` 会输出「已叠加 docker-compose.override.yml」。

示例：Web 端口仅监听本机（配合反向代理），并且不开放 API 直连端口：

```yaml
services:
  ecohub:
    ports: !override
      - "127.0.0.1:${WEB_PORT:-3000}:3000"
```

保存后执行 `ecohub restart`。

- `!override` 表示整体替换原有的 `ports`。省略时，Compose 会合并两处端口配置，原有的监听所有网卡的绑定仍然存在。
- `!override` 需要 Docker Compose v2.24.4 或更高版本，可通过 `docker compose version` 查看。独立的 `docker-compose` v1 不支持。
- 不再需要时删除该文件，并执行 `ecohub restart`。

## 方式二：1Panel

1. 在 1Panel 中新建 Compose 编排，工作目录示例：`/opt/1panel/apps/ecohub`。
2. 粘贴 [deploy/release/compose.yml](../deploy/release/compose.yml) 的全部内容。
3. 填写环境变量：

```env
ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.3
WEB_PORT=3000
SERVER_PORT=18080
JWT_SECRET=请替换为长随机串
ECOHUB_ADMIN_PASSWORD=请改成强密码
MYSQL_ROOT_PASSWORD=请改成强密码
MYSQL_HOST=mysql
MYSQL_PORT=3306
MYSQL_USER=eco
MYSQL_PASSWORD=请改成强密码
MYSQL_DBNAME=eco
REDIS_HOST=redis
REDIS_PORT=6379
REDIS_PASSWORD=请改成强密码
REDIS_DB=0
```

4. 保存并启动，确认 `Eco-hub`、`Eco-mysql`、`Eco-redis` 三个容器均在运行。
5. 为站点配置反向代理，指向 `http://127.0.0.1:3000`。

更新：修改 `ECOHUB_IMAGE` 后，在工作目录中执行：

```bash
docker compose pull && docker compose up -d
```

## 方式三：手动 docker run

此方式不使用 Compose。执行前请将以下密码替换为强随机值。

```bash
mkdir -p ~/ecohub/data/mysql ~/ecohub/data/redis ~/ecohub/data/uploads
docker network inspect Eco-network >/dev/null 2>&1 || docker network create Eco-network

MYSQL_ROOT_PASSWORD='请改成强密码'
MYSQL_PASSWORD='请改成强密码'
REDIS_PASSWORD='请改成强密码'
JWT_SECRET="$(openssl rand -hex 32)"

docker pull mysql:8.4
docker pull redis:7.4-alpine
docker pull ghcr.io/fe-spark/ecohub:v3.0.3

docker run -d --name Eco-mysql --restart always --network Eco-network \
  -e MYSQL_ROOT_PASSWORD="$MYSQL_ROOT_PASSWORD" \
  -e MYSQL_DATABASE=eco \
  -e MYSQL_USER=eco \
  -e MYSQL_PASSWORD="$MYSQL_PASSWORD" \
  -v ~/ecohub/data/mysql:/var/lib/mysql \
  mysql:8.4

docker run -d --name Eco-redis --restart always --network Eco-network \
  -v ~/ecohub/data/redis:/data \
  redis:7.4-alpine \
  redis-server --requirepass "$REDIS_PASSWORD"

until docker exec Eco-mysql mysql -ueco -p"$MYSQL_PASSWORD" -e 'SELECT 1' eco >/dev/null 2>&1; do
  sleep 2
done

docker run -d --name Eco-hub --restart always --network Eco-network \
  --add-host=host.docker.internal:host-gateway \
  -p 3000:3000 \
  -p 18080:8080 \
  -e PORT=8080 \
  -e JWT_SECRET="$JWT_SECRET" \
  -e ECOHUB_ADMIN_PASSWORD="请改成强密码" \
  -e MYSQL_HOST=Eco-mysql \
  -e MYSQL_PORT=3306 \
  -e MYSQL_USER=eco \
  -e MYSQL_PASSWORD="$MYSQL_PASSWORD" \
  -e MYSQL_DBNAME=eco \
  -e REDIS_HOST=Eco-redis \
  -e REDIS_PORT=6379 \
  -e REDIS_PASSWORD="$REDIS_PASSWORD" \
  -e REDIS_DB=0 \
  -v ~/ecohub/data/uploads:/app/static/upload \
  -v /var/run/docker.sock:/var/run/docker.sock \
  ghcr.io/fe-spark/ecohub:v3.0.3
```

- 应用容器中的数据库主机名为容器名 `Eco-mysql`、`Eco-redis`。
- `until` 循环用于等待 MySQL 首次初始化完成。长时间未完成时，请查看 `docker logs Eco-mysql`。
- 挂载 `/var/run/docker.sock` 供后台「立即升级并重启」使用，不使用该功能时可以移除。

查看日志：`docker logs -f Eco-hub`。更新：`docker pull` 新 tag，执行 `docker rm -f Eco-hub`，再使用相同参数和新 tag 重新执行 `docker run`。

## 自备 MySQL 和 Redis

1. 从编排中删除 `mysql`、`redis` 两个服务以及 `ecohub` 下的 `depends_on`。手动部署时不启动 `Eco-mysql`、`Eco-redis`。
2. 将 `MYSQL_*`、`REDIS_*` 指向自备实例。
3. 应用运行在容器内，`127.0.0.1` 指向容器自身。数据库安装在宿主机上时，主机名使用 `host.docker.internal`。

示例：数据库位于宿主机时：

```env
MYSQL_HOST=host.docker.internal
REDIS_HOST=host.docker.internal
```

应用启动时自动建表并写入初始数据。数据库需要预先创建，可以为空库。

## 访问地址

| 地址 | 用途 |
| --- | --- |
| `http://<服务器IP>:3000` | 前台 |
| `http://<服务器IP>:3000/manage` | 管理后台 |
| `http://<服务器IP>:3000/api/provide/app` | EcoHub 客户端订阅 |
| `http://<服务器IP>:3000/api/provide/tvbox` | TVBox / 影视仓订阅 |
| `http://<服务器IP>:3000/api/provide/vod` | MacCMS 订阅 |

`3000` 为默认 Web 端口，实际地址以终端输出为准。

订阅地址也可使用 API 直连端口，例如 `http://<服务器IP>:18080/api/provide/tvbox`。三条订阅地址均为 `/api/provide/*`，由 Go 后端直接处理；Web 端口仅将 `/api/*` 原样转发给 Go，因此两个端口返回的内容相同。返回内容中的地址（TVBox 的站点接口、客户端的 `api_base`）按请求地址生成，通过 `18080` 订阅时这些地址同样使用 `18080`。前台和后台页面仅通过 Web 端口提供。

后台账号为 `admin`，密码为 `ECOHUB_ADMIN_PASSWORD`，未设置时为 `admin`。首次登录后请立即修改密码。

后台开启「私有化访问」后，订阅地址需要携带订阅密钥，例如 `http://<服务器IP>:3000/api/provide/tvbox?key=你的密钥`。详见 [常见问题](./README-FAQ.md#开启私有化访问后订阅怎么填)。

数据目录（安装目录默认为 `~/ecohub`）：

| 目录 | 内容 |
| --- | --- |
| `data/mysql` | MySQL 数据 |
| `data/redis` | Redis 数据 |
| `data/uploads` | 后台上传的图片，对应容器内 `/app/static/upload` |

应用运行日志同时输出到 `docker logs`（`ecohub logs`）和容器内的 `/app/server/logs`。容器内的日志未挂载到宿主机，重建容器后会丢失。

首次打开时前台为空。请在后台「采集中心」核对 12 个预设采集站，并执行一次全量采集。影片会随采集进度陆续出现，首次采集可能需要数小时。

## 反向代理

生产环境建议：

- 反向代理将整个站点指向 `127.0.0.1:3000`，并启用 HTTPS。`/api` 无需单独转发到 `18080`，Web 端口会将 `/api/*` 转发给容器内的 Go 进程。
- 不希望公网直接访问 `3000`、也不使用 `18080` 时：通过安装脚本部署的，使用 [docker-compose.override.yml](#自定义编排)；自行维护编排的，直接修改 `ports`。
- 云服务器安全组仅放行 `80`、`443`。

## 环境变量

「读取方」一列中，Compose 表示仅供编排使用，应用表示由 Go 进程读取。

| 变量 | 默认值 | 读取方 | 说明 |
| --- | --- | --- | --- |
| `ECOHUB_IMAGE` | `ghcr.io/fe-spark/ecohub:latest`（安装脚本全新安装时写入 `:v3.0.3`） | Compose | 镜像 tag |
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
| `REDIS_HOST` | `redis` | 应用 | |
| `REDIS_PORT` | `6379` | 应用 | |
| `REDIS_PASSWORD` | `ecohub` | 应用、内置 Redis | 修改 `.env` 后执行 `ecohub restart` 生效 |
| `REDIS_DB` | `0` | 应用 | |

采集、TMDB 刮削、Telegram 通知、版本检查或弹幕抓取需要通过代理访问时，在后台「网络代理」（`/manage/system/proxy`）中填写代理地址（`http://`、`https://` 或 `socks5://`），并按模块开启。代理仅在此处配置，未启用代理的请求一律直连。各模块涵盖的请求及默认状态，见 [常见问题](./README-FAQ.md#哪些请求走后台网络代理)。

`NODE_NAME`、`ECOHUB_LOG_DIR` 等仅供 Go 进程使用的变量未由发布编排传入容器，写入 `.env` 不会生效。如需使用，请在编排的 `environment` 中自行添加。变量含义见 [服务端说明](../server/README.md#环境变量)。

发布镜像在构建时已将前端的 `API_URL` 固定为 `http://127.0.0.1:8080`，部署时无需也不应设置 `API_URL`。

## 故障排查

- 健康检查：访问 `http://<服务器IP>:3000/api/config/basic`，返回 JSON 即表示前后端均已启动。映射了 `18080` 时，也可通过 `http://<服务器IP>:18080/api/config/basic` 直接检查后端。
- 容器反复重启：查看 `ecohub logs` 或 `docker logs Eco-hub`。常见原因是数据库密码与首次初始化时的值不一致，或端口被占用。后端启动时先等待 Redis、再等待 MySQL，各重试 30 次，间隔 2 秒；仍无法连接时进程退出，由容器内的 supervisord 重新拉起。
- 前台可以打开但接口报错：检查反向代理是否将整个站点指向 Web 端口。
- 端口被占用：通常是旧容器仍在运行，使用 `docker ps -a` 找到后删除。

功能行为相关的问题（如站点为空、前台未更新等）见 [常见问题](./README-FAQ.md)。

## 相关文档

[总览](../README.md) · [从 v2 升级到 v3](./v2-to-v3.md) · [常见问题](./README-FAQ.md) · [版本说明](./RELEASE.md)
