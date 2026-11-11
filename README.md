<div align="center">

<img src="logo.png" alt="EcoHub" width="120" />

# EcoHub

[![Release](https://img.shields.io/github/v/release/fe-spark/EcoHub)](https://github.com/fe-spark/EcoHub/releases)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-16-black?logo=nextdotjs&logoColor=white)](https://nextjs.org/)
[![MySQL](https://img.shields.io/badge/MySQL-8-4479A1?logo=mysql&logoColor=white)](https://www.mysql.com/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?logo=redis&logoColor=white)](https://redis.io/)
[![License](https://img.shields.io/github/license/fe-spark/EcoHub)](./LICENSE)

**自托管影视聚合系统**

中文 | [English](./docs/README_EN.md)

[在线演示](https://eco.fe-spark.cn) · [管理后台](https://eco.fe-spark.cn/manage) · [部署指南](./docs/README-Deploy.md) · [常见问题](./docs/README-FAQ.md) · [交流群组](https://t.me/ecohub_club)

</div>

> [!WARNING]
> v3 和 v2 的数据库不兼容，也没有数据迁移。不要在旧的 `./data/mysql` 上直接拉新镜像，也不要在 v2 后台点「立即升级」。请用新目录或清空数据后重新安装，再做一次全量采集。步骤见 [从 v2 升级到 v3](./docs/v2-to-v3.md)。当前正式版是 `v3.0.2`，遇到问题请提 [Issues](https://github.com/fe-spark/EcoHub/issues)。

作者时间有限，更新较慢，欢迎提 [PR](https://github.com/fe-spark/EcoHub/pulls)。

## 简介

EcoHub 从你配置的采集站拉取影片，提供观影前台、管理后台和三种订阅地址（EcoHub 客户端、TVBox / 影视仓、MacCMS）。

举个例子：装好后在后台启用几座采集站，做一次全量采集，前台就能搜索、筛选、播放；把订阅地址填进 TVBox，电视上也能看。

客户端是独立仓库，以 git submodule 放在 `app-for-ohos/` 和 `app-for-android/`：

- [EcoHub for OHOS](https://github.com/fe-spark/EcoHub-for-OHOS)
- [EcoHub for Android](https://github.com/fe-spark/EcoHub-for-Android)

## 在线演示

| 入口 | 地址 |
| --- | --- |
| 前台 | [https://eco.fe-spark.cn](https://eco.fe-spark.cn) |
| 管理后台 | [https://eco.fe-spark.cn/manage](https://eco.fe-spark.cn/manage) |

## 安装

需要 Docker 20+ 和 Docker Compose，建议 2 核 2 GB 以上配置。一条命令装好：

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2 sh
```

装完后：

- 数据在 `~/ecohub`，以后用 `ecohub` 命令管理（`ecohub status`、`ecohub update` 等）。
- 终端会打印前台、后台和三条订阅地址。Web 端口默认 `3000`。
- 前台一开始是空的。到后台「采集中心」做一次全量采集，影片会边采边出现，第一次可能要几个小时。

改密码、换端口、反向代理、1Panel 见 [部署指南](./docs/README-Deploy.md)。

## 本地开发

本机需要 Go 1.27+、Node.js 20.9+、MySQL 8 和 Redis 7。

仓库根目录的 `docker-compose.yml` 只用于本地开发，不要拿来部署。它和发布用的 [deploy/release/compose.yml](./deploy/release/compose.yml) 的区别：

| | 根目录 `docker-compose.yml` | `deploy/release/compose.yml` |
| --- | --- | --- |
| 应用 | 从 `server/`、`web/` 源码本地构建，前后端分成 `Eco-server`、`Eco-web` 两个容器 | 拉取 All-in-One 镜像，一个 `Eco-hub` 容器 |
| MySQL、Redis 端口 | 映射到本机 `127.0.0.1:3306`、`127.0.0.1:6379` | 不映射 |
| 数据 | Docker 命名卷 `eco-mysql-data`、`eco-redis-data`、`eco-server-uploads` | 安装目录下的 `data/` |

两种用法：

1. 只起数据库，前后端在本机跑（日常开发）。数据库账号密码和 `server/.env.example` 一致：

   ```bash
   docker compose up -d mysql redis
   ```

   然后在两个终端分别启动后端和前端：

   ```bash
   cd server && cp .env.example .env && go run ./cmd/server
   ```

   ```bash
   cd web && cp .env.example .env.local && npm install && npm run dev
   ```

   前台 `http://127.0.0.1:3000`，后台 `http://127.0.0.1:3000/manage`，后端 API `http://127.0.0.1:8080`。

2. 整套从源码构建成容器（验证 Dockerfile 或联调）：

   ```bash
   docker compose up -d --build
   ```

   前台 `http://127.0.0.1:3000`，后端 API `http://127.0.0.1:18080`。端口和密码可以在仓库根目录的 `.env` 里改（从 [.env.example](./.env.example) 复制，不建也能用默认值）。

注意：

- 容器名 `Eco-mysql`、`Eco-redis` 和发布编排相同，同一台机器上不要同时跑开发编排和正式部署。
- 本机已经有 MySQL 或 Redis 占着 `3306`、`6379` 时，开发编排起不来，先停掉本机的或直接用本机的库。

细节见 [服务端说明](./server/README.md) 和 [前端说明](./web/README.md)。

## 文档

| 文档 | 内容 |
| --- | --- |
| [部署指南](./docs/README-Deploy.md) | 安装、`ecohub` 命令、密码、端口、1Panel、反向代理、环境变量 |
| [从 v2 升级到 v3](./docs/v2-to-v3.md) | 停掉旧栈、用空库重装、全量采集 |
| [常见问题](./docs/README-FAQ.md) | 空站、主站、采集、缓存、登录 |
| [计划任务运行冲突](./docs/cron-task-conflict.md) | 采集和维护任务同时到点时谁先跑 |
| [版本说明](./docs/RELEASE.md) | 当前版本改了什么 |
| [文档索引](./docs/README.md) | 全部文档，含英文版 |

> EcoHub 不提供、不存储影视文件。片源来自使用者自己配置的采集接口。请遵守当地法律和采集站的使用约定。

[PolyForm Noncommercial 1.0.0](./LICENSE) · [fe-spark/EcoHub](https://github.com/fe-spark/EcoHub) · [Issues](https://github.com/fe-spark/EcoHub/issues) · [Telegram](https://t.me/ecohub_club)
