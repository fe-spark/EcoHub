<div align="center">

<img src="../logo.png" alt="EcoHub" width="120" />

# EcoHub

[![Release](https://img.shields.io/github/v/release/fe-spark/EcoHub)](https://github.com/fe-spark/EcoHub/releases)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-16-black?logo=nextdotjs&logoColor=white)](https://nextjs.org/)
[![MySQL](https://img.shields.io/badge/MySQL-8-4479A1?logo=mysql&logoColor=white)](https://www.mysql.com/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?logo=redis&logoColor=white)](https://redis.io/)
[![License](https://img.shields.io/github/license/fe-spark/EcoHub)](../LICENSE)

**Self-hosted media aggregation**

[中文](../README.md) | English

[Demo](https://eco.fe-spark.cn) · [v3 Preview](http://148.135.106.141:3000) · [Admin](https://eco.fe-spark.cn/manage) · [Deploy](./README-Deploy_EN.md) · [FAQ](./README-FAQ_EN.md) · [Telegram](https://t.me/ecohub_club)

</div>

> [!WARNING]
> v3.0 does not read v2.x databases and there is no migration. Do not pull on an old `./data/mysql`, and do not use in-app upgrade. Deploy in a new directory or after wiping data, then run a full collect. Steps: [v2 to v3](./v2-to-v3.md). This preview is `3.0.0-preview`. File problems as [Issues](https://github.com/fe-spark/EcoHub/issues).

Updates are slow. Pull requests are welcome.

## Overview

Multi-source collect, a web player, and an admin panel. Clients are separate repos:

- [EcoHub for OHOS](https://github.com/fe-spark/EcoHub-for-OHOS)
- [EcoHub for Android](https://github.com/fe-spark/EcoHub-for-Android)

## Demo

| Entry | URL |
| --- | --- |
| Site | [https://eco.fe-spark.cn](https://eco.fe-spark.cn) |
| Admin | [https://eco.fe-spark.cn/manage](https://eco.fe-spark.cn/manage) |
| v3 preview | [http://148.135.106.141:3000](http://148.135.106.141:3000) |

Visitor login is `guest` / `guest` (read-only). The demo does not work on China Mobile. Use China Unicom or China Telecom.

## Recommendations

| Kind | Provider | Link |
| --- | --- | --- |
| Server | CloudCone (hosts the demo; unthrottled disk I/O) | [Visit](https://app.cloudcone.com/?ref=14393) |
| Network | LiangXinYun, from ¥2/mo | [Register](https://xn--9kqz23b19z.com/#/register?code=xAmvfdic) |
| Network | PeiQian, from ¥1.5/mo | [Register](https://xn--mes358aby2apfg.com/register?code=FA4xlzHD&cover=sfw) |
| Network | DOVE | [Register](https://dovee.cc/a.php?asbcbO1PCgTx2JM66) |

## Install

Docker 20+ and Compose 2+. Aim for 2 CPU cores and 2 GB RAM.

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.0-preview sh
```

Data goes to `~/ecohub`. The command is `ecohub`. Install prints the web port, site, admin, and three subscription URLs. The default web port is `3000`. Passwords, reverse proxy, and 1Panel: [Deploy guide](./README-Deploy_EN.md).

The site has no films until you run a full collect in admin. Films appear as each window is published. The first run can take several hours.

## Local development

Start MySQL 8 and Redis 7 first.

```bash
cd server && cp .env.example .env && go run ./cmd/server
cd web && cp .env.example .env.local && npm install && npm run dev
```

Site `http://127.0.0.1:3000`, admin `/manage`, API `http://127.0.0.1:8080`. See [Server](../server/README.md) and [Web](../web/README.md).

## Documentation

| Document | Contents |
| --- | --- |
| [Deploy](./README-Deploy_EN.md) | Install, commands, passwords, 1Panel, proxy |
| [v2 to v3](./v2-to-v3.md) | Stop the old stack, empty database, full collect |
| [FAQ](./README-FAQ_EN.md) | Empty catalog, main source, collect, cache, login |
| [Release notes](./RELEASE.md) | This release |
| [Chinese](../README.md) | 中文总览 |

EcoHub does not provide or store media files. Sources come from collect APIs the operator configures.

[PolyForm Noncommercial 1.0.0](../LICENSE) · [fe-spark/EcoHub](https://github.com/fe-spark/EcoHub) · [Issues](https://github.com/fe-spark/EcoHub/issues) · [Telegram](https://t.me/ecohub_club)
