<div align="center">

<img src="../../logo.png" alt="EcoHub" width="110" />

# EcoHub

[![Release](https://img.shields.io/github/v/release/fe-spark/EcoHub)](https://github.com/fe-spark/EcoHub/releases)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-16-black?logo=nextdotjs&logoColor=white)](https://nextjs.org/)
[![Docker](https://img.shields.io/badge/ghcr.io-fe--spark%2Fecohub-2496ED?logo=docker&logoColor=white)](https://github.com/fe-spark/EcoHub/pkgs/container/ecohub)
[![License](https://img.shields.io/badge/License-PolyForm%20Noncommercial%201.0.0-blue)](../../LICENSE)

**Self-hosted media aggregation system: collects catalog data from upstream sources and serves a viewing site, an admin panel, and subscription APIs.**

[中文](../../README.md) | English

[Live demo](https://eco.fe-spark.cn) · [Admin demo](https://eco.fe-spark.cn/manage) · [Deploy guide](./guide/deploy.md) · [FAQ](./guide/faq.md) · [Release notes](./RELEASE.md) · [Community](https://t.me/ecohub_club)

</div>

> [!WARNING]
> v3 is not compatible with v2 databases, and no data migration is provided. Do not pull the v3 image onto an existing `./data/mysql`, and do not use "立即升级" (Upgrade now) in v2. See [Upgrading from v2 to v3](./guide/v2-to-v3.md).

## Overview

EcoHub is a self-hosted media aggregation system for individuals and small teams. After an administrator configures collect sources, the server pulls media metadata on a schedule, ships as a single all-in-one Docker image, and runs with MySQL 8 and Redis 7.

EcoHub provides and stores no media files. All media comes from the collect APIs configured by the operator.

| Demo | URL |
| --- | --- |
| Site | [https://eco.fe-spark.cn](https://eco.fe-spark.cn) |
| Admin panel | [https://eco.fe-spark.cn/manage](https://eco.fe-spark.cn/manage) |

## Main features

- **Multi-source collect**: JSON / XML APIs with scheduled full and incremental collects, collect record tracking, and failed-page retries.
- **Main source and search**: explicit global main source, in-memory pinyin/word search, and TMDB scraping.
- **Viewing site**: homepage banners and chart recommendations, multi-filter browsing, watch history, Artplayer with danmaku, and private access control.
- **Admin panel**: collect and library management, homepage management, category rules, analytics, network proxy, backup/restore, and Telegram notifications.

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

For usage questions, see the [FAQ](./guide/faq.md).

## Quick start

Requirements: Docker 20+ and Docker Compose, with 2 CPU cores and 2 GB RAM or more recommended. Run:

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.2.2 sh
```

After installation, the terminal prints the URLs, initial credentials, and subscription URLs. The default web port is `3000`, the install directory is `~/ecohub`, and daily management uses `ecohub`. For passwords, ports, 1Panel, manual deployment, reverse proxies, and environment variables, see the [Deploy guide](./guide/deploy.md) and [Operations](./guide/operations.md).

## Recommendations

| Kind | Provider | Notes | Link |
| --- | --- | --- | --- |
| Server | CloudCone | Hosts the demo; unthrottled disk I/O | [Visit](https://app.cloudcone.com/?ref=14393) |
| Network | LiangXinYun | From ¥2/mo | [Register](https://xn--9kqz23b19z.com/#/register?code=xAmvfdic) |
| Network | PeiQian | From ¥1.5/mo | [Register](https://xn--mes358aby2apfg.com/register?code=FA4xlzHD&cover=sfw) |
| Network | QingYu | Reliable backup network service | [Register](https://www.lajiaoyun.xyz/#/register?code=oLA3JOfG) |

## Documentation

| Document | Contents |
| --- | --- |
| [Deploy guide](./guide/deploy.md) | Prerequisites, install script, 1Panel, manual docker run, URLs, reverse proxy |
| [Operations and configuration](./guide/operations.md) | `ecohub` CLI, password/port reload rules, overrides, external DB, env vars |
| [Upgrading from v2 to v3](./guide/v2-to-v3.md) | Removing old containers, fresh install with empty DB, full collect |
| [FAQ](./guide/faq.md) | Empty site, main source, banners/recommendations, proxy, danmaku, private access |
| [Release notes](./RELEASE.md) | Changes in the current and previous releases |
| [Docs index](../README.md) | Complete index of user, dev, and bilingual documents |

## Directory layout

```text
EcoHub/
├── server/              # Go backend API
├── web/                 # Next.js frontend (viewing site and admin panel)
├── app-for-android/     # Android client (git submodule)
├── app-for-ohos/        # OHOS client (git submodule)
├── deploy/release/      # Release compose file (compose.yml)
├── scripts/             # Install script install-release.sh (ecohub CLI)
├── docs/                # Project documentation (guide/, dev/, en/)
├── .github/workflows/   # GitHub Release and Docker image workflows
├── Dockerfile           # All-in-One release image
├── docker-compose.yml   # Local development compose
└── supervisord.conf     # Process manager config inside the release image
```

## Contributing

This project is maintained in spare time with limited update frequency. [Issues](https://github.com/fe-spark/EcoHub/issues) and [Pull Requests](https://github.com/fe-spark/EcoHub/pulls) are welcome. Documentation changes should follow the [Style guide](../STYLE.md) (Chinese).

### Local development

Requirements: Go 1.27+, Node.js 20.9+, MySQL 8, and Redis 7. See the [Development guide](./dev/setup.md), [Core mechanics](./dev/architecture.md), [Scheduled task conflicts](./dev/cron-task-conflict.md), [Server guide](../../server/README_EN.md), and [Web guide](../../web/README_EN.md).

## License

This project is licensed under the [PolyForm Noncommercial License 1.0.0](../../LICENSE) for noncommercial use only.

Comply with local laws and the terms of use of each collect source when using EcoHub.
