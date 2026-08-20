<div align="center">

<img src="../logo.png" alt="EcoHub Logo" width="128" />

# EcoHub

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8.svg)](https://golang.org/)
[![Next.js](https://img.shields.io/badge/Next.js-16-black.svg)](https://nextjs.org/)
[![MySQL](https://img.shields.io/badge/MySQL-8-4479A1.svg)](https://www.mysql.com/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D.svg)](https://redis.io/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED.svg)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](../LICENSE)

**Your own media site: watch on computer, TV, and phone**

[中文](../README.md) | English

One command installs the site and the admin panel. Add sources, run a collect, then use it in the browser, TVBox, or YingShiCang.

[Demo](https://eco.fe-spark.cn) · [Admin](https://eco.fe-spark.cn/manage) · [Deploy guide](./README-Deploy_EN.md) · [FAQ](./README-FAQ_EN.md)

</div>

## ⚠️ Before you start

- **For learning and exchange only.** This project does not provide or store any media files. Sources come from collect APIs you configure yourself.
- **Use it legally.** Follow the laws where you live and the terms of each source. You take all risk.
- **Star if it helps.** A ⭐ is the best way to support the project.

## ❤️ Recommendations

### Server

[CloudCone](https://app.cloudcone.com/?ref=14393) — the demo runs here. **No IO throttle** (many cheap VPS boxes choke IO, then the database and collect jobs freeze). Overseas machines skip ICP filing: you get the box and you can use it. Good for crawlers, this kind of site, and a personal proxy. Buying through the invite link supports the project.

### Proxy / VPN

A stable proxy is almost required when you deploy overseas, reach collect sources, or debug APIs. **LiangXinYun** is a high-value direct-connect option:

- **¥2 / month for 100G**, ¥6 for 1000G (1T)
- Direct AWS + Oracle high-spec nodes, VLESS Reality + Hysteria2
- Netflix / Disney+ / TikTok / ChatGPT unblocked, no audit, 1× traffic
- Works in restricted regions (Xinjiang, Henan, Fujian, etc.); 4K at peak hours
- New sign-ups get trial traffic

👉 Exclusive signup (supports this project): [Register LiangXinYun](https://xn--9kqz23b19z.com/#/register?code=xAmvfdic)

> Stable in personal use. Fine as a daily backup line plus heavy downloads. Monthly purchase is a simple way to limit risk.

## ✨ What it does

| You want to… | EcoHub |
| --- | --- |
| Run a media site for yourself or family | Open the browser: search, browse by category, play |
| Watch on a TV or set-top box | Paste one config URL into **TVBox / YingShiCang** |
| Merge several sites into one library | **Master** owns the catalog (titles, categories, descriptions). **Slaves** add extra play lines for the same title |
| Avoid clicking collect every day | Turn on scheduled tasks |
| Know when a show updates | Optional **Telegram** push after collect |
| Change homepage, banners, categories | All in the web admin. No code edits |

> A first start already includes built-in collect sources. Your job is: **enable → collect → wait → watch**.

## 📺 Try the demo

| Entry | URL |
| --- | --- |
| Site (watch) | [https://eco.fe-spark.cn](https://eco.fe-spark.cn) |
| Admin | [https://eco.fe-spark.cn/manage](https://eco.fe-spark.cn/manage) |
| Demo account | `guest` / `guest` (read-only) |

Current release **v2.1** is a single Docker image `ghcr.io/fe-spark/ecohub` (site + admin together). Changelog: [RELEASE.md](./RELEASE.md).

## 🚀 Install in five minutes

Install [Docker](https://docs.docker.com/get-docker/) 20+ and Docker Compose 2+ first. **2 CPU / 2G RAM** is a reasonable start.

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
cd ~/ecohub
# Edit .env: at least change JWT_SECRET and database passwords
docker compose up -d
```

Then open:

| URL | What |
| --- | --- |
| `http://SERVER_IP:3000` | Watch |
| `http://SERVER_IP:3000/manage` | Admin |
| `http://SERVER_IP:3000/api/provide/config` | TVBox / YingShiCang config |

Default accounts (**change passwords before exposing the site**):

| Role | User | Password | Access |
| --- | --- | --- | --- |
| Admin | `admin` | `admin` | Config, collect, films |
| Guest | `guest` | `guest` | Read only |

> 1Panel GUI, your own database, domain reverse proxy: [Deploy guide](./README-Deploy_EN.md).

The site is empty after install. That is expected. Open admin **采集中心**, run the first full collect (**several hours** is normal), and wait until it finishes and publishes. The admin has a first-run tour.

## 📱 Where to use it

This is your own site. Plug it in here:

### Browser

Open the site URL on a computer or phone. Share one link with family.

### TVBox / YingShiCang (TV, boxes)

1. Open Config / Subscribe
2. Paste (replace the IP):

```text
http://SERVER_IP:3000/api/provide/config
```

3. Save and refresh. An EcoHub source appears. Search, categories, and filters match the website.

If you have a domain and HTTPS, replace `http://SERVER_IP:3000` with that domain.

### Other compatible clients

MacCMS-style API:

```text
http://SERVER_IP:3000/api/provide/vod
```

Works with players or small tools that already speak this API.

### Telegram (optional)

Want “collect finished / this show updated” on your phone: open **系统设置 · 通知配置**, fill Bot Token and Chat ID. Servers in mainland China often time out talking to Telegram. In Docker you can add:

```env
TG_PROXY=http://host.docker.internal:7890
```

Use your local proxy port.

## 🛠️ Local development

Use this only when you change code. For personal hosting, Docker above is enough.

1. Run MySQL 8 and Redis 7 locally.
2. Start the API:

```bash
cd server
cp .env.example .env
go run ./cmd/server
```

3. Start the web app:

```bash
cd web
cp .env.example .env.local
npm install
npm run dev
```

4. Open:

- Site: `http://127.0.0.1:3000`
- Admin: `http://127.0.0.1:3000/manage`
- API: `http://127.0.0.1:8080`

More detail:

- [Server](../server/README.md)
- [Web](../web/README.md)

## 📚 More docs

| Doc | When |
| --- | --- |
| [Deploy guide](./README-Deploy_EN.md) | Install script, 1Panel, source compose, reverse proxy |
| [FAQ](./README-FAQ_EN.md) | Empty catalog, collect, cache, login, Docker |
| [Release notes](./RELEASE.md) | Upgrades, image tags |
| [Server](../server/README.md) | Env vars, APIs, auth (developers) |
| [Web](../web/README.md) | Pages and local start (developers) |

Chinese versions: [README.md](../README.md) · [Deploy](./README-Deploy.md) · [FAQ](./README-FAQ.md).

## Common snags

| Symptom | Check first |
| --- | --- |
| Site opens, zero titles | The first full collect can take several hours. Nothing shows until it finishes and publishes. Watch Collect progress |
| Collect keeps failing | Source URL is dead, or the server cannot reach the source (overseas box / proxy) |
| Admin opens, you cannot change anything | Are you on `guest`? Guests are read-only |
| TV search is empty | Config URL must be `.../api/provide/config`, and collect must have finished |

More: [FAQ](./README-FAQ_EN.md).

## Star History

<a href="https://star-history.dera.page/#fe-spark/EcoHub&Date">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://star-history.dera.page/svg?repos=fe-spark/EcoHub&type=Date&theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://star-history.dera.page/svg?repos=fe-spark/EcoHub&type=Date" />
    <img alt="Star History Chart" src="https://star-history.dera.page/svg?repos=fe-spark/EcoHub&type=Date" />
  </picture>
</a>

---

<div align="center">

MIT License · [fe-spark/EcoHub](https://github.com/fe-spark/EcoHub)

If this project helps you, please leave a ⭐ Star.

</div>
