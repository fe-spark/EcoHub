# EcoHub 部署指南

中文 | [English](./README-Deploy_EN.md)

发布镜像 `ghcr.io/fe-spark/ecohub`（All-in-One：同容器 Supervisord 托管 Go API `:8080` 与 Next.js `:3000`）。可选用 **安装脚本**、**1Panel** 或 **手动部署**。

> Compose：[deploy/release/compose.yml](../deploy/release/compose.yml) · 环境变量模板：[.env.example](../.env.example) · 版本：[RELEASE.md](./RELEASE.md)

旧的 `ecohub-web` / `ecohub-server` 双镜像已废弃（v2.0+）。

---

## 方式选择

| 场景 | 章节 |
| --- | --- |
| 命令行一键 | [安装脚本](#方式-1安装脚本推荐) |
| 1Panel 图形化 | [1Panel](#方式-21panel) |
| 直接运行镜像 | [手动部署](#方式-3手动部署) |

自备 MySQL / Redis 时：在默认 compose 中去掉 `mysql` / `redis` 服务及 `depends_on`，把 `.env` 的 `MYSQL_*` / `REDIS_*` 指到你的实例（容器内勿用 `127.0.0.1`，宿主机库可用 `host.docker.internal`）。变量含义见 `.env.example`。

---

## 前置条件

| 项 | 说明 |
| --- | --- |
| Docker | 20+；方式 1 / 2 还需 Compose 2+（1Panel 自带） |
| 网络 | 能拉 `ghcr.io`、`docker.io`（国内可配镜像加速） |
| 端口 | 至少空闲 Web 端口（默认 `3000`） |
| 资源 | 建议 ≥ 2 核 2G；采集任务多时适当加内存 |

正式上线前至少修改：`JWT_SECRET`、MySQL/Redis 密码。

```bash
openssl rand -hex 32
```

可选：`TG_PROXY`、`HTTPS_PROXY` / `HTTP_PROXY` / `ALL_PROXY`、`COLLECT_PROFILE`（`auto|light|standard|high`）。详见 `.env.example`。

---

## 方式 1：安装脚本（推荐）

默认三个容器：`Eco-hub`（应用）、`Eco-mysql`、`Eco-redis`。

### 1. 安装

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
```

写入 `~/ecohub`：下载 `docker-compose.yml`（来自 `deploy/release/compose.yml`）、`.env.example`；无 `.env` 时自动复制一份。

### 2. 改配置并启动

```bash
cd ~/ecohub
# 编辑 .env：JWT_SECRET、MYSQL_*、REDIS_PASSWORD 等
docker compose up -d
```

内置库时保持 `MYSQL_HOST=mysql`、`REDIS_HOST=redis`（Compose 服务名）。

### 3. 访问

| 地址 | 说明 |
| --- | --- |
| `http://服务器:3000` | 前台 |
| `http://服务器:3000/manage` | 管理后台 |
| `http://服务器:3000/api/*` | 经站点转发的 API |
| `http://服务器:18080/api/*` | 后端直连（生产勿公网暴露） |
| `http://服务器:3000/api/provide/config` | TVBox / 影视仓 |

默认账号（**立刻改密**）：`admin` / `admin`，`guest` / `guest`。

首次需在后台配置 **采集源** 并执行采集，否则前台无影片。第一次全量采集**可能要数个小时**，**采集完成并发布后数据才会展示**。

### 4. 数据与更新

```text
~/ecohub/data/mysql
~/ecohub/data/redis
~/ecohub/data/uploads
```

```bash
cd ~/ecohub
docker compose pull
docker compose up -d
```

固定版本：compose 中改为 `ghcr.io/fe-spark/ecohub:v2.0.1` 等。正式版 tag 会覆盖 `:latest`。发布版后台可点「立即升级并重启」（compose 需挂载 `/var/run/docker.sock`，新 compose 已包含）。挂载 socket 等于容器内进程可操作宿主机 Docker，仅超级管理员能触发升级；不需要在线升级可去掉该卷。

---

## 方式 2：1Panel

### 1. 新建编排

1. **容器** → **编排** → **创建**，名称如 `ecohub`。  
2. 工作目录例如 `/opt/1panel/apps/ecohub`。  
3. 粘贴与 [deploy/release/compose.yml](../deploy/release/compose.yml) 一致的内容：

```yaml
services:
  mysql:
    container_name: Eco-mysql
    image: mysql:8.4
    restart: always
    environment:
      MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD:-ecohub}
      MYSQL_DATABASE: ${MYSQL_DBNAME:-eco}
      MYSQL_USER: ${MYSQL_USER:-eco}
      MYSQL_PASSWORD: ${MYSQL_PASSWORD:-ecohub}
    volumes:
      - ./data/mysql:/var/lib/mysql
    networks:
      - Eco-network
    healthcheck:
      test:
        [
          "CMD-SHELL",
          "mysqladmin ping -h 127.0.0.1 -uroot -p$$MYSQL_ROOT_PASSWORD --silent",
        ]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 30s

  redis:
    container_name: Eco-redis
    image: redis:7.4-alpine
    restart: always
    environment:
      REDIS_PASSWORD: ${REDIS_PASSWORD:-ecohub}
    command: ["redis-server", "--requirepass", "${REDIS_PASSWORD:-ecohub}"]
    volumes:
      - ./data/redis:/data
    networks:
      - Eco-network
    healthcheck:
      test: ["CMD-SHELL", "redis-cli -a $${REDIS_PASSWORD} ping | grep PONG"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 10s

  ecohub:
    container_name: Eco-hub
    image: ghcr.io/fe-spark/ecohub:latest
    restart: always
    environment:
      PORT: ${SERVER_PORT:-8080}
      JWT_SECRET: ${JWT_SECRET:-ecohub_2026!local@dev_secret$$001}
      MYSQL_HOST: ${MYSQL_HOST:-mysql}
      MYSQL_PORT: ${MYSQL_PORT:-3306}
      MYSQL_USER: ${MYSQL_USER:-eco}
      MYSQL_PASSWORD: ${MYSQL_PASSWORD:-ecohub}
      MYSQL_DBNAME: ${MYSQL_DBNAME:-eco}
      REDIS_HOST: ${REDIS_HOST:-redis}
      REDIS_PORT: ${REDIS_PORT:-6379}
      REDIS_PASSWORD: ${REDIS_PASSWORD:-ecohub}
      REDIS_DB: ${REDIS_DB:-0}
      TG_PROXY: ${TG_PROXY:-}
      HTTPS_PROXY: ${HTTPS_PROXY:-}
      HTTP_PROXY: ${HTTP_PROXY:-}
      ALL_PROXY: ${ALL_PROXY:-}
      COLLECT_PROFILE: ${COLLECT_PROFILE:-auto}
    ports:
      - ${WEB_PUBLIC_PORT:-3000}:3000
      - 0.0.0.0:${SERVER_PUBLIC_PORT:-18080}:${SERVER_PORT:-8080}
    volumes:
      - ./data/uploads:/app/static/upload
      - /var/run/docker.sock:/var/run/docker.sock
    networks:
      - Eco-network
    extra_hosts:
      - "host.docker.internal:host-gateway"
    depends_on:
      mysql:
        condition: service_healthy
      redis:
        condition: service_healthy
    healthcheck:
      test:
        ["CMD-SHELL", "wget -q -O /dev/null http://localhost:8080/api/health"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 30s

networks:
  Eco-network:
    driver: bridge
```

### 2. 环境变量

```env
WEB_PUBLIC_PORT=3000
SERVER_PUBLIC_PORT=18080
SERVER_PORT=8080

JWT_SECRET=请替换为长随机串

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

- 内置库：`MYSQL_HOST=mysql` / `REDIS_HOST=redis`，不要写成 `127.0.0.1`。  
- 发布版 **无需** `API_URL`。  
- 端口占用时改 `WEB_PUBLIC_PORT`，反代同步改。

### 3. 启动

保存 → 启动；确认 `Eco-hub`、`Eco-mysql`、`Eco-redis` 运行中。访问同方式 1。

也可先跑安装脚本，再在 1Panel 容器列表中查看/接管。

### 4. 网站与 HTTPS

1. **网站** → **创建网站** → **反向代理** → `http://127.0.0.1:3000`（或你的 `WEB_PUBLIC_PORT`）。  
2. 申请 Let's Encrypt 或上传证书，开启 HTTPS。  
3. `/api/*` **不要**单独指到 `18080`；整站走 `3000`，由容器内 Next 转发到 Go。  
4. 防火墙放行 `80`/`443`；**不要**公网放行 `18080`、MySQL、Redis。

### 5. 更新

编排中改镜像 tag（可选）→ 拉取 → 重建，或：

```bash
docker compose pull && docker compose up -d
```

数据在 `./data/mysql`、`./data/redis`、`./data/uploads`。

---

## 方式 3：手动部署

不使用 Compose。自行创建网络与数据目录，依次运行 MySQL、Redis 与 `ghcr.io/fe-spark/ecohub`。访问地址与默认账号同方式 1。

将下面的密码与密钥换成强随机值后再执行。

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

容器内访问数据库须用容器名 `Eco-mysql` / `Eco-redis`，不要写 `127.0.0.1`。自备库时不要创建上述 MySQL / Redis 容器，把 `MYSQL_HOST` / `REDIS_HOST` 改成可达地址（宿主机上的库可用 `host.docker.internal`）。首次初始化 MySQL 数据目录可能需要数十秒，就绪探测失败时查看 `docker logs Eco-mysql`。

查看日志：`docker logs -f Eco-hub`。更新镜像：`docker pull ghcr.io/fe-spark/ecohub:latest` 后删除并按上面的参数重新 `docker run` Eco-hub（或使用后台「立即升级并重启」，需挂载 `/var/run/docker.sock`）。

---

## 端口

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `WEB_PUBLIC_PORT` | `3000` | 前台与后台入口 |
| `SERVER_PUBLIC_PORT` | `18080` | 后端直连映射 |
| `SERVER_PORT` | `8080` | 容器内 Go 监听（注入为 `PORT`） |

生产建议只暴露 Web 端口（或反代后的 80/443）。

---

## 常用命令

方式 1 / 2：

```bash
docker compose ps
docker compose logs -f ecohub
docker compose restart ecohub
docker compose down
```

方式 3：

```bash
docker logs -f Eco-hub
docker restart Eco-hub
docker inspect Eco-hub --format '{{range .Config.Env}}{{println .}}{{end}}'
```

数据在安装目录 `data/`（方式 3 默认 `~/ecohub/data`）。

---

## 排障

- 探活：`http://主机:18080/api/health`
- 方式 1 / 2 反复重启：查 `.env` 密码、`JWT_SECRET`、端口占用、`docker pull ghcr.io/fe-spark/ecohub:latest`
- 方式 3 反复重启：`docker logs Eco-hub`，用 `docker inspect Eco-hub` 核对环境变量与端口
- 前台开、接口挂：反代是否只指 Web、是否误配 `API_URL`
- Telegram 发不出：方式 1 / 2 在 `.env` 配 `TG_PROXY`；方式 3 在 `docker run` 增加 `-e TG_PROXY=...`；Token 在后台配置  

更多：[README-FAQ.md](./README-FAQ.md)

---

## 安全

- 立刻改默认账号密码  
- 每环境单独 `JWT_SECRET`  
- 勿提交生产密码  
- 优先 HTTPS；勿公网暴露库与 `18080`  

---

## 相关文档

- [README.md](../README.md) · [RELEASE.md](./RELEASE.md) · [README-FAQ.md](./README-FAQ.md) · [English](./README-Deploy_EN.md)
