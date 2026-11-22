<div align="center">

<img src="logo.png" alt="EcoHub" width="110" />

# EcoHub

[![Release](https://img.shields.io/github/v/release/fe-spark/EcoHub)](https://github.com/fe-spark/EcoHub/releases)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-16-black?logo=nextdotjs&logoColor=white)](https://nextjs.org/)
[![Docker](https://img.shields.io/badge/ghcr.io-fe--spark%2Fecohub-2496ED?logo=docker&logoColor=white)](https://github.com/fe-spark/EcoHub/pkgs/container/ecohub)
[![License](https://img.shields.io/badge/License-PolyForm%20Noncommercial%201.0.0-blue)](./LICENSE)

**自托管影视聚合系统：从采集站汇集影视数据，提供观影网站、管理后台与多端订阅接口。**

中文 | [English](./docs/README_EN.md)

[在线演示](https://eco.fe-spark.cn) · [管理后台](https://eco.fe-spark.cn/manage) · [部署指南](./docs/README-Deploy.md) · [常见问题](./docs/README-FAQ.md) · [版本说明](./docs/RELEASE.md) · [交流群组](https://t.me/ecohub_club)

</div>

> [!WARNING]
> v3 与 v2 的数据库不兼容，且不提供数据迁移。请勿在旧的 `./data/mysql` 上直接拉取新镜像，也勿使用 v2 后台的「立即升级」。升级步骤见 [从 v2 升级到 v3](./docs/v2-to-v3.md)。

## 简介

EcoHub 是一套面向个人与小型团队的自托管影视聚合系统。管理员在后台配置采集站后，系统定时拉取影片数据并统一入库，以单个 All-in-One Docker 镜像发布，配合 MySQL 8 与 Redis 7 即可运行。

EcoHub 不提供、不存储任何影视文件，片源全部来自部署者自行配置的采集接口。

| 演示入口 | 地址 |
| --- | --- |
| 观影前台 | [https://eco.fe-spark.cn](https://eco.fe-spark.cn) |
| 管理后台 | [https://eco.fe-spark.cn/manage](https://eco.fe-spark.cn/manage) |



## 主要功能

- **多站采集**：支持 JSON / XML 接口，全量与增量定时采集。
- **主站与检索**：后台显式指定全局主站，内存拼音/分词检索，TMDB 刮削。
- **观影网站**：轮播筛选、观看历史、Artplayer 弹幕播放与私有访问控制。
- **管理后台**：采集与片库管理、分类规则、统计、代理与 Telegram 通知。

### 订阅接口

| 地址 | 用途 |
| --- | --- |
| `/api/provide/app` | EcoHub 客户端 |
| `/api/provide/tvbox` | TVBox / 影视仓 |
| `/api/provide/vod` | MacCMS |

### 客户端

独立仓库，以 git submodule 形式包含在本仓库中：

- [EcoHub for Android](https://github.com/fe-spark/EcoHub-for-Android)（`app-for-android/`）
- [EcoHub for OHOS](https://github.com/fe-spark/EcoHub-for-OHOS)（`app-for-ohos/`）

功能细节见 [常见问题](./docs/README-FAQ.md)。

## 推荐

| 类型 | 服务商 | 说明 | 链接 |
| --- | --- | --- | --- |
| 服务器 | CloudCone | 演示站所用，磁盘 I/O 不限 | [选购](https://app.cloudcone.com/?ref=14393) |
| 网络 | 良心云 | 低至 2 元/月 | [注册](https://xn--9kqz23b19z.com/#/register?code=xAmvfdic) |
| 网络 | 赔钱机场 | 低至 1.5 元/月 | [注册](https://xn--mes358aby2apfg.com/register?code=FA4xlzHD&cover=sfw) |
| 网络 | DOVE | 稳定备用网络服务 | [注册](https://dovee.cc/a.php?asbcbO1PCgTx2JM66) |


## 技术架构

```mermaid
flowchart LR
    Client["浏览器 / 订阅客户端"] --> Web["Next.js 前端 :3000"]
    Web -- "/api/*" --> API["Go API :8080"]
    API --> DB[("MySQL 8 / Redis 7")]
    API --> Ext["采集站 / TMDB / Telegram / 弹幕"]
```

## 快速开始

环境要求：Docker 20+ 与 Docker Compose，建议 2 核 CPU、2 GB 内存及以上。执行以下命令安装：

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.1.5 sh
```

安装完成后终端将输出访问地址、初始账号与订阅地址，默认 Web 端口为 `3000`，数据目录为 `~/ecohub`，后续可通过 `ecohub` 命令管理。密码、端口、1Panel、手动部署、反向代理与环境变量见 [部署指南](./docs/README-Deploy.md)。

## 文档

| 文档 | 内容 |
| --- | --- |
| [部署指南](./docs/README-Deploy.md) | 安装、`ecohub` 命令、密码、端口、1Panel、反向代理、环境变量 |
| [从 v2 升级到 v3](./docs/v2-to-v3.md) | 删除旧容器、使用空库重装、全量采集 |
| [常见问题](./docs/README-FAQ.md) | 空站、主站、采集、缓存、网络代理、弹幕、登录、私有化访问 |
| [计划任务运行冲突](./docs/cron-task-conflict.md) | 采集任务与维护任务同时触发时的执行顺序 |
| [开发指南](./docs/README-Dev.md) | 本地开发环境与开发编排 |
| [服务端说明](./server/README.md) | Go 后端启动、环境变量、接口、目录结构 |
| [前端说明](./web/README.md) | Next.js 前端启动、`API_URL`、鉴权、目录结构 |
| [版本说明](./docs/RELEASE.md) | 当前版本及历史版本的变更内容 |
| [文档索引](./docs/README.md) | 全部中英文文档索引 |

## 目录结构

```text
EcoHub/
├── server/              # Go 后端 API
├── web/                 # Next.js 前端（前台与管理后台）
├── app-for-android/     # Android 客户端（git submodule）
├── app-for-ohos/        # OHOS 客户端（git submodule）
├── deploy/release/      # 发布编排 compose.yml
├── scripts/             # 安装脚本 install-release.sh（ecohub 命令）
├── docs/                # 项目文档
├── .github/workflows/   # GitHub Release 与 Docker 镜像构建流程
├── Dockerfile           # All-in-One 发布镜像
├── docker-compose.yml   # 本地开发编排
└── supervisord.conf     # 镜像内进程管理配置
```

## 参与贡献

本项目由作者业余维护，更新频率有限，欢迎提交 [Issues](https://github.com/fe-spark/EcoHub/issues) 与 [Pull Requests](https://github.com/fe-spark/EcoHub/pulls)。文档修改请遵循 [文档写作规范](./docs/STYLE.md)。

### 本地开发

环境要求：Go 1.27+、Node.js 20.9+、MySQL 8、Redis 7。搭建步骤与编排说明见 [开发指南](./docs/README-Dev.md)，模块细节见 [服务端说明](./server/README.md) 与 [前端说明](./web/README.md)。

## 许可证

本项目基于 [PolyForm Noncommercial License 1.0.0](./LICENSE) 发布，仅允许非商业用途。

使用 EcoHub 时请遵守当地法律法规及采集站的使用条款。
