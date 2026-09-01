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
      - ${WEB_PORT:-3000}:3000
      - 0.0.0.0:${SERVER_PORT:-18080}:8080
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
WEB_PORT=3000
SERVER_PORT=18080

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
- 端口占用时改 `WEB_PORT`，反代同步改。

### 3. 启动

保存 → 启动；确认 `Eco-hub`、`Eco-mysql`、`Eco-redis` 运行中。访问同方式 1。

也可先跑安装脚本，再在 1Panel 容器列表中查看/接管。

### 4. 网站、HTTPS 与不对外开放裸端口

1. **网站** → **创建网站** → **反向代理** → `http://127.0.0.1:3000`（或你的 `WEB_PORT`）。  
2. 申请 Let's Encrypt 或上传证书，开启 HTTPS。  
3. `/api/*` **不要**单独指到 `18080`；整站走 `3000`，由容器内 Next 转发到 Go。  
4. **不对外开放裸端口（安全推荐）**：
   - Docker 默认规则会穿透系统防火墙（如 UFW）。若要**禁止公网直接访问 3000 端口**，请在 `compose.yml` 中显式指定 `127.0.0.1:` 绑定（如 `127.0.0.1:${WEB_PORT:-3000}:3000`），此时公网无法直连，仅本机反向代理可用；
   - 若无播放器直连需求，直接在 `compose.yml` 中注释或删除 `18080:8080` 端口映射；
   - 防火墙放行 `80`/`443`；**不要**公网放行 `18080`、MySQL、Redis。

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
| `WEB_PORT` | `3000` | 宿主机 Web 访问入口端口 |
| `SERVER_PORT` | `18080` | 宿主机后端直连映射端口（可选） |

生产建议只暴露 Web 端口（或反代后的 80/443）。如需**完全禁止外部直连裸端口**，请在 `compose.yml` 中将端口映射写为 `127.0.0.1:${WEB_PORT:-3000}:3000`，使端口仅限本机及 1Panel / Nginx 反向代理访问。

---

## 集群部署与多节点（CLUSTER_ROLE）

多台 VPS 搭建集群以分流抗高并发时，可通过环境变量指定节点角色：
- `CLUSTER_ROLE=master`（默认）：主控节点，同时运行 Next.js Web 与 Go 后端，负责后台管理、写库及定时采集任务；
- `CLUSTER_ROLE=worker`：从属读节点，**专职作为纯 Go API 节点运行（自动跳过 Next.js Web 进程以节约大量内存，同时自动禁用定时采集调度器）**，专为 TVBox、影视仓、MacCMS 与前台 API 提供海量高并发读服务；
- **反向代理（Nginx）最佳实践**：
  - 前台网页浏览（`/`）与管理后台（`/manage`、`/api/manage/*`）固定打到 Master 节点（后台素材上传已收敛在 `/api/manage/file/upload`，已被该规则自然覆盖）；
  - 高并发只读 API 与海报静态图片读请求（`/api/`，含 `/api/upload/pic/poster/`）分流到集群负载均衡（由 Master 与多个 Worker 共同抗压）；
  - **Nginx 配置示例**：
    ```nginx
    upstream eco_cluster_api {
        server 192.168.1.10:8080 weight=1; # Master 节点 API
        server 192.168.1.11:8080 weight=2; # Worker 1 节点 API
        server 192.168.1.12:8080 weight=2; # Worker 2 节点 API
    }

    upstream eco_master_web {
        server 192.168.1.10:3000;          # Master Web 页面
    }

    upstream eco_master_api {
        server 192.168.1.10:8080;          # Master API（写操作与管理后台）
    }

    server {
        listen 80;
        server_name your-domain.com;

        # 管理后台前端页面固定路由至 Master
        location /manage {
            proxy_pass http://eco_master_web;
        }

        # 管理后台写/改/配置/上传接口固定路由至 Master
        location /api/manage/ {
            proxy_pass http://eco_master_api;
        }

        # 高并发前台只读 API 及海报静态素材（/api/upload/pic/poster/）由 Master 与 Worker 集群共同分流负载
        location /api/ {
            proxy_pass http://eco_cluster_api;
        }

        # 默认网页浏览请求打到 Master Web
        location / {
            proxy_pass http://eco_master_web;
        }
    }
    ```
- **部署顺序与数据同步注意事项**：
  - **先启动 Master，再扩容 Worker**：Worker 启动后每 3s 轮询 Redis 中的快照版本/修订号自动对齐 Master 的读模型与搜索索引；若 Master 尚未完成首次快照发布，Worker 会在首个快照发布后自动装载，无需重启。
  - **共享持久化数据卷**：快照表与静态资源存放在数据库/共享磁盘，Worker 必须能读取 Master 写入的快照数据（`film_list_snapshot` 等），建议将 `data/` 挂载为共享存储（如 NFS / 云盘），不要为 Worker 单独建空库。
  - **Worker 为应用层只读节点**：Worker 上 `/api/manage/*`（含上传）等写接口会被后端直接拒绝（HTTP 403），反向代理路由仅是第一层约束，双重防护避免误写。

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
