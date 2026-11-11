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

[Demo](https://eco.fe-spark.cn) · [Admin](https://eco.fe-spark.cn/manage) · [Deploy guide](./README-Deploy_EN.md) · [FAQ](./README-FAQ_EN.md) · [Telegram](https://t.me/ecohub_club)

</div>

> [!WARNING]
> v3 cannot read a v2 database, and there is no migration. Do not pull the new image on top of an old `./data/mysql`, and do not click "立即升级" (upgrade now) in the v2 admin. Install into a new directory or wipe the data, then run a full collect. Steps: [v2 to v3](./v2-to-v3_EN.md). The current release is `v3.0.2`. Report problems in [Issues](https://github.com/fe-spark/EcoHub/issues).

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

## Install

You need Docker 20+ and Docker Compose. Aim for at least 2 CPU cores and 2 GB RAM. One command installs everything:

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

You need Go 1.27+, Node.js 20.9+, MySQL 8, and Redis 7.

The `docker-compose.yml` in the repository root is for local development only. Do not use it to deploy. How it differs from the release file [deploy/release/compose.yml](../deploy/release/compose.yml):

| | Root `docker-compose.yml` | `deploy/release/compose.yml` |
| --- | --- | --- |
| App | Built locally from `server/` and `web/` source, as two containers `Eco-server` and `Eco-web` | Pulls the All-in-One image, one `Eco-hub` container |
| MySQL and Redis ports | Mapped to `127.0.0.1:3306` and `127.0.0.1:6379` on the host | Not mapped |
| Data | Docker named volumes `eco-mysql-data`, `eco-redis-data`, `eco-server-uploads` | `data/` in the install directory |

Two ways to use it:

1. Start only the databases and run the server and web app on the host (day-to-day development). The database credentials match `server/.env.example`:

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

   Site `http://127.0.0.1:3000`, admin `http://127.0.0.1:3000/manage`, API `http://127.0.0.1:8080`.

2. Build the whole stack from source as containers (to check the Dockerfiles or test end to end):

   ```bash
   docker compose up -d --build
   ```

   Site `http://127.0.0.1:3000`, API `http://127.0.0.1:18080`. Change ports and passwords in a `.env` in the repository root (copy it from [.env.example](../.env.example); the defaults work without it).

Notes:

- The container names `Eco-mysql` and `Eco-redis` are the same as in the release file. Do not run the dev stack and a real deployment on the same machine.
- If a MySQL or Redis on the host already uses `3306` or `6379`, the dev stack cannot start. Stop the host service, or use it instead.

Details: [Server](../server/README_EN.md) and [Web](../web/README_EN.md).

## Documentation

| Document | Contents |
| --- | --- |
| [Deploy guide](./README-Deploy_EN.md) | Install, the `ecohub` command, passwords, ports, 1Panel, reverse proxy, environment variables |
| [v2 to v3](./v2-to-v3_EN.md) | Stop the old stack, reinstall on an empty database, full collect |
| [FAQ](./README-FAQ_EN.md) | Empty site, main source, collect, cache, login |
| [Scheduled task conflicts](./cron-task-conflict_EN.md) | Who runs first when collect and maintenance fire together |
| [Release notes](./RELEASE_EN.md) | What changed in the current release |
| [Docs index](./README.md) | All documents |

> EcoHub does not provide or store media files. Sources come from collect APIs the operator configures. Follow local law and the collect sources' terms of use.

[PolyForm Noncommercial 1.0.0](../LICENSE) · [fe-spark/EcoHub](https://github.com/fe-spark/EcoHub) · [Issues](https://github.com/fe-spark/EcoHub/issues) · [Telegram](https://t.me/ecohub_club)
