<div align="center">

<img src="logo.png" alt="EcoHub" width="110" />

# EcoHub

[![Release](https://img.shields.io/github/v/release/fe-spark/EcoHub)](https://github.com/fe-spark/EcoHub/releases)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-16-black?logo=nextdotjs&logoColor=white)](https://nextjs.org/)
[![Docker](https://img.shields.io/badge/ghcr.io-fe--spark%2Fecohub-2496ED?logo=docker&logoColor=white)](https://github.com/fe-spark/EcoHub/pkgs/container/ecohub)
[![License](https://img.shields.io/badge/License-PolyForm%20Noncommercial%201.0.0-blue)](./LICENSE)

**面向个人与小型团队的自托管影视聚合系统：单镜像集成前端、Go 后端与多端订阅接口。**

中文 | [English](./docs/en/README.md)

[在线演示](https://eco.fe-spark.cn) · [管理后台](https://eco.fe-spark.cn/manage) · [部署指南](./docs/guide/deploy.md) · [常见问题](./docs/guide/faq.md) · [版本说明](./docs/RELEASE.md) · [交流群组](https://t.me/ecohub_club)

</div>

> [!WARNING]
> v3 与 v2 数据库不兼容且不提供数据迁移，请勿在已有 `./data/mysql` 上直接更新或使用 v2「立即升级」。升级请见 [从 v2 升级到 v3](./docs/guide/v2-to-v3.md)。

## 推荐

| 类型 | 服务商 | 说明 | 链接 |
| --- | --- | --- | --- |
| 服务器 | CloudCone | 演示站所用，磁盘 I/O 不限 | [选购](https://app.cloudcone.com/?ref=14393) |
| 网络 | 良心云 | 低至 2 元/月 | [注册](https://xn--9kqz23b19z.com/#/register?code=xAmvfdic) |
| 网络 | 赔钱机场 | 低至 1.5 元/月 | [注册](https://xn--mes358aby2apfg.com/register?code=FA4xlzHD&cover=sfw) |
| 网络 | 轻语机场 | 稳定备用网络服务 | [注册](https://www.lajiaoyun.xyz/#/register?code=oLA3JOfG) |

## 主要功能

- **多站采集**：支持 JSON / XML 接口、全量/增量定时采集、采集记录追踪与失败页重试。
- **主站与检索**：后台指定全局主站与独立海报源，支持内存拼音/分词检索与 TMDB 刮削。
- **观影网站**：首页轮播与榜单推荐、多维筛选、观看历史、Artplayer 弹幕播放与私有访问控制。
- **管理后台**：片库与分类规则、首页管理、访问分析、网络代理、数据备份恢复与 Telegram 通知。
- **多端订阅**：提供多端统一订阅接口：
  - `/api/provide/app`：EcoHub 原生客户端
  - `/api/provide/tvbox`：TVBox / 影视仓
  - `/api/provide/vod`：MacCMS

### 配套客户端

提供两款独立移动端原生客户端（以 git submodule 形式随主仓关联），填入站点地址或 `/api/provide/app` 即可同步片库与播放，支持多源切换及私有化密钥（`?key=`）访问：

| 客户端 | 平台与技术栈 | 子模块路径 | 安装包 |
| --- | --- | --- | --- |
| [EcoHub for Android](https://github.com/fe-spark/EcoHub-for-Android) | Android（Flutter 3.x） | `app-for-android/` | [下载 APK](https://github.com/fe-spark/EcoHub-for-Android/releases/latest) |
| [EcoHub for OHOS](https://github.com/fe-spark/EcoHub-for-OHOS) | HarmonyOS / OpenHarmony（API 13+） | `app-for-ohos/` | [下载 HAP](https://github.com/fe-spark/EcoHub-for-OHOS/releases/latest) |

## 快速开始

环境要求：Docker 20+、Docker Compose，建议 2 核 CPU 与 2 GB 内存及以上（配合 MySQL 8 与 Redis 7）。

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.2.2 sh
```

安装完成后终端将输出访问地址、初始账号与订阅地址（默认端口 `3000`、数据目录 `~/ecohub`），日常维护使用 `ecohub` 命令。密码设置、1Panel、手动部署、反向代理与环境变量见 [部署指南](./docs/guide/deploy.md) 与 [运维与配置](./docs/guide/operations.md)。

## 文档

| 文档 | 内容 |
| --- | --- |
| [部署指南](./docs/guide/deploy.md) | 安装脚本、1Panel、手动部署、访问地址与反向代理 |
| [运维与配置](./docs/guide/operations.md) | `ecohub` 命令、密码与端口规则、自定义编排、外置库与环境变量 |
| [从 v2 升级到 v3](./docs/guide/v2-to-v3.md) | 停用旧容器、空库重装与验证 |
| [常见问题](./docs/guide/faq.md) | 空站、主站、轮播与推荐、代理、弹幕与私有化访问 |
| [开发与架构文档](./docs/dev/setup.md) | 本地环境搭建、[核心机制](./docs/dev/architecture.md)、[任务冲突](./docs/dev/cron-task-conflict.md)、[服务端](./server/README.md)与[前端](./web/README.md) |
| [完整文档索引](./docs/README.md) | 全部中英文使用、运维与开发文档导航 |

<details>
<summary>仓库目录结构（点击展开）</summary>

```text
EcoHub/
├── server/              # Go 后端 API（Go 1.27+）
├── web/                 # Next.js 前端（Next.js 16）
├── app-for-android/     # Android 客户端（git submodule）
├── app-for-ohos/        # OHOS 客户端（git submodule）
├── deploy/release/      # 发布编排 compose.yml
├── scripts/             # 安装与管理脚本 install-release.sh（ecohub）
├── docs/                # 项目文档（guide/、dev/、en/）
├── .github/workflows/   # GitHub Release 与 Docker 构建工作流
├── Dockerfile           # All-in-One 发布镜像
├── docker-compose.yml   # 本地开发编排
└── supervisord.conf     # 镜像内进程管理配置
```

</details>

> [!NOTE]
> EcoHub 不提供也不存储任何影视文件，全部数据来自部署者自行配置的采集接口。

## 参与贡献与许可

本项目由作者业余维护，欢迎提交 [Issues](https://github.com/fe-spark/EcoHub/issues) 与 [Pull Requests](https://github.com/fe-spark/EcoHub/pulls)（修改文档请遵循 [文档写作规范](./docs/STYLE.md)）。

基于 [PolyForm Noncommercial License 1.0.0](./LICENSE) 发布，仅允许非商业用途；使用时请遵守当地法律法规及采集站使用条款。
