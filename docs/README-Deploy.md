# EcoHub 部署指南

中文 | [English](./README-Deploy_EN.md)

`ghcr.io/fe-spark/ecohub:latest` 指向最新正式版 v3。要固定版本就写 `ghcr.io/fe-spark/ecohub:v3.0.1`。不写 `ECOHUB_IMAGE` 时，compose 用的就是 `:latest`。

发布镜像是 All-in-One：同一容器里跑 Go API `:8080` 和 Next.js `:3000`。旧的 `ecohub-web` / `ecohub-server` 双镜像已废弃。

编排：[deploy/release/compose.yml](../deploy/release/compose.yml) · 变量模板：[.env.example](../.env.example) · 版本：[RELEASE.md](./RELEASE.md)

`ecohub` 通过 Docker Compose 拉起应用、MySQL 和 Redis。本机开发不走这条命令，见 [README](../README.md)。

## 前提

Docker 20+。安装脚本和 1Panel 还要 Compose 2+。建议 2 核 2 GB。需要能拉取 `ghcr.io` 和 `docker.io`。

Web 默认 `3000`，前台、后台和订阅都走它。API 直连默认 `18080`，只给播放器，订阅不要填这个端口。

```bash
openssl rand -hex 32
```

自备 MySQL / Redis：从编排里去掉 `mysql`、`redis` 和 `depends_on`，把 `MYSQL_*`、`REDIS_*` 指到你的实例。容器里不要写 `127.0.0.1`。库在宿主机上时用 `host.docker.internal`。

从 v2 过来见 [从 v2 升级到 v3](./v2-to-v3.md)。

## 安装脚本

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.1 sh
```

数据目录默认 `~/ecohub`。换目录把 `ECOHUB_INSTALL_DIR=~/ecohub-v3` 写在 `sh` 前面。变量写在 `curl` 前面不会进脚本。脚本写入编排和 `.env`，安装 `ecohub`，启动，并打印前台、后台、三条订阅和初始账号。之后直接执行 `ecohub`。

`/usr/local/bin` 不可写时，命令装到 `~/.local/bin`，按提示加入 `PATH`。

第一次启动前设好 `JWT_SECRET`、`ECOHUB_ADMIN_PASSWORD`、`MYSQL_ROOT_PASSWORD`、`MYSQL_PASSWORD`、`REDIS_PASSWORD`。`install` 和 `update` 会把当时导出的这些变量，以及 `ECOHUB_IMAGE`、`WEB_PORT`、`SERVER_PORT`，写进 `.env`。已有 `.env` 的其余行保留。内置库保持 `MYSQL_HOST=mysql`、`REDIS_HOST=redis`。

库目录一旦初始化，再改 `ECOHUB_ADMIN_PASSWORD` 或 `MYSQL_PASSWORD` 不会改掉已经写入的密码。改了 MySQL 密码再执行 `ecohub restart`，应用会连不上数据库。`JWT_SECRET`、端口和 `REDIS_PASSWORD` 改完 `.env` 后执行 `ecohub restart` 生效。`start` 和 `restart` 不会用当前环境变量覆盖 `.env`。

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

`uninstall` 删除容器，保留数据。`uninstall --purge` 再删除 `data/mysql`、`data/redis`、`data/uploads`。两种都保留 `.env` 和 `ecohub`。`ecohub update` 沿用 `.env` 里的 `ECOHUB_IMAGE`。后台「立即升级」拉的是 `:latest`。

| 地址 | 说明 |
| --- | --- |
| `http://服务器:3000` | 前台 |
| `http://服务器:3000/manage` | 管理后台 |
| `http://服务器:3000/api/provide/app` | 客户端软件源 |
| `http://服务器:3000/api/provide/tvbox` | TVBox / 影视仓 |
| `http://服务器:3000/api/provide/vod` | MacCMS |

`3000` 是默认 Web 端口，以终端打印的地址为准。账号 `admin`，密码是 `ECOHUB_ADMIN_PASSWORD`，没配就是 `admin`。登录后立刻修改。

数据在 `~/ecohub/data/mysql`、`data/redis`、`data/uploads`。

前台一开始没有影片。到采集中心核对 12 个预设站，执行全量采集。影片随窗口出现，首次可能要数小时。

生产环境把反代指到 `127.0.0.1:3000` 并开 HTTPS。不要把 `/api` 单独指到 `18080`。不想公网直连时，把端口写成 `127.0.0.1:${WEB_PORT:-3000}:3000`。播放器不直连就去掉 `18080`。防火墙只放行 `80` / `443`。

这套容器会出现在 1Panel 的容器列表里。继续用 `ecohub` 管理，不要再用同一组容器名建第二套编排。

## 1Panel

1. **容器** → **编排** → **创建**。工作目录例如 `/opt/1panel/apps/ecohub`。
2. 贴入 [deploy/release/compose.yml](../deploy/release/compose.yml) 全文。
3. 环境变量：

```env
ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.1
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

4. 保存并启动。确认 `Eco-hub`、`Eco-mysql`、`Eco-redis` 在运行。
5. **网站** → **反向代理** → `http://127.0.0.1:3000`。更新在该目录执行 `docker compose pull && docker compose up -d`。

## 手动部署

不用 Compose。先把密码换成强随机值。

```bash
mkdir -p ~/ecohub/data/mysql ~/ecohub/data/redis ~/ecohub/data/uploads
docker network inspect Eco-network >/dev/null 2>&1 || docker network create Eco-network

MYSQL_ROOT_PASSWORD='请改成强密码'
MYSQL_PASSWORD='请改成强密码'
REDIS_PASSWORD='请改成强密码'
JWT_SECRET="$(openssl rand -hex 32)"

docker pull mysql:8.4
docker pull redis:7.4-alpine
docker pull ghcr.io/fe-spark/ecohub:v3.0.1

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
  ghcr.io/fe-spark/ecohub:v3.0.1
```

容器内数据库主机名是 `Eco-mysql`、`Eco-redis`。自备库时不要启动上面两个数据库容器。MySQL 第一次初始化可能要几十秒，失败看 `docker logs Eco-mysql`。

日志：`docker logs -f Eco-hub`。更新：`docker pull` 当前 tag，删掉 `Eco-hub`，用同样参数再 `docker run`。

## 环境变量

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `ECOHUB_IMAGE` | `ghcr.io/fe-spark/ecohub:latest` | 镜像 tag。见文首 |
| `WEB_PORT` | `3000` | 前台、后台、订阅 |
| `SERVER_PORT` | `18080` | API 直连。订阅不用这个端口 |
| `JWT_SECRET` | 无，必填 | 登录签名 |
| `ECOHUB_ADMIN_PASSWORD` | `admin` | 只在首次创建管理员时写入 |
| `MYSQL_HOST` | `mysql` | 容器内不要写 `127.0.0.1` |
| `MYSQL_PORT` | `3306` | |
| `MYSQL_USER` | `eco` | |
| `MYSQL_PASSWORD` | `ecohub` | 只在数据目录首次初始化时生效 |
| `MYSQL_DBNAME` | `eco` | |
| `MYSQL_ROOT_PASSWORD` | `ecohub` | 只给内置 MySQL 第一次初始化 |
| `REDIS_HOST` | `redis` | |
| `REDIS_PORT` | `6379` | |
| `REDIS_PASSWORD` | `ecohub` | 改 `.env` 后 `ecohub restart` 生效 |
| `REDIS_DB` | `0` | |
| `HTTPS_PROXY` / `HTTP_PROXY` / `ALL_PROXY` | 空 | 采集出网代理 |
| `NODE_NAME` | 主机名 | 多节点才需要 |
| `ECOHUB_LOG_DIR` | `logs/` | 日志目录 |

## 排障

- 探活：`http://主机:3000/api/config/basic`。映射了 `18080` 时该端口也能探。
- 反复重启：`ecohub logs` 或 `docker logs Eco-hub`。核对 `JWT_SECRET` 和数据库密码是否仍是第一次初始化的值，端口是否被占用。
- 前台能开、接口失败：反代指向 Web 端口。发布镜像不要设 `API_URL`。

行为问题见 [常见问题](./README-FAQ.md)。

## 相关

[README](../README.md) · [版本说明](./RELEASE.md) · [从 v2 升级到 v3](./v2-to-v3.md)
