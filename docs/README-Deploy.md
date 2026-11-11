# EcoHub 部署指南

中文 | [English](./README-Deploy_EN.md)

EcoHub 发布的是一个 All-in-One 镜像 `ghcr.io/fe-spark/ecohub`：同一个容器里跑 Go API（容器内端口 `8080`）和 Next.js 前端（容器内端口 `3000`）。再配一个 MySQL 和一个 Redis 就能用。

镜像 tag 怎么选：

| tag | 指向 | 什么时候用 |
| --- | --- | --- |
| `:v3.0.2` 这类 | 固定的正式版 | 推荐。升级时自己改 tag |
| `:latest` | 最新正式版，每次发正式版都会更新 | 想一直跟最新版 |
| `-beta` 结尾的 tag | 测试版 | 不要用于生产。发正式版后会被自动删除 |

本页用到的文件：[编排 deploy/release/compose.yml](../deploy/release/compose.yml) · [变量模板 .env.example](../.env.example) · [版本说明](./RELEASE.md)。从 v2 升级先看 [从 v2 升级到 v3](./v2-to-v3.md)。

## 准备

- Docker 20+ 和 Docker Compose 2+（手动 `docker run` 部署不需要 Compose）。
- 建议 2 核 2 GB 以上。
- 服务器能拉取 `ghcr.io`（应用镜像）和 `docker.io`（MySQL、Redis 镜像）。

两个对外端口：

| 端口 | 默认 | 用途 |
| --- | --- | --- |
| Web 端口 | `3000` | 前台、后台、订阅地址都走这个端口 |
| API 直连端口 | `18080` | 映射到容器内 `8080`，给播放器直连后端用。用不上可以去掉 |

准备一个随机密钥给 `JWT_SECRET`：

```bash
openssl rand -hex 32
```

## 方式一：安装脚本（推荐）

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2 sh
```

脚本做了这些事：

1. 在安装目录（默认 `~/ecohub`）建 `data/mysql`、`data/redis`、`data/uploads`。
2. 下载编排存成 `docker-compose.yml`，下载 `.env.example`。没有 `.env` 时用模板生成一份，已有 `.env` 时保留。
3. 把写在 `sh` 前面的变量写进 `.env`（见下文）。新装且没指定镜像时，写入 `ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2`。
4. 安装 `ecohub` 命令到 `/usr/local/bin`；这个目录不可写时装到 `~/.local/bin`，并提示把它加进 `PATH`。
5. 记住安装目录（写在 `~/.ecohub/install-dir`），以后 `ecohub` 命令都操作这个目录。
6. 启动容器，打印前台、后台、初始账号和三条订阅地址。

### 第一次安装就设好密码

变量要写在 `sh` 前面，写在 `curl` 前面传不进脚本。例如：

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2 \
    JWT_SECRET="$(openssl rand -hex 32)" \
    ECOHUB_ADMIN_PASSWORD='请改成强密码' \
    MYSQL_ROOT_PASSWORD='请改成强密码' \
    MYSQL_PASSWORD='请改成强密码' \
    REDIS_PASSWORD='请改成强密码' \
    sh
```

`install` 和 `update` 只会把这 8 个变量写进 `.env`：`ECOHUB_IMAGE`、`WEB_PORT`、`SERVER_PORT`、`JWT_SECRET`、`ECOHUB_ADMIN_PASSWORD`、`MYSQL_ROOT_PASSWORD`、`MYSQL_PASSWORD`、`REDIS_PASSWORD`。没传的变量不动，`.env` 里其他行也保留。

不传密码时，`.env` 用模板里的公开默认值（`JWT_SECRET` 是一串写在仓库里的示例值，数据库密码是 `ecohub`，管理员密码是 `admin`）。对公网开放前一定要改。

换安装目录：把 `ECOHUB_INSTALL_DIR=~/ecohub-v3` 也写在 `sh` 前面。

### 改密码、改端口什么时候生效

| 变量 | 改 `.env` 后执行 `ecohub restart` | 说明 |
| --- | --- | --- |
| `WEB_PORT`、`SERVER_PORT` | 生效 | |
| `REDIS_PASSWORD` | 生效 | Redis 每次启动都按这个值设密码 |
| `JWT_SECRET` | 生效 | 所有已登录的人都要重新登录 |
| `ECOHUB_ADMIN_PASSWORD` | 不生效 | 只在第一次创建 `admin` 账号时写入。之后在后台改密码 |
| `MYSQL_PASSWORD`、`MYSQL_ROOT_PASSWORD` | 不生效，还会出错 | MySQL 只在 `data/mysql` 第一次初始化时设密码 |

举例：装好后把 `.env` 里的 `MYSQL_PASSWORD` 从 `ecohub` 改成 `abc`，再 `ecohub restart`。应用用 `abc` 去连，MySQL 里存的还是 `ecohub`，结果连不上、容器反复重启。

`ecohub start` 和 `ecohub restart` 只读 `.env`，不会把当前终端里的变量写进 `.env`。

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

| 命令 | 做什么 |
| --- | --- |
| `ecohub install` | 下载编排、启动并打印地址。直接执行 `ecohub` 等于这一条 |
| `ecohub status` | 打印访问地址和各容器状态 |
| `ecohub logs` | 看最近 100 行日志。`logs -f` 持续跟踪，`logs ecohub` 只看应用 |
| `ecohub start` | 按当前 `.env` 启动 |
| `ecohub stop` | 停止容器，保留数据 |
| `ecohub restart` | 重建容器，让 `.env` 的修改生效 |
| `ecohub update` | 重新下载编排，拉取 `.env` 里 `ECOHUB_IMAGE` 指向的镜像并重建 |
| `ecohub uninstall` | 删除容器，保留 `data/`、`.env` 和 `ecohub` 命令 |
| `ecohub uninstall --purge` | 再删除 `data/mysql`、`data/redis`、`data/uploads`。`.env` 和 `ecohub` 命令仍保留 |
| `ecohub help` | 显示用法 |

升级到新版本：把 `.env` 里的 `ECOHUB_IMAGE` 改成新 tag，再 `ecohub update`；或者直接 `ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:<新版本> ecohub update`。

注意：`ecohub update` 每次都会重新下载 `docker-compose.yml`，你手改过的编排会被覆盖，`.env` 不受影响。

后台的「立即升级并重启」拉的是 `:latest`，不看 `.env` 里固定的 tag。

这套容器会出现在 1Panel 的容器列表里。继续用 `ecohub` 管理，不要再用同样的容器名建第二套编排。

## 方式二：1Panel

1. 「容器」→「编排」→「创建」，工作目录例如 `/opt/1panel/apps/ecohub`。
2. 贴入 [deploy/release/compose.yml](../deploy/release/compose.yml) 全文。
3. 填环境变量：

```env
ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2
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

4. 保存并启动，确认 `Eco-hub`、`Eco-mysql`、`Eco-redis` 三个容器在运行。
5. 「网站」→「反向代理」，指向 `http://127.0.0.1:3000`。

更新：改好 `ECOHUB_IMAGE` 后，在工作目录执行：

```bash
docker compose pull && docker compose up -d
```

## 方式三：手动 docker run

不用 Compose。先把下面的密码换成强随机值。

```bash
mkdir -p ~/ecohub/data/mysql ~/ecohub/data/redis ~/ecohub/data/uploads
docker network inspect Eco-network >/dev/null 2>&1 || docker network create Eco-network

MYSQL_ROOT_PASSWORD='请改成强密码'
MYSQL_PASSWORD='请改成强密码'
REDIS_PASSWORD='请改成强密码'
JWT_SECRET="$(openssl rand -hex 32)"

docker pull mysql:8.4
docker pull redis:7.4-alpine
docker pull ghcr.io/fe-spark/ecohub:v3.0.2

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
  ghcr.io/fe-spark/ecohub:v3.0.2
```

- 应用容器里的数据库主机名是容器名 `Eco-mysql`、`Eco-redis`。
- `until` 那一段在等 MySQL 第一次初始化完成，可能要几十秒。一直等不到就看 `docker logs Eco-mysql`。
- 挂载 `/var/run/docker.sock` 是给后台「立即升级并重启」用的，不用这个功能可以去掉。

看日志：`docker logs -f Eco-hub`。更新：`docker pull` 新 tag，`docker rm -f Eco-hub`，再用同样的参数和新 tag 执行一次 `docker run`。

## 自备 MySQL 和 Redis

1. 从编排里删掉 `mysql`、`redis` 两个服务和 `ecohub` 下面的 `depends_on`。手动部署时不启动 `Eco-mysql`、`Eco-redis`。
2. 把 `MYSQL_*`、`REDIS_*` 指向你的实例。
3. 应用跑在容器里，`127.0.0.1` 指的是容器自己。数据库装在宿主机上时写 `host.docker.internal`。

例如数据库在宿主机：

```env
MYSQL_HOST=host.docker.internal
REDIS_HOST=host.docker.internal
```

应用启动时会自己建表和写入初始数据，库要事先建好、可以是空的。

## 访问地址

| 地址 | 用途 |
| --- | --- |
| `http://<服务器IP>:3000` | 前台 |
| `http://<服务器IP>:3000/manage` | 管理后台 |
| `http://<服务器IP>:3000/api/provide/app` | EcoHub 客户端订阅 |
| `http://<服务器IP>:3000/api/provide/tvbox` | TVBox / 影视仓订阅 |
| `http://<服务器IP>:3000/api/provide/vod` | MacCMS 订阅 |

`3000` 是默认 Web 端口，以终端打印的地址为准。后台账号 `admin`，密码是 `ECOHUB_ADMIN_PASSWORD`，没设就是 `admin`。登录后马上改密码。

后台开启「私有化访问」后，订阅地址要带上订阅密钥，例如 `http://<服务器IP>:3000/api/provide/tvbox?key=你的密钥`。详见 [常见问题](./README-FAQ.md#开启私有化访问后订阅怎么填)。

数据位置（安装目录默认 `~/ecohub`）：

| 目录 | 内容 |
| --- | --- |
| `data/mysql` | MySQL 数据 |
| `data/redis` | Redis 数据 |
| `data/uploads` | 后台上传的图片，对应容器内 `/app/static/upload` |

应用的运行日志同时输出到 `docker logs`（`ecohub logs`）和容器内的 `/app/server/logs`。容器内的这份没有挂载出来，重建容器后会丢。

第一次打开前台是空的。到后台「采集中心」核对 12 个预设采集站，执行一次全量采集。影片会边采边出现，第一次可能要几个小时。

## 反向代理

生产环境建议：

- 反向代理整站指向 `127.0.0.1:3000`，并开启 HTTPS。`/api` 不需要单独转到 `18080`，Web 端口会自己把 `/api/*` 转给容器内的 Go。
- 不想公网直接访问 `3000`，可以把编排里的端口写成 `127.0.0.1:${WEB_PORT:-3000}:3000`。不用播放器直连，就删掉 `18080` 那一行。用安装脚本时记住：`ecohub update` 会覆盖手改的编排，更新后要重新改。
- 云服务器安全组只放行 `80`、`443`。

## 环境变量

「谁读」一列：Compose 表示只给编排用，应用表示 Go 进程会读。

| 变量 | 默认值 | 谁读 | 说明 |
| --- | --- | --- | --- |
| `ECOHUB_IMAGE` | `ghcr.io/fe-spark/ecohub:latest`（安装脚本新装写入 `:v3.0.2`） | Compose | 镜像 tag |
| `WEB_PORT` | `3000` | Compose | 宿主机 Web 端口，映射到容器内 `3000` |
| `SERVER_PORT` | `18080` | Compose | 宿主机 API 直连端口，映射到容器内 `8080` |
| `JWT_SECRET` | 仓库里的示例值 | 应用 | 登录签名密钥。必须改成随机串，改了以后所有人重新登录 |
| `ECOHUB_ADMIN_PASSWORD` | `admin` | 应用 | 只在第一次创建 `admin` 时写入 |
| `MYSQL_ROOT_PASSWORD` | `ecohub` | 内置 MySQL | 只在 `data/mysql` 第一次初始化时生效 |
| `MYSQL_HOST` | `mysql` | 应用 | 容器里不要写 `127.0.0.1` |
| `MYSQL_PORT` | `3306` | 应用 | |
| `MYSQL_USER` | `eco` | 应用、内置 MySQL | |
| `MYSQL_PASSWORD` | `ecohub` | 应用、内置 MySQL | 内置 MySQL 只在第一次初始化时设这个密码 |
| `MYSQL_DBNAME` | `eco` | 应用、内置 MySQL | |
| `REDIS_HOST` | `redis` | 应用 | |
| `REDIS_PORT` | `6379` | 应用 | |
| `REDIS_PASSWORD` | `ecohub` | 应用、内置 Redis | 改 `.env` 后 `ecohub restart` 生效 |
| `REDIS_DB` | `0` | 应用 | |

需要让采集、TMDB 刮削、Telegram 通知或版本检查走代理时，在后台「网络代理」（`/manage/system/proxy`）填写代理地址（`http://`、`https://` 或 `socks5://`），再按模块开启。代理只在这里配置。

`NODE_NAME`、`ECOHUB_LOG_DIR` 等只给 Go 进程用的变量，发布编排没有传进容器，写进 `.env` 不会生效。需要时自己在编排的 `environment` 里加。这些变量的含义见 [服务端说明](../server/README.md#环境变量)。

发布镜像构建时已经把前端的 `API_URL` 固定为 `http://127.0.0.1:8080`，部署时不需要也不应该设置 `API_URL`。

## 排障

- 健康检查：访问 `http://<服务器IP>:3000/api/config/basic`，返回 JSON 就说明前后端都起来了。映射了 `18080` 时，`http://<服务器IP>:18080/api/config/basic` 也能直接查后端。
- 容器反复重启：先看 `ecohub logs` 或 `docker logs Eco-hub`。常见原因是数据库密码已经不是第一次初始化时的值、端口被占用。后端启动时先等 Redis、再等 MySQL，各重试 30 次、每次间隔 2 秒，还连不上就退出，再由容器里的 supervisord 重新拉起。
- 前台能打开、接口报错：检查反向代理是不是整站指向 Web 端口。
- 端口被占：多半是旧容器还在，`docker ps -a` 找到后删掉。

行为上的问题（为什么空站、为什么前台没变化等）见 [常见问题](./README-FAQ.md)。

## 相关

[总览](../README.md) · [从 v2 升级到 v3](./v2-to-v3.md) · [常见问题](./README-FAQ.md) · [版本说明](./RELEASE.md)
