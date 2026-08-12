# 1Panel 一键部署教程

用 [1Panel](https://1panel.cn/) 图形界面部署 EcoHub **发布版**（All-in-One 单镜像 + 内置 MySQL / Redis）。不需要本机装 Go / Node，也不用拆前后端。

> 镜像：`ghcr.io/fe-spark/ecohub:latest`  
> Compose 源文件：[deploy/release/compose.yml](./deploy/release/compose.yml)  
> 环境变量模板：[.env.example](./.env.example)

## 前置条件

| 项 | 说明 |
| --- | --- |
| 1Panel | 已安装并可用（含 Docker / Compose） |
| 网络 | 服务器能拉 `ghcr.io`、`docker.io`（国内可配镜像加速） |
| 端口 | 至少空闲一个对外 Web 端口（默认 `3000`；若被占用可改） |
| 资源 | 建议 ≥ 2 核 2G；采集任务多时适当加内存 |

## 方式 A：编排商店 / 自定义编排（推荐）

适合 1Panel **容器 → 编排**（或「Compose / 编排应用」）里新建项目。

### 1. 新建编排

1. 打开 1Panel → **容器** → **编排**（部分版本叫「Compose」）。
2. **创建编排** / **创建应用**。
3. 名称建议：`ecohub`。
4. 工作目录任选，例如：`/opt/1panel/apps/ecohub`（以面板实际路径为准）。

### 2. 粘贴 Compose

将下面内容完整粘贴到编排的 `docker-compose.yml`（与仓库 [deploy/release/compose.yml](./deploy/release/compose.yml) 一致）：

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

### 3. 配置环境变量（`.env`）

在编排的 **环境变量** / `.env` 编辑区写入（**务必改密码与 JWT**）：

```env
WEB_PUBLIC_PORT=3000
SERVER_PUBLIC_PORT=18080
SERVER_PORT=8080

# 用 openssl rand -hex 32 生成后粘贴，不要用默认值
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

# 可选：Telegram 代理（国内常用）
# TG_PROXY=http://host.docker.internal:7890

# 可选：采集档位 auto|light|standard|high
# COLLECT_PROFILE=auto
```

说明：

- `MYSQL_HOST=mysql` / `REDIS_HOST=redis` 是 **Compose 服务名**，不要改成 `127.0.0.1`。
- `WEB_PUBLIC_PORT` 若宿主机 `3000` 已被占用，改成例如 `13000`，后面网站反代指向该端口。
- 不需要配置 `API_URL`（镜像内默认同容器 `http://127.0.0.1:8080`）。

### 4. 启动

1. 保存编排 → **启动** / **应用**。
2. 等待镜像拉取（首次较慢）与健康检查通过。
3. 在编排详情中确认三个容器：`Eco-hub`、`Eco-mysql`、`Eco-redis` 均为运行中。

### 5. 验证

浏览器访问：

| 地址 | 说明 |
| --- | --- |
| `http://服务器IP:WEB_PUBLIC_PORT` | 前台 |
| `http://服务器IP:WEB_PUBLIC_PORT/manage` | 管理后台 |
| `http://服务器IP:SERVER_PUBLIC_PORT/api/health` | API 探活（可选） |

默认账号（**登录后立刻改密**）：

- 管理员：`admin` / `admin`
- 访客：`guest` / `guest`

首次**需要**在后台配置 **采集源** 并执行采集，否则前台无影片。

---

## 方式 B：终端安装脚本 + 1Panel 接管

若更习惯命令行，可先在服务器执行：

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
cd ~/ecohub
# 编辑 .env 改 JWT / 密码
docker compose up -d
```

然后在 1Panel **容器** 中可看到 `Eco-hub` 等容器；网站反代仍按下文配置。数据目录默认：`~/ecohub/data/`。

---

## 网站与 HTTPS（1Panel 网站模块）

生产环境建议 **只反代 Web 端口**，不要把 `18080` 暴露到公网。

### 1. 创建网站

1. **网站** → **创建网站**。
2. 主域名填你的域名（已解析到本机）。
3. 类型选 **反向代理**。
4. 代理地址：

```text
http://127.0.0.1:3000
```

若改过 `WEB_PUBLIC_PORT`，把 `3000` 换成实际端口。

### 2. SSL

在网站设置中申请 **Let's Encrypt** 或上传证书，开启 HTTPS 与强制跳转。

### 3. 代理注意点

- 路径保持默认 `/` → 整站（前台 + `/manage` + `/api/*`）。
- `/api/*` **不要**单独指到 `18080`；应走站点 `3000`，由容器内 Next 转发到 Go。
- 若登录 Cookie 异常，检查是否 HTTPS、域名是否与访问一致，以及反代是否改写了 Host（一般保持默认即可）。

### 4. 防火墙

1Panel **主机** → **防火墙**（或安全组）放行：

- `80` / `443`（网站）
- 若暂时不用域名、直接 IP 访问，再放行 `WEB_PUBLIC_PORT`

**不建议** 对公网放行 `SERVER_PUBLIC_PORT`（默认 `18080`）与 MySQL/Redis。

---

## 更新版本

### 编排方式

1. 打开 `ecohub` 编排。
2. 需要固定版本时，把 compose 中镜像改为例如：

```yaml
image: ghcr.io/fe-spark/ecohub:v2.0.1
```

3. **拉取镜像** → **重建 / 重启** 应用（或等价「更新」按钮）。

跟 `:latest` 时：

```bash
# 在编排工作目录或 SSH 中
docker compose pull
docker compose up -d
```

正式版 tag 会同步覆盖 `:latest`。变更见 [RELEASE.md](./RELEASE.md)。

### 数据

编排目录下相对路径：

```text
./data/mysql
./data/redis
./data/uploads
```

升级一般 **不必** 删 data；删目录等于清空库与上传文件。

---

## 使用 1Panel 已有 MySQL / Redis（可选）

不想用内置库时：

1. 在 1Panel 装好 MySQL 8、Redis 7，记下账号密码与 **容器网络内可访问的主机名/IP**。
2. 从 compose 中 **删除** `mysql`、`redis` 服务，以及 `ecohub.depends_on`。
3. `.env` 示例：

```env
MYSQL_HOST=1Panel里MySQL的容器名或IP
MYSQL_PORT=3306
MYSQL_USER=...
MYSQL_PASSWORD=...
MYSQL_DBNAME=eco

REDIS_HOST=1Panel里Redis的容器名或IP
REDIS_PORT=6379
REDIS_PASSWORD=...
REDIS_DB=0
```

4. 确保 `Eco-hub` 与数据库容器在 **同一 Docker 网络**，或 `MYSQL_HOST` 对 `ecohub` 容器可达。  
5. 仅启动 `ecohub` 服务。

---

## 常见问题

### 拉取 `ghcr.io/fe-spark/ecohub` 失败

- 检查服务器出网与 DNS。
- 在 1Panel / Docker 配置镜像加速或代理后重试。
- 手动：`docker pull ghcr.io/fe-spark/ecohub:latest` 看完整报错。

### `Eco-hub` 一直不健康 / 重启

1. 看日志：编排 → `Eco-hub` → 日志，或 `docker logs Eco-hub`。
2. 常见原因：`JWT_SECRET` 未设、MySQL/Redis 密码与 `.env` 不一致、库未 healthy 就连、磁盘满。
3. 探活：`wget -q -O- http://127.0.0.1:18080/api/health`（端口以 `SERVER_PUBLIC_PORT` 为准）。

### 前台能开，接口全挂

- 反代是否只指到 Web 端口，且未错误剥离 `/api`。
- 是否用了错误的外链 API 地址（发布版无需在面板里再配 `API_URL`）。

### 端口冲突

改 `.env` 中 `WEB_PUBLIC_PORT` / `SERVER_PUBLIC_PORT`，保存后重建编排，网站反代同步改端口。

### Telegram 通知发不出

国内机器配置 `TG_PROXY`（如 `http://host.docker.internal:7890`），并保证宿主机代理可达；Bot Token 在 **管理后台** 配置，不在 compose 里写死。

---

## 相关文档

- [Docker 部署说明](./README-Docker.md)（命令行 / 源码版）
- [FAQ 与排障](./README-FAQ.md)
- [版本变更](./RELEASE.md)
- [根目录总览](./README.md)
