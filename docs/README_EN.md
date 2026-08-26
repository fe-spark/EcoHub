<div align="center">

<img src="../logo.png" alt="EcoHub" width="120" />

# EcoHub

[![Release](https://img.shields.io/github/v/release/fe-spark/EcoHub)](https://github.com/fe-spark/EcoHub/releases)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-16-black?logo=nextdotjs&logoColor=white)](https://nextjs.org/)
[![MySQL](https://img.shields.io/badge/MySQL-8-4479A1?logo=mysql&logoColor=white)](https://www.mysql.com/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?logo=redis&logoColor=white)](https://redis.io/)
[![License](https://img.shields.io/github/license/fe-spark/EcoHub)](../LICENSE)

**Self-hosted media aggregation**

[中文](../README.md) | English

[Demo](https://eco.fe-spark.cn) · [Admin](https://eco.fe-spark.cn/manage) · [Deploy](./README-Deploy_EN.md) · [FAQ](./README-FAQ_EN.md) · [Telegram Group](https://t.me/+6O6MiUdVSOplNjQ0)

</div>

> **Notice**  
> EcoHub does not provide or store media files. Sources originate from collect APIs configured by the operator. Comply with applicable laws and the terms of each source. The operator assumes all related risk. This project is intended for study and technical exchange.

## Overview

EcoHub is a high-performance, modern full-stack multi-source media aggregation system. It delivers a smooth web viewing experience and includes automated collection plus an admin panel, so developers and enthusiasts can quickly set up a private media library.

Client apps live in separate repos, not this repo's server or web UI:

- [EcoHub for OHOS](https://github.com/fe-spark/EcoHub-for-OHOS) (HAP)
- [EcoHub for Android](https://github.com/fe-spark/EcoHub-for-Android)

## Online demo

| Entry | URL |
| --- | --- |
| Site | [https://eco.fe-spark.cn](https://eco.fe-spark.cn) |
| Admin | [https://eco.fe-spark.cn/manage](https://eco.fe-spark.cn/manage) |

Read-only demo account: `guest` / `guest`. This account cannot save settings. For a real deployment, use your own accounts and change the default passwords.

## Recommendations

### Server

[CloudCone](https://app.cloudcone.com/?ref=14393) hosts the demonstration site and does **not throttle disk I/O**. Many low-cost VPS plans impose I/O limits, which can stall the database and collect jobs. Overseas instances do not require ICP filing and are available immediately after provisioning. They are suitable for collect workloads, deployments of this kind, and a self-hosted network egress.

### Proxy service

A stable network proxy is typically required when deploying overseas hosts, reaching collect sources, or debugging APIs. **LiangXinYun** is a direct-connect option:

- **¥2 / month for 100G**, ¥6 for 1000G (1T)
- Direct AWS and Oracle nodes; VLESS Reality and Hysteria2
- Netflix, Disney+, TikTok, and ChatGPT unblocked; no traffic audit; 1× multiplier
- Coverage includes Xinjiang, Henan, and Fujian; 4K playback during peak hours
- New registrations receive trial traffic

[Register LiangXinYun](https://xn--9kqz23b19z.com/#/register?code=xAmvfdic)

## Quick start

Docker 20+ and Compose 2+ are required. A minimum of 2 CPU cores and 2 GB RAM is recommended.

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
cd ~/ecohub
```

Edit `.env`: put the output of `openssl rand -hex 32` into `JWT_SECRET`, change the MySQL / Redis passwords, then start:

```bash
docker compose up -d
```

| URL | Purpose |
| --- | --- |
| `http://<host>:3000` | Public site |
| `http://<host>:3000/manage` | Administration panel |
| `http://<host>:3000/api/provide/config` | TVBox / YingShiCang subscription URL |
| `http://<host>:3000/api/provide/vod` | MacCMS-compatible API |
| `http://<host>:18080/api/*` | Direct API access (do not expose to the public internet) |

Default accounts: `admin` / `admin` (read/write), `guest` / `guest` (read-only). Default passwords must be changed before any public deployment.

An empty public site after installation is expected. Films appear only after a full collect has completed and been published in **Collect**. For 1Panel, an external database, or a reverse proxy, see the [Deploy guide](./README-Deploy_EN.md).

Telegram notifications are configured under **System settings → Notify**. Hosts in mainland China frequently time out when reaching Telegram; set `TG_PROXY=http://host.docker.internal:7890` in `.env`. See [server/notify.md](../server/notify.md).

## Local development

Start MySQL 8 and Redis 7 locally. Run the API and the web app in two terminals, both from the repository root.

API:

```bash
cd server
cp .env.example .env
go run ./cmd/server
```

Web:

```bash
cd web
cp .env.example .env.local
npm install
npm run dev
```

Public site: `http://127.0.0.1:3000`. Administration panel: `/manage`. API: `http://127.0.0.1:8080`. [Server](../server/README.md) · [Web](../web/README.md)

## Documentation

| Document | Contents |
| --- | --- |
| [Deploy](./README-Deploy_EN.md) | Install script, 1Panel, manual deploy, reverse proxy, upgrades |
| [FAQ](./README-FAQ_EN.md) | Empty catalog, collect, cache, authentication |
| [Release notes](./RELEASE.md) | Changelog, image tags |
| [Server](../server/README.md) / [Web](../web/README.md) | Environment variables, APIs, local startup |
| [Chinese README](../README.md) | 中文总览 |

## Community

- Telegram Group: [https://t.me/+6O6MiUdVSOplNjQ0](https://t.me/+6O6MiUdVSOplNjQ0)

## Star History

<a href="https://star-history.dera.page/#fe-spark/EcoHub&Date">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://star-history.dera.page/svg?repos=fe-spark/EcoHub&type=Date&theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://star-history.dera.page/svg?repos=fe-spark/EcoHub&type=Date" />
    <img alt="Star History Chart" src="https://star-history.dera.page/svg?repos=fe-spark/EcoHub&type=Date" />
  </picture>
</a>

---

[MIT](../LICENSE) · [fe-spark/EcoHub](https://github.com/fe-spark/EcoHub) · [Issues](https://github.com/fe-spark/EcoHub/issues) · [Telegram Group](https://t.me/+6O6MiUdVSOplNjQ0)
