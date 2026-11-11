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

[Demo](https://eco.fe-spark.cn) · [v3 Preview](http://148.135.106.141:3000) · [Admin](https://eco.fe-spark.cn/manage) · [Deploy guide](./README-Deploy_EN.md) · [FAQ](./README-FAQ_EN.md) · [Telegram](https://t.me/ecohub_club)

</div>

> [!WARNING]
> v3 cannot read a v2 database, and there is no migration. Do not pull the new image on top of an old `./data/mysql`, and do not click "立即升级" (upgrade now) in the v2 admin. Install into a new directory or wipe the data, then run a full collect. Steps: [v2 to v3](./v2-to-v3.md) (Chinese). The current release is `v3.0.2`. Report problems in [Issues](https://github.com/fe-spark/EcoHub/issues).

Updates are slow. Pull requests are welcome.

## Overview

EcoHub pulls films from the collect sources you configure. It gives you a public site, an admin panel, and three subscription URLs (EcoHub client, TVBox / YingShiCang, MacCMS).

Example: after install, enable a few collect sources in admin and run one full collect. The site can then search, filter, and play. Put the TVBox subscription URL into TVBox and you can watch on a TV.

The clients are separate repositories, included as git submodules under `app-for-ohos/` and `app-for-android/`:

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

You need Docker 20+ and Docker Compose 2+. Aim for at least 2 CPU cores and 2 GB RAM. One command installs everything:

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2 sh
```

After install:

- Data lives in `~/ecohub`. Manage it with the `ecohub` command (`ecohub status`, `ecohub update`, and so on).
- The terminal prints the site, admin, and three subscription URLs. The default web port is `3000`.
- The site starts empty. Run a full collect in admin under "采集中心" (Collect center). Films appear while the collect runs. The first run can take several hours.

Passwords, ports, reverse proxy, and 1Panel: [Deploy guide](./README-Deploy_EN.md).

## Local development

You need Go 1.27+, Node.js 20.9+, MySQL 8, and Redis 7. If you have no database at hand, the root compose file can start only the databases. It maps MySQL and Redis to `127.0.0.1:3306` and `127.0.0.1:6379`, with the same credentials as `server/.env.example`:

```bash
docker compose up -d mysql redis
```

Then start the server and the web app in two terminals:

```bash
cd server && cp .env.example .env && go run ./cmd/server
```

```bash
cd web && cp .env.example .env.local && npm install && npm run dev
```

Site `http://127.0.0.1:3000`, admin `http://127.0.0.1:3000/manage`, API `http://127.0.0.1:8080`. Details: [Server](../server/README.md) and [Web](../web/README.md) (Chinese).

## Documentation

| Document | Contents |
| --- | --- |
| [Deploy guide](./README-Deploy_EN.md) | Install, the `ecohub` command, passwords, ports, 1Panel, reverse proxy, environment variables |
| [v2 to v3](./v2-to-v3.md) (Chinese) | Stop the old stack, reinstall on an empty database, full collect |
| [FAQ](./README-FAQ_EN.md) | Empty site, main source, collect, cache, login |
| [Scheduled task conflicts](./cron-task-conflict.md) (Chinese) | Who runs first when collect and maintenance fire together |
| [Release notes](./RELEASE.md) (Chinese) | What changed in the current release |
| [Docs index](./README.md) | All documents |

> EcoHub does not provide or store media files. Sources come from collect APIs the operator configures. Follow local law and the collect sources' terms of use.

[PolyForm Noncommercial 1.0.0](../LICENSE) · [fe-spark/EcoHub](https://github.com/fe-spark/EcoHub) · [Issues](https://github.com/fe-spark/EcoHub/issues) · [Telegram](https://t.me/ecohub_club)
