# EcoHub Deploy Guide

[中文](./README-Deploy.md) | English

Release image `ghcr.io/fe-spark/ecohub` (All-in-One: Supervisord in the same container runs Go API `:8080` and Next.js `:3000`). Most people only need the **install script** or **1Panel**.

> Compose: [deploy/release/compose.yml](../deploy/release/compose.yml) · env template: [.env.example](../.env.example) · versions: [RELEASE.md](./RELEASE.md)

The old `ecohub-web` / `ecohub-server` two-image setup is retired (v2.0+).

---

## Pick a path

| You want | Section |
| --- | --- |
| One command in the terminal | [Install script](#method-a-install-script-recommended) |
| 1Panel GUI | [1Panel](#method-b-1panel) |
| Build from source locally | [Source compose](#method-c-source-compose) |
| Local development | [README_EN.md](./README_EN.md) “Local development” |

Using your own MySQL / Redis: drop the `mysql` / `redis` services and `depends_on` from the default compose, then point `.env` `MYSQL_*` / `REDIS_*` at your instances (do **not** use `127.0.0.1` from inside a container; the host DB is often `host.docker.internal`). Variable meanings: [server/README.md](../server/README.md).

---

## Prerequisites

| Item | Notes |
| --- | --- |
| Docker | 20+, Compose 2+ (1Panel already includes this) |
| Network | Can pull `ghcr.io` and `docker.io` (China may need a mirror) |
| Ports | At least the Web port free (default `3000`) |
| Resources | Aim for ≥ 2 CPU / 2G RAM; add RAM if you collect a lot |

Before going live, change at least `JWT_SECRET` and MySQL/Redis passwords.

```bash
openssl rand -hex 32
```

Optional: `TG_PROXY`, `HTTPS_PROXY` / `HTTP_PROXY` / `ALL_PROXY`, `COLLECT_PROFILE` (`auto|light|standard|high`). See `.env.example`.

---

## Method A: Install script (recommended)

Default three containers: `Eco-hub` (app), `Eco-mysql`, `Eco-redis`.

### 1. Install

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
```

Writes into `~/ecohub`: downloads `docker-compose.yml` (from `deploy/release/compose.yml`) and `.env.example`; copies `.env` if it does not exist yet.

### 2. Edit config and start

```bash
cd ~/ecohub
# Edit .env: JWT_SECRET, MYSQL_*, REDIS_PASSWORD, etc.
docker compose up -d
```

With the bundled databases keep `MYSQL_HOST=mysql` and `REDIS_HOST=redis` (Compose service names).

### 3. Open the site

| URL | What |
| --- | --- |
| `http://SERVER:3000` | Site |
| `http://SERVER:3000/manage` | Admin |
| `http://SERVER:3000/api/*` | API via the site |
| `http://SERVER:18080/api/*` | Direct API (do not expose this on the public internet) |
| `http://SERVER:3000/api/provide/config` | TVBox / YingShiCang |

Default accounts (**change passwords immediately**): `admin` / `admin`, `guest` / `guest`.

First run: configure **collect sources** in admin and run a collect, or the site has no titles. The first full collect **can take several hours**. **Data shows only after collect finishes and publishes.**

### 4. Data and updates

```text
~/ecohub/data/mysql
~/ecohub/data/redis
~/ecohub/data/uploads
```

```bash
cd ~/ecohub
docker compose pull
docker compose up -d
```

Pin a version by changing the compose image to `ghcr.io/fe-spark/ecohub:v2.0.1` (or similar). Release tags overwrite `:latest`. In the release admin you can click **Upgrade now and restart** (compose must mount `/var/run/docker.sock`; the new compose file already does). Mounting the socket means the process inside the container can talk to host Docker. Only write-capable accounts can trigger an upgrade. Drop that volume if you do not want in-app upgrades.

### 5. Upgrade from v1.x

1. Back up `data/`.
2. Stop the old stack (`ecohub-web` / `ecohub-server` and so on) and switch to the All-in-One release compose (`ghcr.io/fe-spark/ecohub`).
3. Point `.env` at the same database, then `pull` + `up -d`.
4. Do not let old and new servers share the same database at once.

#### Align data (recommended)

After a v1 database lands on v2, **reset site data and run one full collect** so ContentKey, snapshots, the category tree, and multi-source playlists follow v2 rules. Leftover dirty data can mismatch titles or lists.

- Admin → **Reset site data** (or equivalent empty of business tables)
- Reconfigure / confirm collect sources (master + slaves) → run a **full collect** (not incremental only)
- User-uploaded assets (`data/uploads`) can stay; film data and play sources follow this full collect

Skipping the reset often still runs on v2, but data may not fully match the new model. If something looks wrong, follow the steps above.

---

## Method B: 1Panel

### 1. Create a compose stack

1. **Containers** → **Compose** → **Create**, name it e.g. `ecohub`.
2. Working directory e.g. `/opt/1panel/apps/ecohub`.
3. Paste the same content as [deploy/release/compose.yml](../deploy/release/compose.yml):

```yaml
services:
  mysql:
    container_name: Eco-mysql
    image: mysql:8.4
    restart: always
    environment:
      MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD:-ecohub}
      MYSQL_DATABASE: ${MYSQL_DBNAME:-eco}
      MYSQL_USER: ${MYSQL_USER:-eco}
      MYSQL_PASSWORD: ${MYSQL_PASSWORD:-ecohub}
    volumes:
      - ./data/mysql:/var/lib/mysql
    networks:
      - Eco-network
    healthcheck:
      test:
        [
          "CMD-SHELL",
          "mysqladmin ping -h 127.0.0.1 -uroot -p$$MYSQL_ROOT_PASSWORD --silent",
        ]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 30s

  redis:
    container_name: Eco-redis
    image: redis:7.4-alpine
    restart: always
    environment:
      REDIS_PASSWORD: ${REDIS_PASSWORD:-ecohub}
    command: ["redis-server", "--requirepass", "${REDIS_PASSWORD:-ecohub}"]
    volumes:
      - ./data/redis:/data
    networks:
      - Eco-network
    healthcheck:
      test: ["CMD-SHELL", "redis-cli -a $${REDIS_PASSWORD} ping | grep PONG"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 10s

  ecohub:
    container_name: Eco-hub
    image: ghcr.io/fe-spark/ecohub:latest
    restart: always
    environment:
      PORT: ${SERVER_PORT:-8080}
      JWT_SECRET: ${JWT_SECRET:-ecohub_2026!local@dev_secret$$001}
      MYSQL_HOST: ${MYSQL_HOST:-mysql}
      MYSQL_PORT: ${MYSQL_PORT:-3306}
      MYSQL_USER: ${MYSQL_USER:-eco}
      MYSQL_PASSWORD: ${MYSQL_PASSWORD:-ecohub}
      MYSQL_DBNAME: ${MYSQL_DBNAME:-eco}
      REDIS_HOST: ${REDIS_HOST:-redis}
      REDIS_PORT: ${REDIS_PORT:-6379}
      REDIS_PASSWORD: ${REDIS_PASSWORD:-ecohub}
      REDIS_DB: ${REDIS_DB:-0}
      TG_PROXY: ${TG_PROXY:-}
      HTTPS_PROXY: ${HTTPS_PROXY:-}
      HTTP_PROXY: ${HTTP_PROXY:-}
      ALL_PROXY: ${ALL_PROXY:-}
      COLLECT_PROFILE: ${COLLECT_PROFILE:-auto}
    ports:
      - ${WEB_PUBLIC_PORT:-3000}:3000
      - 0.0.0.0:${SERVER_PUBLIC_PORT:-18080}:${SERVER_PORT:-8080}
    volumes:
      - ./data/uploads:/app/static/upload
      - /var/run/docker.sock:/var/run/docker.sock
    networks:
      - Eco-network
    extra_hosts:
      - "host.docker.internal:host-gateway"
    depends_on:
      mysql:
        condition: service_healthy
      redis:
        condition: service_healthy
    healthcheck:
      test:
        ["CMD-SHELL", "wget -q -O /dev/null http://localhost:8080/api/health"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 30s

networks:
  Eco-network:
    driver: bridge
```

### 2. Environment variables

```env
WEB_PUBLIC_PORT=3000
SERVER_PUBLIC_PORT=18080
SERVER_PORT=8080

JWT_SECRET=replace-with-a-long-random-string

MYSQL_ROOT_PASSWORD=use-a-strong-password
MYSQL_HOST=mysql
MYSQL_PORT=3306
MYSQL_USER=eco
MYSQL_PASSWORD=use-a-strong-password
MYSQL_DBNAME=eco

REDIS_HOST=redis
REDIS_PORT=6379
REDIS_PASSWORD=use-a-strong-password
REDIS_DB=0
```

- Bundled databases: `MYSQL_HOST=mysql` / `REDIS_HOST=redis`. Do not write `127.0.0.1`.
- Release image does **not** need `API_URL`.
- If the port is taken, change `WEB_PUBLIC_PORT` and match the reverse proxy.

### 3. Start

Save → start. Confirm `Eco-hub`, `Eco-mysql`, and `Eco-redis` are running. URLs are the same as Method A.

You can also run the install script first, then inspect / take over the stack in 1Panel’s container list.

### 4. Website and HTTPS

1. **Website** → **Create site** → **Reverse proxy** → `http://127.0.0.1:3000` (or your `WEB_PUBLIC_PORT`).
2. Issue Let’s Encrypt or upload a cert, enable HTTPS.
3. Do **not** point `/api/*` at `18080` by itself. Send the whole site through `3000`; Next inside the container forwards to Go.
4. Open `80`/`443` on the firewall. Do **not** expose `18080`, MySQL, or Redis to the public internet.

### 5. Update

Change the image tag in compose (optional) → pull → recreate, or:

```bash
docker compose pull && docker compose up -d
```

Data lives in `./data/mysql`, `./data/redis`, `./data/uploads`.

---

## Method C: Source compose

For development or building yourself. Root [docker-compose.yml](../docker-compose.yml) (`web` + `server` built locally). **Production should use the All-in-One release.**

```bash
cp .env.example .env
# Change JWT_SECRET, passwords, etc.
docker compose up --build -d
```

Ports match the release. Source compose injects `API_URL`; the release image does not need it.

---

## Ports

| Variable | Default | Notes |
| --- | --- | --- |
| `WEB_PUBLIC_PORT` | `3000` | Site and admin |
| `SERVER_PUBLIC_PORT` | `18080` | Direct API mapping |
| `SERVER_PORT` | `8080` | Go listen port inside the container (injected as `PORT`) |

In production, expose only the Web port (or 80/443 behind a reverse proxy).

---

## Common commands

```bash
# Release
docker compose ps
docker compose logs -f ecohub
docker compose restart ecohub
docker compose down

# Source
docker compose logs -f web
docker compose logs -f server
```

Release data is in the install directory `data/`. Source compose uses Docker volumes by default (`down -v` deletes them).

---

## Troubleshooting

- Health: `http://HOST:18080/api/health`
- Container restart loop: check `.env` passwords, `JWT_SECRET`, port conflicts, `docker pull ghcr.io/fe-spark/ecohub:latest`
- Site opens, APIs fail: reverse proxy should target Web only; do not set `API_URL` on the release image
- Telegram never sends: set `TG_PROXY`; Token is configured in admin

More: [README-FAQ_EN.md](./README-FAQ_EN.md)

---

## Security

- Change default account passwords immediately
- Unique `JWT_SECRET` per environment
- Do not commit production passwords
- Prefer HTTPS; do not expose databases or `18080` to the public internet

---

## Related docs

- [README_EN.md](./README_EN.md) · [RELEASE.md](./RELEASE.md) · [server/README.md](../server/README.md) · [web/README.md](../web/README.md) · [FAQ](./README-FAQ_EN.md)
