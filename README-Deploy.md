# EcoHub 部署指南

本文集中说明 **所有部署方式**。普通用户推荐 **方式 A（安装脚本）** 或 **方式 B（1Panel）**，均使用发布镜像 `ghcr.io/fe-spark/ecohub`（All-in-One：同容器 Supervisord 托管 Go API `:8080` 与 Next.js `:3000`）。

> Compose 源文件：[deploy/release/compose.yml](./deploy/release/compose.yml)  
> 环境变量模板：[.env.example](./.env.example)  
> 版本变更：[RELEASE.md](./RELEASE.md)

旧的 `ecohub-web` / `ecohub-server` 双镜像已废弃（v2.0+）。

---

## 方式选择

| 场景 | 方式 | 说明 |
| --- | --- | --- |
| 命令行一键跑起来 | [A. 安装脚本](#方式-a安装脚本推荐) | `install-release.sh` → `~/ecohub`，内置 MySQL / Redis |
| 1Panel 图形化 | [B. 1Panel 内置库](#方式-b1panel--内置-mysql--redis) | 编排粘贴三服务 compose + 网站反代 |
| 1Panel + 面板已有库 | [C. 1Panel 外部库](#方式-c1panel--已有-mysql--redis) | 仅 `ecohub` 服务，连 1Panel MySQL/Redis |
| 命令行 + 自有库 | [D. 外部数据库](#方式-d命令行--外部-mysql--redis) | 改 `.env`，compose 去掉 mysql/redis |
| 改源码本地构建 | [E. 源码版 Compose](#方式-e源码版-compose) | 根目录 `docker-compose.yml`（web + server 拆分） |
| 本机开发 | 见 [README.md](./README.md)「本地开发」 | `go run` + `npm run dev`，不强制 Docker |

---

## 前置条件

| 项 | 说明 |
| --- | --- |
| Docker | 20+，Compose 2+（1Panel 自带即可） |
| 网络 | 能拉 `ghcr.io`、`docker.io`（国内可配镜像加速） |
| 端口 | 至少空闲 Web 端口（默认 `3000`） |
| 资源 | 建议 ≥ 2 核 2G；采集任务多时适当加内存 |

正式上线前至少修改：`JWT_SECRET`、MySQL/Redis 密码。生成密钥：

```bash
openssl rand -hex 32
```

可选环境变量（见 `.env.example` / [server/README.md](./server/README.md)）：

- `TG_PROXY`：Telegram 专用代理  
- `HTTPS_PROXY` / `HTTP_PROXY` / `ALL_PROXY`：采集等通用代理  
- `COLLECT_PROFILE`：`auto|light|standard|high`（默认 `auto`）

---

## 方式 A：安装脚本（推荐）

发布版默认三个容器：

| 容器 | 作用 | 镜像 |
| --- | --- | --- |
| `Eco-hub` | 前台、后台、API、采集、鉴权、开放接口 | `ghcr.io/fe-spark/ecohub:latest` |
| `Eco-mysql` | 内置 MySQL | `mysql:8.4` |
| `Eco-redis` | 内置 Redis | `redis:7.4-alpine` |

### 1. 下载安装文件

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
```

默认写入 `~/ecohub`，生成 `docker-compose.yml`。若无 `.env`，会从仓库 `.env.example` 复制。

### 2. 修改配置

```bash
cd ~/ecohub
# 编辑 .env：JWT_SECRET、MYSQL_*、REDIS_PASSWORD 等
```

内置库时：`MYSQL_HOST=mysql`、`REDIS_HOST=redis`（Compose **服务名**），不要写成 `127.0.0.1`。

### 3. 启动

```bash
cd ~/ecohub
docker compose up -d
```

### 4. 访问

| 地址 | 说明 |
| --- | --- |
| `http://服务器:3000` | 前台 |
| `http://服务器:3000/manage` | 管理后台 |
| `http://服务器:3000/api/*` | 经站点转发的 API |
| `http://服务器:18080/api/*` | 后端直连（生产不建议公网暴露） |
| `http://服务器:3000/api/provide/config` | TVBox / 影视仓 |

默认账号（**登录后立刻改密**）：`admin` / `admin`，`guest` / `guest`。

首次需在后台配置 **采集源** 并执行采集，否则前台无影片。

### 5. 数据目录

```text
~/ecohub/data/mysql
~/ecohub/data/redis
~/ecohub/data/uploads
```

删除后数据库、缓存与上传会丢失。

### 6. 更新

```bash
cd ~/ecohub
docker compose pull
docker compose up -d
```

固定版本可把镜像改为例如 `ghcr.io/fe-spark/ecohub:v2.0.1`。正式版 tag 会同步覆盖 `:latest`。

### 7. 从 v1.x 双镜像升级

1. 备份 `data/`（或旧 volume）。  
2. 用安装脚本或手动换成当前 [deploy/release/compose.yml](./deploy/release/compose.yml)（单服务 `ecohub`）。  
3. `.env` 中 JWT / MySQL / Redis 与旧库一致。  
4. `docker compose pull && docker compose up -d`。  

主站 `ContentKey` 等由 server 启动时自动处理；禁止新旧版本混连同一库。

---

## 方式 B：1Panel + 内置 MySQL / Redis

适合 1Panel **容器 → 编排**（或「Compose」）新建项目。不需要本机装 Go / Node。

### 1. 新建编排

1. 1Panel → **容器** → **编排**。  
2. **创建编排**，名称建议 `ecohub`。  
3. 工作目录例如：`/opt/1panel/apps/ecohub`（以面板为准）。

### 2. 粘贴 Compose

与仓库 [deploy/release/compose.yml](./deploy/release/compose.yml) 一致：

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

### 3. 环境变量（`.env`）

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

# 可选
# TG_PROXY=http://host.docker.internal:7890
# COLLECT_PROFILE=auto
```

- `MYSQL_HOST=mysql` / `REDIS_HOST=redis` 是服务名，不要改成 `127.0.0.1`。  
- 宿主机 `3000` 占用时改 `WEB_PUBLIC_PORT`，网站反代同步改。  
- 发布版 **无需** 配置 `API_URL`（镜像内默认同容器 `http://127.0.0.1:8080`）。

### 4. 启动与验证

1. 保存 → **启动**。  
2. 确认 `Eco-hub`、`Eco-mysql`、`Eco-redis` 均为运行中。  
3. 访问同 [方式 A 访问表](#4-访问)。  
4. 网站 HTTPS 见下文 [网站与 HTTPS](#网站与-https1panel--nginx-等)。

也可先脚本安装再在 1Panel 容器列表中接管（数据目录默认 `~/ecohub/data/`）。

### 5. 1Panel 更新

1. 打开 `ecohub` 编排；固定版本时改镜像 tag。  
2. **拉取镜像** → **重建 / 重启**，或：

```bash
docker compose pull && docker compose up -d
```

数据在编排目录 `./data/mysql`、`./data/redis`、`./data/uploads`；升级一般不必删 data。

---

## 方式 C：1Panel + 已有 MySQL / Redis

编排里 **只跑 `ecohub`**，把 `MYSQL_*` / `REDIS_*` 指到 1Panel 已安装实例。

### 1. 准备数据库

1. 1Panel 装好 **MySQL 8**、**Redis 7**（或已有实例）。  
2. MySQL 建库（如 `eco`）与业务用户并授权。  
3. 记下主机/端口/用户/密码。

| 场景 | `MYSQL_HOST` / `REDIS_HOST` |
| --- | --- |
| 与 EcoHub 同一 Docker 网络 | 库的 **容器名** |
| 已映射到宿主机端口 | `host.docker.internal`（compose 已配 `extra_hosts`） |
| 远程 / 固定 IP | 内网 IP 或域名 |

**不要**用 `127.0.0.1`（容器内指自己）。

### 2. 粘贴 Compose（仅 ecohub）

```yaml
services:
  ecohub:
    container_name: Eco-hub
    image: ghcr.io/fe-spark/ecohub:latest
    restart: always
    environment:
      PORT: ${SERVER_PORT:-8080}
      JWT_SECRET: ${JWT_SECRET:-ecohub_2026!local@dev_secret$$001}
      MYSQL_HOST: ${MYSQL_HOST}
      MYSQL_PORT: ${MYSQL_PORT:-3306}
      MYSQL_USER: ${MYSQL_USER}
      MYSQL_PASSWORD: ${MYSQL_PASSWORD}
      MYSQL_DBNAME: ${MYSQL_DBNAME:-eco}
      REDIS_HOST: ${REDIS_HOST}
      REDIS_PORT: ${REDIS_PORT:-6379}
      REDIS_PASSWORD: ${REDIS_PASSWORD:-}
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
    networks:
      - Eco-network
    extra_hosts:
      - "host.docker.internal:host-gateway"
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

挂载 1Panel 已有网络（网络名以面板为准，常见类似 `1panel-network`）：

```yaml
networks:
  Eco-network:
    external: true
    name: 1panel-network
```

此时 `MYSQL_HOST` / `REDIS_HOST` 可填库的 **容器名**。

### 3. 环境变量

```env
WEB_PUBLIC_PORT=3000
SERVER_PUBLIC_PORT=18080
SERVER_PORT=8080

JWT_SECRET=请替换为长随机串

MYSQL_HOST=host.docker.internal
MYSQL_PORT=3306
MYSQL_USER=eco
MYSQL_PASSWORD=请改成强密码
MYSQL_DBNAME=eco

REDIS_HOST=host.docker.internal
REDIS_PORT=6379
REDIS_PASSWORD=请改成强密码
REDIS_DB=0
```

- 无内置库时 **不需要** `MYSQL_ROOT_PASSWORD`。  
- 仅 `./data/uploads` 由本编排持久化；库数据在 1Panel 侧管理。

### 4. 启动

1. 保存 → 启动，应只有 `Eco-hub`。  
2. 连不上：看 `docker logs Eco-hub`（host/端口、网络、MySQL 是否允许 Docker 网段访问）。

---

## 方式 D：命令行 + 外部 MySQL / Redis

1. 安装脚本或手动使用 [deploy/release/compose.yml](./deploy/release/compose.yml)。  
2. **删除** compose 中 `mysql`、`redis` 服务及 `ecohub.depends_on`（或改用方式 C 的单服务 yaml）。  
3. `.env` 示例：

```env
MYSQL_HOST=host.docker.internal
MYSQL_PORT=3306
MYSQL_USER=your_mysql_user
MYSQL_PASSWORD=your_mysql_password
MYSQL_DBNAME=your_mysql_db

REDIS_HOST=host.docker.internal
REDIS_PORT=6379
REDIS_PASSWORD=your_redis_password
REDIS_DB=0
```

4. `docker compose up -d`（仅启动 `ecohub`）。

地址建议：

- 库在宿主机：`host.docker.internal`（仓库 compose 已配 `extra_hosts`）  
- 库在其他机器：真实 IP / 域名  
- Redis 无密码：`REDIS_PASSWORD` 留空  

---

## 方式 E：源码版 Compose

适合开发或自行构建。使用仓库根目录 [docker-compose.yml](./docker-compose.yml)（`web` + `server` 两服务本地 build；**生产发布请用 All-in-One**）。

```bash
cp .env.example .env
# 修改 JWT_SECRET、密码等
docker compose up --build -d
```

仅应用、外接库时：

```bash
docker compose up --build -d server web
```

访问端口与发布版相同（`3000` / `18080`）。源码版 `API_URL` 由 compose 注入为 `http://server:...`；发布版不需要配。

---

## 网站与 HTTPS（1Panel / Nginx 等）

生产建议 **只反代 Web 端口**，不要把 `18080` 暴露公网。

### 1Panel 网站

1. **网站** → **创建网站** → 类型 **反向代理**。  
2. 代理地址：`http://127.0.0.1:3000`（若改过 `WEB_PUBLIC_PORT` 则换成实际端口）。  
3. 申请 Let's Encrypt 或上传证书，开启 HTTPS 与强制跳转。

### 注意

- 路径 `/` 整站（前台 + `/manage` + `/api/*`）。  
- `/api/*` **不要**单独指到 `18080`；应走 `3000`，由容器内 Next 转发到 Go。  
- Cookie 异常时检查 HTTPS、域名是否一致、反代是否乱改 Host。

### 通用反代示意

```text
https://your-domain.com        -> :3000
https://your-domain.com/api/*  -> :3000/api/* -> 内部 :8080
```

### 防火墙

放行 `80` / `443`；临时 IP 访问再放行 `WEB_PUBLIC_PORT`。  
**不建议** 公网放行 `SERVER_PUBLIC_PORT`、MySQL、Redis。

---

## 端口说明

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `WEB_PUBLIC_PORT` | `3000` | 前台与后台入口 |
| `SERVER_PUBLIC_PORT` | `18080` | 后端直连（映射容器内 `SERVER_PORT`） |
| `SERVER_PORT` | `8080` | 容器内 Go API 监听；compose 注入为进程 `PORT` |

浏览器访问 Web 下的 `/api/*` 时，由 Next 转发到同容器（发布版）或 `server` 服务（源码版）的 Go。

---

## 常用命令

**发布版（`ecohub` / `Eco-hub`）：**

```bash
docker compose ps
docker compose logs -f ecohub
docker compose logs -f mysql
docker compose logs -f redis
docker compose restart ecohub
docker compose down
```

**源码版：**

```bash
docker compose logs -f web
docker compose logs -f server
docker compose restart web
docker compose restart server
docker compose down        # 删容器保留数据
docker compose down -v    # 源码版还会删默认 volume
```

发布版数据在安装目录 `data/`；源码版默认 Docker volume。

---

## 健康检查与排障

- 探活：`GET /api/health`（容器内 `8080`；宿主机多为 `http://主机:18080/api/health`）。  
- 发布版内置库：`ecohub` 依赖 mysql/redis healthy 后再起。  
- 启动连不上 MySQL/Redis 会不健康或退出。

反复重启时查：

- `.env` 密码是否与库一致  
- `JWT_SECRET` 是否已设  
- `WEB_PUBLIC_PORT` 是否占用  
- 能否 `docker pull ghcr.io/fe-spark/ecohub:latest`  
- 外部库：host 是否容器可达、用户是否允许 Docker 网段、库是否只绑 `127.0.0.1`

### 拉取镜像失败

- 检查出网与 DNS；配置镜像加速或代理。  
- `docker pull ghcr.io/fe-spark/ecohub:latest` 看完整报错。

### 前台能开、接口全挂

- 反代是否只指 Web 端口且未错误剥离 `/api`。  
- 是否误配外链 `API_URL`（发布版不需要）。  
- 源码版：`API_URL` 是否指向可解析的 `server` 服务。

### Telegram 通知发不出

国内配置 `TG_PROXY`（如 `http://host.docker.internal:7890`）；Bot Token 在 **管理后台** 配置，不写死在 compose。

更多业务向 FAQ：[README-FAQ.md](./README-FAQ.md)。

---

## 安全建议

- 部署后立即改默认账号密码  
- 每环境单独生成 `JWT_SECRET`  
- 不要把生产密码提交进仓库  
- 优先 HTTPS 暴露前端入口  
- 不建议公网暴露 MySQL、Redis 或后端直连端口  

---

## 相关文档

- [根目录总览](./README.md)  
- [版本变更](./RELEASE.md)  
- [服务端说明](./server/README.md)（环境变量详表）  
- [前端说明](./web/README.md)  
- [FAQ 与排障](./README-FAQ.md)  
