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
> v3 is not compatible with the v2 database, and no data migration is provided. Do not pull the new image on top of an existing `./data/mysql`, and do not use "立即升级" (Upgrade now) in the v2 admin. Install into a new directory or clear the data, then run a full collect. For the procedure, see [Upgrading from v2 to v3](./v2-to-v3_EN.md). The current release is `v3.0.3`. Report issues in [Issues](https://github.com/fe-spark/EcoHub/issues).

The project is maintained in the author's spare time, and updates are infrequent. Pull requests are welcome.

## Overview

EcoHub retrieves film data from the collect sources configured by the administrator. It provides a public site, an admin panel, and three subscription URLs (EcoHub client, TVBox / YingShiCang, MacCMS).

Typical workflow: after installation, enable collect sources in the admin panel and run a full collect. The site then supports search, filtering, and playback. Adding the TVBox subscription URL to TVBox enables playback on a TV.

The clients are maintained in separate repositories and included as git submodules under `app-for-ohos/` and `app-for-android/`:

- [EcoHub for OHOS](https://github.com/fe-spark/EcoHub-for-OHOS)
- [EcoHub for Android](https://github.com/fe-spark/EcoHub-for-Android)

## Demo

| Entry | URL |
| --- | --- |
| Site | [https://eco.fe-spark.cn](https://eco.fe-spark.cn) |
| Admin | [https://eco.fe-spark.cn/manage](https://eco.fe-spark.cn/manage) |

## Install

Requirements: Docker 20+ and Docker Compose. At least 2 CPU cores and 2 GB of RAM are recommended. Run the following command to install:

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.3 sh
```

After installation:

- Data is stored in `~/ecohub` and is managed with the `ecohub` command (for example, `ecohub status` and `ecohub update`).
- The terminal prints the site URL, the admin URL, and the three subscription URLs. The default web port is `3000`.
- The site is empty at first. Run a full collect from "采集中心" (Collect center) in the admin panel. Films appear as the collect progresses. The first collect can take several hours.

For passwords, ports, reverse proxy, and 1Panel, see the [Deploy guide](./README-Deploy_EN.md).

## Local development

Requirements: Go 1.27+, Node.js 20.9+, MySQL 8, and Redis 7.

The `docker-compose.yml` in the repository root is intended for local development only and must not be used for deployment. It differs from the release file [deploy/release/compose.yml](../deploy/release/compose.yml) as follows:

| | Root `docker-compose.yml` | `deploy/release/compose.yml` |
| --- | --- | --- |
| App | Built locally from the `server/` and `web/` sources, running as two containers, `Eco-server` and `Eco-web` | Pulls the All-in-One image and runs a single `Eco-hub` container |
| MySQL and Redis ports | Mapped to `127.0.0.1:3306` and `127.0.0.1:6379` on the host | Not mapped |
| Data | Docker named volumes `eco-mysql-data`, `eco-redis-data`, `eco-server-uploads` | `data/` in the install directory |

Two modes are supported:

1. Start only the databases and run the server and web app on the host (day-to-day development). The database credentials match `server/.env.example`:

   ```bash
   docker compose up -d mysql redis
   ```

   Start the server and the web app in two separate terminals:

   ```bash
   cd server && cp .env.example .env && go run ./cmd/server
   ```

   ```bash
   cd web && cp .env.example .env.local && npm install && npm run dev
   ```

   The site is available at `http://127.0.0.1:3000`, the admin panel at `http://127.0.0.1:3000/manage`, and the API at `http://127.0.0.1:8080`.

2. Build and run the entire stack from source as containers (to validate the Dockerfiles or for integration testing):

   ```bash
   docker compose up -d --build
   ```

   The site is available at `http://127.0.0.1:3000` and the API at `http://127.0.0.1:18080`. Ports and passwords can be changed in a `.env` file in the repository root (copied from [.env.example](../.env.example); the defaults apply when the file is absent).

> [!NOTE]
> - The container names `Eco-mysql` and `Eco-redis` are the same as in the release file. Do not run the development stack and a production deployment on the same machine.
> - If a MySQL or Redis instance on the host already uses port `3306` or `6379`, the development stack cannot start. Stop the host service, or use the host database instead.

For details, see [Server](../server/README_EN.md) and [Web](../web/README_EN.md).

## Documentation

| Document | Contents |
| --- | --- |
| [Deploy guide](./README-Deploy_EN.md) | Installation, the `ecohub` command, passwords, ports, 1Panel, reverse proxy, environment variables |
| [Upgrading from v2 to v3](./v2-to-v3_EN.md) | Stopping the old stack, reinstalling on an empty database, running a full collect |
| [FAQ](./README-FAQ_EN.md) | Empty site, main source, collect, cache, login |
| [Scheduled task conflicts](./cron-task-conflict_EN.md) | Execution order when collect and maintenance tasks are due at the same time |
| [Release notes](./RELEASE_EN.md) | Changes in the current release |
| [Docs index](./README.md) | All documents |

> EcoHub does not provide or store any media files. Content comes from collect APIs configured by the operator. Comply with local laws and the terms of use of each collect source.

[PolyForm Noncommercial 1.0.0](../LICENSE) · [fe-spark/EcoHub](https://github.com/fe-spark/EcoHub) · [Issues](https://github.com/fe-spark/EcoHub/issues) · [Telegram](https://t.me/ecohub_club)
