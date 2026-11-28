<div align="center">

<img src="../../logo.png" alt="EcoHub" width="110" />

# EcoHub

[![Release](https://img.shields.io/github/v/release/fe-spark/EcoHub)](https://github.com/fe-spark/EcoHub/releases)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-16-black?logo=nextdotjs&logoColor=white)](https://nextjs.org/)
[![Docker](https://img.shields.io/badge/ghcr.io-fe--spark%2Fecohub-2496ED?logo=docker&logoColor=white)](https://github.com/fe-spark/EcoHub/pkgs/container/ecohub)
[![License](https://img.shields.io/badge/License-PolyForm%20Noncommercial%201.0.0-blue)](../../LICENSE)

**Self-hosted media aggregation system for individuals and small teams: all-in-one image bundling the site, Go API, and subscription endpoints.**

[中文](../../README.md) | English

[Live demo](https://eco.fe-spark.cn) · [Admin demo](https://eco.fe-spark.cn/manage) · [Deploy guide](./guide/deploy.md) · [FAQ](./guide/faq.md) · [Release notes](./RELEASE.md) · [Community](https://t.me/ecohub_club)

</div>

## Recommendations

| Kind | Provider | Notes | Link |
| --- | --- | --- | --- |
| Server | CloudCone | Hosts the demo; unthrottled disk I/O | [Visit](https://app.cloudcone.com/?ref=14393) |
| Network | LiangXinYun | From ¥2/mo | [Register](https://xn--9kqz23b19z.com/#/register?code=xAmvfdic) |
| Network | PeiQian | From ¥1.5/mo | [Register](https://xn--mes358aby2apfg.com/register?code=FA4xlzHD&cover=sfw) |
| Network | QingYu | Reliable backup network service | [Register](https://www.lajiaoyun.xyz/#/register?code=oLA3JOfG) |

## Main features

- **Multi-source collect**: JSON / XML APIs with scheduled full and incremental collects, collect record tracking, and failed-page retries.
- **Main source and search**: explicit global main source and independent poster source, in-memory pinyin/word search, and TMDB scraping.
- **Viewing site**: homepage banners and chart recommendations, multi-filter browsing, watch history, Artplayer with danmaku, and private access control.
- **Admin panel**: library and category rules, homepage management, analytics, outbound network proxy, backup/restore, and Telegram notifications.
- **Subscription endpoints**: exposes unified APIs for multiple clients:
  - `/api/provide/app`: EcoHub native clients
  - `/api/provide/tvbox`: TVBox / 影视仓
  - `/api/provide/vod`: MacCMS

### Companion clients

Two standalone native mobile clients are included as git submodules. Enter the site root URL or `/api/provide/app` in the app to sync the library and play, with multi-source switching and private access key (`?key=`) support:

| Client | Platform & stack | Submodule path | Downloads |
| --- | --- | --- | --- |
| [EcoHub for Android](https://github.com/fe-spark/EcoHub-for-Android) | Android (Flutter 3.x) | `app-for-android/` | [Download APK](https://github.com/fe-spark/EcoHub-for-Android/releases/latest) |
| [EcoHub for OHOS](https://github.com/fe-spark/EcoHub-for-OHOS) | HarmonyOS / OpenHarmony (API 13+) | `app-for-ohos/` | [Download HAP](https://github.com/fe-spark/EcoHub-for-OHOS/releases/latest) |

## Quick start

Requirements: Docker 20+ and Docker Compose, with 2 CPU cores and 2 GB RAM or more recommended (running with MySQL 8 and Redis 7).

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.2.2 sh
```

The installer outputs the URLs, initial credentials, and subscription endpoints (default web port `3000`, directory `~/ecohub`, managed via `ecohub`). For passwords, 1Panel, manual runs, reverse proxies, and environment variables, see the [Deploy guide](./guide/deploy.md) and [Operations](./guide/operations.md).

## Documentation

| Document | Contents |
| --- | --- |
| [Deploy guide](./guide/deploy.md) | Install script, 1Panel, manual docker run, URLs, reverse proxy |
| [Operations and configuration](./guide/operations.md) | `ecohub` CLI, password/port reload rules, overrides, external DB, env vars |
| [Upgrading from v2 to v3](./guide/v2-to-v3.md) | Removing old containers, fresh install with empty DB, full collect |
| [FAQ](./guide/faq.md) | Empty site, main source, banners/recommendations, proxy, danmaku, private access |
| [Developer & architecture docs](./dev/setup.md) | Local setup, [Core mechanics](./dev/architecture.md), [Task conflicts](./dev/cron-task-conflict.md), [Server](../../server/README_EN.md), and [Web](../../web/README_EN.md) |
| [Full docs index](../README.md) | Complete index of user, operations, and developer docs |

<details>
<summary>Repository layout (click to expand)</summary>

```text
EcoHub/
├── server/              # Go backend API (Go 1.27+)
├── web/                 # Next.js frontend (Next.js 16)
├── app-for-android/     # Android client (git submodule)
├── app-for-ohos/        # OHOS client (git submodule)
├── deploy/release/      # Release compose file (compose.yml)
├── scripts/             # Install and CLI script install-release.sh (ecohub)
├── docs/                # Project documentation (guide/, dev/, en/)
├── .github/workflows/   # GitHub Release and Docker image workflows
├── Dockerfile           # All-in-One release image
├── docker-compose.yml   # Local development compose
└── supervisord.conf     # Process manager config inside the release image
```

</details>

> [!NOTE]
> EcoHub provides and stores no media files. All metadata comes from collect APIs configured by the operator.

> [!WARNING]
> v3 is incompatible with v2 databases and provides no migration. Do not update directly over an existing `./data/mysql` or use "立即升级" in v2. See [Upgrading from v2 to v3](./guide/v2-to-v3.md).

## Contributing and license

Maintained in spare time; [Issues](https://github.com/fe-spark/EcoHub/issues) and [Pull Requests](https://github.com/fe-spark/EcoHub/pulls) are welcome (documentation updates follow the [Style guide](../STYLE.md) (Chinese)).

Licensed under the [PolyForm Noncommercial License 1.0.0](../../LICENSE) for noncommercial use only. Comply with local laws and the terms of use of each collect source.
