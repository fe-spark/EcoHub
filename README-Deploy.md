# EcoHub 部署指南

发布镜像 `ghcr.io/fe-spark/ecohub`（All-in-One：同容器 Supervisord 托管 Go API `:8080` 与 Next.js `:3000`）。普通用户用 **安装脚本** 或 **1Panel** 即可。

> Compose：[deploy/release/compose.yml](./deploy/release/compose.yml) · 环境变量模板：[.env.example](./.env.example) · 版本：[RELEASE.md](./RELEASE.md)

旧的 `ecohub-web` / `ecohub-server` 双镜像已废弃（v2.0+）。

---

## 方式选择

| 场景 | 章节 |
| --- | --- |
| 命令行一键 | [安装脚本](#方式-a安装脚本推荐) |
| 1Panel 图形化 | [1Panel](#方式-b1panel) |
| 改源码本地构建 | [源码版 Compose](#方式-c源码版-compose) |
| 本机开发 | [README.md](./README.md)「本地开发」 |

自备 MySQL / Redis 时：在默认 compose 中去掉 `mysql` / `redis` 服务及 `depends_on`，把 `.env` 的 `MYSQL_*` / `REDIS_*` 指到你的实例（容器内勿用 `127.0.0.1`，宿主机库可用 `host.docker.internal`）。变量含义见 [server/README.md](./server/README.md)。

---

## 前置条件

| 项 | 说明 |
| --- | --- |
| Docker | 20+，Compose 2+（1Panel 自带即可） |
| 网络 | 能拉 `ghcr.io`、`docker.io`（国内可配镜像加速） |
| 端口 | 至少空闲 Web 端口（默认 `3000`） |
| 资源 | 建议 ≥ 2 核 2G；采集任务多时适当加内存 |

正式上线前至少修改：`JWT_SECRET`、MySQL/Redis 密码。

```bash
openssl rand -hex 32
```

可选：`TG_PROXY`、`HTTPS_PROXY` / `HTTP_PROXY` / `ALL_PROXY`、`COLLECT_PROFILE`（`auto|light|standard|high`）。详见 `.env.example`。

---

## 方式 A：安装脚本（推荐）

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

首次需在后台配置 **采集源** 并执行采集，否则前台无影片。

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

固定版本：compose 中改为 `ghcr.io/fe-spark/ecohub:v2.0.1` 等。正式版 tag 会覆盖 `:latest`。发布版后台可点「立即升级并重启」（compose 需挂载 `/var/run/docker.sock`，新 compose 已包含）。挂载 socket 等于容器内进程可操作宿主机 Docker，仅可写账号能触发升级；不需要在线升级可去掉该卷。

### 5. 从 v1.x 升级

1. 备份 `data/`。  
2. 停掉旧栈（v1 的 `ecohub-web` / `ecohub-server` 等），换成 All-in-One 发布版 compose（`ghcr.io/fe-spark/ecohub`）。  
3. `.env` 指向同一库后 `pull` + `up -d`。  
4. 禁止新旧 server 混连同一库。

#### 对齐数据（建议）

v1 旧库存到 v2 后，**建议重置站点数据并全量采集一次**，让 ContentKey、快照、分类树与多源播放列表按 v2 规则重新对齐，避免残留脏数据导致匹配偏差或列表不一致。

- 管理后台 → **重置站点数据**（或等价清空业务表）  
- 重新配置 / 确认采集源（主站 + 附属站）→ 执行 **全量采集**（不要只跑增量）  
- 用户上传素材（`data/uploads`）可按需保留；影片主数据与播放源以本次全量采集结果为准  

若不重置，多数场景仍可直接跑 v2，但数据可能未完全按新模型对齐，出现问题再按上面步骤处理即可。

---

## 方式 B：1Panel

### 1. 新建编排

1. **容器** → **编排** → **创建**，名称如 `ecohub`。  
2. 工作目录例如 `/opt/1panel/apps/ecohub`。  
3. 粘贴与 [deploy/release/compose.yml](./deploy/release/compose.yml) 一致的内容：

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

保存 → 启动；确认 `Eco-hub`、`Eco-mysql`、`Eco-redis` 运行中。访问同方式 A。

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

## 方式 C：源码版 Compose

适合开发或自行构建。根目录 [docker-compose.yml](./docker-compose.yml)（`web` + `server` 本地 build）。**生产请用 All-in-One 发布版。**

```bash
cp .env.example .env
# 修改 JWT_SECRET、密码等
docker compose up --build -d
```

访问端口与发布版相同。源码版 `API_URL` 由 compose 注入；发布版不需要配。

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

```bash
# 发布版
docker compose ps
docker compose logs -f ecohub
docker compose restart ecohub
docker compose down

# 源码版
docker compose logs -f web
docker compose logs -f server
```

发布版数据在安装目录 `data/`；源码版默认 Docker volume（`down -v` 会删）。

---

## 排障

- 探活：`http://主机:18080/api/health`  
- 反复重启：查 `.env` 密码、`JWT_SECRET`、端口占用、`docker pull ghcr.io/fe-spark/ecohub:latest`  
- 前台开、接口挂：反代是否只指 Web、是否误配 `API_URL`  
- Telegram 发不出：配 `TG_PROXY`；Token 在后台配置  

更多：[README-FAQ.md](./README-FAQ.md)

---

## 安全

- 立刻改默认账号密码  
- 每环境单独 `JWT_SECRET`  
- 勿提交生产密码  
- 优先 HTTPS；勿公网暴露库与 `18080`  

---

## 相关文档

- [README.md](./README.md) · [RELEASE.md](./RELEASE.md) · [server/README.md](./server/README.md) · [web/README.md](./web/README.md) · [README-FAQ.md](./README-FAQ.md)
