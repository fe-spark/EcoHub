<div align="center">

<img src="../logo.png" alt="EcoHub" width="110" />

# EcoHub

[![Release](https://img.shields.io/github/v/release/fe-spark/EcoHub)](https://github.com/fe-spark/EcoHub/releases)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-16-black?logo=nextdotjs&logoColor=white)](https://nextjs.org/)
[![Docker](https://img.shields.io/badge/ghcr.io-fe--spark%2Fecohub-2496ED?logo=docker&logoColor=white)](https://github.com/fe-spark/EcoHub/pkgs/container/ecohub)
[![License](https://img.shields.io/badge/License-PolyForm%20Noncommercial%201.0.0-blue)](../LICENSE)

**Self-hosted film aggregation system: collects film metadata from collect sources and provides a viewing site, an admin panel, and subscription endpoints.**

[中文](../README.md) | English

[Demo](https://eco.fe-spark.cn) · [Admin](https://eco.fe-spark.cn/manage) · [Deploy guide](./README-Deploy_EN.md) · [FAQ](./README-FAQ_EN.md) · [Release notes](./RELEASE_EN.md) · [Telegram](https://t.me/ecohub_club)

</div>

> [!WARNING]
> v3 cannot read a v2 database, and there is no migration. Do not pull the new image on top of an old `./data/mysql`, and do not click "立即升级" (upgrade now) in the v2 admin. Steps: [v2 to v3](./v2-to-v3_EN.md).

## Overview

EcoHub is a self-hosted film aggregation system for individuals and small teams. After an administrator configures collect sources in the admin panel, EcoHub periodically fetches film metadata into the database. The application ships as a single All-in-One Docker image alongside MySQL 8 and Redis 7.

EcoHub neither provides nor stores any media files; all sources come from collect APIs configured by the deployer.

| Demo | URL |
| --- | --- |
| Viewing site | [https://eco.fe-spark.cn](https://eco.fe-spark.cn) |
| Admin panel | [https://eco.fe-spark.cn/manage](https://eco.fe-spark.cn/manage) |

## Recommendations

| Kind | Provider | Notes | Link |
| --- | --- | --- | --- |
| Server | CloudCone | Hosts the demo; unthrottled disk I/O | [Visit](https://app.cloudcone.com/?ref=14393) |
| Network | LiangXinYun | From ¥2/mo | [Register](https://xn--9kqz23b19z.com/#/register?code=xAmvfdic) |
| Network | PeiQian | From ¥1.5/mo | [Register](https://xn--mes358aby2apfg.com/register?code=FA4xlzHD&cover=sfw) |
| Network | DOVE | Reliable backup network service | [Register](https://dovee.cc/a.php?asbcbO1PCgTx2JM66) |

## Main features

- **Multi-source collect**: JSON / XML APIs with scheduled full and incremental collects.
- **Main source and search**: explicit global main source, in-memory pinyin/word search, and TMDB scraping.
- **Viewing site**: banners, filters, watch history, Artplayer with danmaku, and private access control.
- **Admin panel**: collect and library management, category rules, analytics, proxy, and Telegram notifications.

### Subscription endpoints

| URL | Purpose |
| --- | --- |
| `/api/provide/app` | EcoHub clients |
| `/api/provide/tvbox` | TVBox / 影视仓 |
| `/api/provide/vod` | MacCMS |

### Clients

Separate repositories, included as git submodules:

- [EcoHub for Android](https://github.com/fe-spark/EcoHub-for-Android) (`app-for-android/`)
- [EcoHub for OHOS](https://github.com/fe-spark/EcoHub-for-OHOS) (`app-for-ohos/`)

For behavioral details, see the [FAQ](./README-FAQ_EN.md).

## Architecture

```mermaid
flowchart LR
    Client["Browser / Subscription clients"] --> Web["Next.js :3000"]
    Web -- "/api/*" --> API["Go API :8080"]
    API --> DB[("MySQL 8 / Redis 7")]
    API --> Ext["Collect sources / TMDB / Telegram / Danmaku"]
```

## Quick start

Requirements: Docker 20+ and Docker Compose, with 2 CPU cores and 2 GB RAM or more recommended. Run:

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.1.3 sh
```

After installation, the terminal prints the URLs, initial credentials, and subscription URLs. The default web port is `3000`, the install directory is `~/ecohub`, and daily management uses `ecohub`. For passwords, ports, 1Panel, manual deployment, reverse proxies, and environment variables, see the [Deploy guide](./README-Deploy_EN.md).

## Documentation

| Document | Contents |
| --- | --- |
| [Deploy guide](./README-Deploy_EN.md) | Install, `ecohub` CLI, passwords, ports, 1Panel, reverse proxy, environment variables |
| [v2 to v3](./v2-to-v3_EN.md) | Removing old containers, fresh install with an empty database, full collect |
| [FAQ](./README-FAQ_EN.md) | Empty site, main source, collect, cache, network proxy, danmaku, login, private access |
| [Scheduled task conflicts](./cron-task-conflict_EN.md) | Execution order when collect and maintenance tasks are triggered together |
| [Dev guide](./README-Dev_EN.md) | Local development environment and development compose |
| [Server guide](../server/README_EN.md) | Go backend startup, environment variables, endpoints, directory layout |
| [Web guide](../web/README_EN.md) | Next.js frontend startup, `API_URL`, authentication, directory layout |
| [Release notes](./RELEASE_EN.md) | Changes in the current and previous releases |
| [Docs index](./README.md) | Complete index of Chinese and English documents |

## Directory layout

```text
EcoHub/
├── server/              # Go backend API
├── web/                 # Next.js frontend (viewing site and admin panel)
├── app-for-android/     # Android client (git submodule)
├── app-for-ohos/        # OHOS client (git submodule)
├── deploy/release/      # Release compose file (compose.yml)
├── scripts/             # Install script install-release.sh (ecohub CLI)
├── docs/                # Project documentation
├── .github/workflows/   # GitHub Release and Docker image workflows
├── Dockerfile           # All-in-One release image
├── docker-compose.yml   # Local development compose
└── supervisord.conf     # Process manager config inside the release image
```

## Contributing

This project is maintained in spare time with limited update frequency. [Issues](https://github.com/fe-spark/EcoHub/issues) and [Pull Requests](https://github.com/fe-spark/EcoHub/pulls) are welcome. Documentation changes should follow the [Style guide](./STYLE.md) (Chinese).

### Local development

Requirements: Go 1.27+, Node.js 20.9+, MySQL 8, and Redis 7. See the [Dev guide](./README-Dev_EN.md) for setup and compose usage, and the [Server guide](../server/README_EN.md) and [Web guide](../web/README_EN.md) for module details.

## License

This project is licensed under the [PolyForm Noncommercial License 1.0.0](../LICENSE) for noncommercial use only.

Comply with local laws and the terms of use of each collect source when using EcoHub.
