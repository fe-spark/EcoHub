# EcoHub 部署指南

中文 | [English](../en/guide/deploy.md)

EcoHub 以 All-in-One 镜像 `ghcr.io/fe-spark/ecohub` 发布：同一容器内运行 Go API（容器内端口 `8080`）和 Next.js 前端（容器内端口 `3000`），另需配合 MySQL 8 与 Redis 7 运行。`:latest` 跟随最新正式版，`-beta` 为测试版勿用于生产。

相关文件：[编排 deploy/release/compose.yml](../../deploy/release/compose.yml) · [变量模板 .env.example](../../.env.example) · [版本说明](../RELEASE.md)。从 v2 升级前，先阅读 [从 v2 升级到 v3](./v2-to-v3.md)。

## 准备

- Docker 20+ 和 Docker Compose（`docker compose` 插件或独立的 `docker-compose` 均可；使用 `docker run` 手动部署时不需要 Compose）。
- 建议 2 核 CPU、2 GB 内存及以上。
- 服务器能够访问 `ghcr.io`（应用镜像）和 `docker.io`（MySQL、Redis 镜像）。

对外端口：

| 端口 | 默认值 | 用途 |
| --- | --- | --- |
| Web 端口 | `3000` | 前台、后台和订阅地址均通过此端口访问 |
| API 直连端口 | `18080` | 映射到容器内 `8080`，直接访问后端 `/api/*`；订阅地址也可使用此端口，不需要时可移除 |

为 `JWT_SECRET` 生成随机密钥：

```bash
openssl rand -hex 32
```

## 方式一：安装脚本（推荐）

以下命令默认安装最新 v3 正式版；如需固定具体版本，可将 `:latest` 替换为 [版本说明](../RELEASE.md) 中的 v3 tag：

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:latest sh
```

脚本执行以下操作：

1. 在安装目录（默认 `~/ecohub`）中创建 `data/mysql`、`data/redis`、`data/uploads`。
2. 下载编排文件保存为 `docker-compose.yml`，并下载 `.env.example`；不存在 `.env` 时由模板生成，已存在时保留，不创建或修改 `docker-compose.override.yml`。
3. 将写在 `sh` 前的变量写入 `.env`；全新安装且未指定镜像时，写入脚本内置的默认 v3 镜像 tag。
4. 将 `ecohub` 命令安装到 `/usr/local/bin`（不可写时安装到 `~/.local/bin` 并提示加入 `PATH`），同时将安装目录写入 `~/.ecohub/install-dir`。
5. 启动容器，并输出前台、后台地址、初始账号和三条订阅地址。

### 第一次安装就设好密码

变量必须写在 `sh` 之前；写在 `curl` 之前的变量不会传入脚本：

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:latest \
    JWT_SECRET="$(openssl rand -hex 32)" \
    ECOHUB_ADMIN_PASSWORD='请改成强密码' \
    MYSQL_ROOT_PASSWORD='请改成强密码' \
    MYSQL_PASSWORD='请改成强密码' \
    REDIS_PASSWORD='请改成强密码' \
    sh
```

`install` 和 `update` 只会将以下 8 个变量写入 `.env`：`ECOHUB_IMAGE`、`WEB_PORT`、`SERVER_PORT`、`JWT_SECRET`、`ECOHUB_ADMIN_PASSWORD`、`MYSQL_ROOT_PASSWORD`、`MYSQL_PASSWORD`、`REDIS_PASSWORD`；其余自定义行会保留。未传入密码时使用公开模板默认值（管理员密码 `admin`、数据库密码 `ecohub`），公网开放前务必修改。

如需更换安装目录，将 `ECOHUB_INSTALL_DIR=~/ecohub-v3` 一并写在 `sh` 之前。日常运维命令（`ecohub update`、`ecohub logs`、修改密码生效规则等）见 [运维与配置](./operations.md)。

## 方式二：1Panel

1. 在 1Panel 中新建 Compose 编排，工作目录示例：`/opt/1panel/apps/ecohub`。
2. 粘贴 [deploy/release/compose.yml](../../deploy/release/compose.yml) 的全部内容。
3. 填写环境变量：

```env
ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:latest
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

更新时修改 `ECOHUB_IMAGE` 后，在工作目录执行：

```bash
docker compose pull && docker compose up -d
```

## 方式三：手动 docker run

此方式不使用 Compose。执行前请将以下密码替换为强随机值：

```bash
mkdir -p ~/ecohub/data/mysql ~/ecohub/data/redis ~/ecohub/data/uploads
docker network inspect Eco-network >/dev/null 2>&1 || docker network create Eco-network

MYSQL_ROOT_PASSWORD='请改成强密码'
MYSQL_PASSWORD='请改成强密码'
REDIS_PASSWORD='请改成强密码'
JWT_SECRET="$(openssl rand -hex 32)"

docker pull mysql:8.4
docker pull redis:7.4-alpine
docker pull ghcr.io/fe-spark/ecohub:latest

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
  ghcr.io/fe-spark/ecohub:latest
```

- 应用容器中的数据库主机名为容器名 `Eco-mysql`、`Eco-redis`。
- `until` 循环用于等待 MySQL 首次初始化完成；长时间未完成时查看 `docker logs Eco-mysql`。
- 挂载 `/var/run/docker.sock` 供后台「立即升级并重启」使用，不用该功能时可以移除。

查看日志：`docker logs -f Eco-hub`。更新方式：`docker pull` 新 tag、`docker rm -f Eco-hub`，再使用相同参数与新 tag 重新启动。

## 访问地址

| 地址 | 用途 |
| --- | --- |
| `http://<服务器IP>:3000` | 前台 |
| `http://<服务器IP>:3000/manage` | 管理后台 |
| `http://<服务器IP>:3000/api/provide/app` | EcoHub 客户端订阅 |
| `http://<服务器IP>:3000/api/provide/tvbox` | TVBox / 影视仓订阅 |
| `http://<服务器IP>:3000/api/provide/vod` | MacCMS 订阅 |

订阅地址也可使用 `18080` 直连端口（如 `http://<服务器IP>:18080/api/provide/tvbox`），响应体中的回调地址会按请求地址自动生成；前台和后台页面仅通过 Web 端口提供。

后台初始账号为 `admin`，密码由首次启动时的 `ECOHUB_ADMIN_PASSWORD` 决定（默认 `admin`），首次登录后请立即修改。开启「私有化访问」后的订阅写法见 [常见问题](./faq.md#开启私有化访问后订阅怎么填)。

首次打开时前台为空。在后台「采集中心」核对 12 个预设采集站并执行一次全量采集，影片会随采集进度陆续出现。

## 反向代理

生产环境建议：

- 反向代理将整个站点指向 `127.0.0.1:3000`，并启用 HTTPS；`/api` 无需单独转发到 `18080`，Web 端口会将 `/api/*` 转发给容器内的 Go 进程。
- 如不希望公网直接访问 `3000` 或不使用 `18080`：使用安装脚本部署时通过 [自定义编排](./operations.md#自定义编排) 覆盖 `ports`；自行维护编排的直接修改 `ports`。
- 云服务器安全组仅放行 `80`、`443`。

## 相关文档

[运维与配置](./operations.md) · [从 v2 升级到 v3](./v2-to-v3.md) · [常见问题](./faq.md) · [版本说明](../RELEASE.md)
