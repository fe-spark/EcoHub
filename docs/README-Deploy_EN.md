# EcoHub Deploy Guide

[中文](./README-Deploy.md) | English

Release image `ghcr.io/fe-spark/ecohub` (All-in-One: Supervisord in the same container runs Go API `:8080` and Next.js `:3000`). Use the **install script**, **1Panel**, or **manual deploy**.

> Compose: [deploy/release/compose.yml](../deploy/release/compose.yml) · env template: [.env.example](../.env.example) · versions: [RELEASE.md](./RELEASE.md)

The old `ecohub-web` / `ecohub-server` two-image setup is retired (v2.0+).

---

## Pick a path

| You want | Section |
| --- | --- |
| One command in the terminal | [Install script](#method-1-install-script-recommended) |
| 1Panel GUI | [1Panel](#method-2-1panel) |
| Run the images yourself | [Manual deploy](#method-3-manual-deploy) |

Using your own MySQL / Redis: drop the `mysql` / `redis` services and `depends_on` from the default compose, then point `.env` `MYSQL_*` / `REDIS_*` at your instances (do **not** use `127.0.0.1` from inside a container; the host DB is often `host.docker.internal`). Variable meanings: `.env.example`.

---

## Prerequisites

| Item | Notes |
| --- | --- |
| Docker | 20+; Methods 1 / 2 also need Compose 2+ (1Panel includes it) |
| Network | Can pull `ghcr.io` and `docker.io` (China may need a mirror) |
| Ports | At least the Web port free (default `3000`) |
| Resources | Aim for ≥ 2 CPU / 2G RAM; add RAM if you collect a lot |

Before going live, change at least `JWT_SECRET` and MySQL/Redis passwords.

```bash
openssl rand -hex 32
```

Optional: `TG_PROXY`, `HTTPS_PROXY` / `HTTP_PROXY` / `ALL_PROXY`, `COLLECT_PROFILE` (`auto|light|standard|high`). See `.env.example`.

---

## Method 1: Install script (recommended)

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

Pin a version by changing the compose image to `ghcr.io/fe-spark/ecohub:v2.0.1` (or similar). Release tags overwrite `:latest`. In the release admin you can click **Upgrade now and restart** (compose must mount `/var/run/docker.sock`; the new compose file already does). Mounting the socket means the process inside the container can talk to host Docker. Only super administrator accounts can trigger an upgrade. Drop that volume if you do not want in-app upgrades.

---

## Method 2: 1Panel

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
      - ${WEB_PORT:-3000}:3000
      - 0.0.0.0:${SERVER_PORT:-18080}:8080
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
WEB_PORT=3000
SERVER_PORT=18080

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
- If the port is taken, change `WEB_PORT` and match the reverse proxy.

### 3. Start

Save → start. Confirm `Eco-hub`, `Eco-mysql`, and `Eco-redis` are running. URLs are the same as Method 1.

You can also run the install script first, then inspect / take over the stack in 1Panel’s container list.

### 4. Website, HTTPS, and Avoiding Public Port Exposure

1. **Website** → **Create site** → **Reverse proxy** → `http://127.0.0.1:3000` (or your `WEB_PORT`).
2. Issue Let’s Encrypt or upload a cert, enable HTTPS.
3. Do **not** point `/api/*` at `18080` by itself. Send the whole site through `3000`; Next inside the container forwards to Go.
4. **Avoid exposing raw ports to the public internet (Recommended)**:
   - Docker’s default iptables rules bypass system firewalls (like UFW). To **prevent direct public access to port 3000**, explicitly bind `127.0.0.1:` in `compose.yml` (e.g. `127.0.0.1:${WEB_PORT:-3000}:3000`), so only the local reverse proxy can reach it.
   - If direct player access is not needed, comment out or remove the `18080:8080` port mapping.
   - Open `80`/`443` on the firewall. Do **not** expose `18080`, MySQL, or Redis to the public internet.

### 5. Update

Change the image tag in compose (optional) → pull → recreate, or:

```bash
docker compose pull && docker compose up -d
```

Data lives in `./data/mysql`, `./data/redis`, `./data/uploads`.

---

## Method 3: Manual deploy

No Compose. Create a network and data directories, then run MySQL, Redis, and `ghcr.io/fe-spark/ecohub`. URLs and default accounts are the same as Method 1.

Replace the passwords and secret below before running.

```bash
mkdir -p ~/ecohub/data/mysql ~/ecohub/data/redis ~/ecohub/data/uploads
docker network inspect Eco-network >/dev/null 2>&1 || docker network create Eco-network

MYSQL_ROOT_PASSWORD='use-a-strong-password'
MYSQL_PASSWORD='use-a-strong-password'
REDIS_PASSWORD='use-a-strong-password'
JWT_SECRET="$(openssl rand -hex 32)"

docker pull mysql:8.4
docker pull redis:7.4-alpine
docker pull ghcr.io/fe-spark/ecohub:latest

docker run -d --name Eco-mysql --restart always --network Eco-network \
  -e MYSQL_ROOT_PASSWORD="$MYSQL_ROOT_PASSWORD" \
  -e MYSQL_DATABASE=eco \
  -e MYSQL_USER=eco \
  -e MYSQL_PASSWORD="$MYSQL_PASSWORD" \
  -v ~/ecohub/data/mysql:/var/lib/mysql \
  mysql:8.4

docker run -d --name Eco-redis --restart always --network Eco-network \
  -v ~/ecohub/data/redis:/data \
  redis:7.4-alpine \
  redis-server --requirepass "$REDIS_PASSWORD"

until docker exec Eco-mysql mysql -ueco -p"$MYSQL_PASSWORD" -e 'SELECT 1' eco >/dev/null 2>&1; do
  sleep 2
done

docker run -d --name Eco-hub --restart always --network Eco-network \
  --add-host=host.docker.internal:host-gateway \
  -p 3000:3000 \
  -p 18080:8080 \
  -e PORT=8080 \
  -e JWT_SECRET="$JWT_SECRET" \
  -e MYSQL_HOST=Eco-mysql \
  -e MYSQL_PORT=3306 \
  -e MYSQL_USER=eco \
  -e MYSQL_PASSWORD="$MYSQL_PASSWORD" \
  -e MYSQL_DBNAME=eco \
  -e REDIS_HOST=Eco-redis \
  -e REDIS_PORT=6379 \
  -e REDIS_PASSWORD="$REDIS_PASSWORD" \
  -e REDIS_DB=0 \
  -v ~/ecohub/data/uploads:/app/static/upload \
  -v /var/run/docker.sock:/var/run/docker.sock \
  ghcr.io/fe-spark/ecohub:latest
```

Inside the app container the databases are `Eco-mysql` / `Eco-redis`, not `127.0.0.1`. If you bring your own database, do not start the MySQL / Redis containers above; set `MYSQL_HOST` / `REDIS_HOST` to an address the container can reach (`host.docker.internal` for a database on the host). First-time MySQL data-dir init can take tens of seconds; if the ready check fails, inspect `docker logs Eco-mysql`.

Logs: `docker logs -f Eco-hub`. To update: `docker pull ghcr.io/fe-spark/ecohub:latest`, remove Eco-hub, and `docker run` it again with the same flags (or use **Upgrade now and restart** in admin, which requires `/var/run/docker.sock`).

---

## Ports

| Variable | Default | Notes |
| --- | --- | --- |
| `WEB_PORT` | `3000` | Host Web access port |
| `SERVER_PORT` | `18080` | Host direct API access port (optional) |

In production, expose only the Web port (or 80/443 behind a reverse proxy). To **completely prevent direct public access to raw ports**, set the port mapping in `compose.yml` to `127.0.0.1:${WEB_PORT:-3000}:3000`, making it accessible only via the local host and reverse proxy.

---

## Cluster & Multi-Node Deployment (`CLUSTER_ROLE`)

When deploying across multiple VPS nodes for load balancing and high concurrency, specify node roles via environment variables:
- `CLUSTER_ROLE=master` (Default): Master node, runs both Next.js Web and Go backend, handles management, database writes, and scheduled collect jobs (Cron).
- `CLUSTER_ROLE=worker`: Read-only replica, dedicated to handling high-concurrency TVBox, YingShiCang, MacCMS, and public API traffic (automatically disables scheduled collectors, skips Redis cache purges, and syncs snapshots via Redis Pub/Sub; also runs Next.js Web process, naturally enabling active-active load balancing and seamless zero-downtime failover);
- **Reverse Proxy (Nginx) Best Practices**:
  - **Public Web Browsing (`/`)**: Recommended **Active-Active load balancing mode** (Master and Workers share web rendering and search traffic based on weights), paired with `proxy_next_upstream` so all traffic automatically fails over to Workers if Master restarts, achieving both **multiplied capacity and zero downtime** (or mark Workers as `backup` if cold standby is preferred).
  - **Administration UI and write APIs (`/manage`, `/api/manage/*`)**: Strictly routed to the Master node.
  - **Custom Uploaded Assets (`/api/upload/`)**: Strictly routed to the Master node (logos and custom banners live on Master disk; this allows **Worker nodes to operate 100% statelessly without complex NFS mounts**).
  - **High-concurrency read-only APIs (`/api/`)**: Load balanced across Master and Worker nodes (TVBox, YingShiCang, and public search/browse queries).
  - **Nginx Configuration Example (Active-Active Load Balancing + Zero-Downtime Failover)**:
    ```nginx
    # 1. Public API cluster (Master & Workers load balanced)
    upstream eco_cluster_api {
        server 192.168.1.10:8080 weight=1 max_fails=2 fail_timeout=5s; # Master Node API
        server 192.168.1.11:8080 weight=2 max_fails=2 fail_timeout=5s; # Worker 1 Node API
        server 192.168.1.12:8080 weight=2 max_fails=2 fail_timeout=5s; # Worker 2 Node API
    }

    # 2. Public Web cluster (Active-Active mode; or add 'backup' to Workers for cold standby)
    upstream eco_web_cluster {
        server 192.168.1.10:3000 weight=1 max_fails=2 fail_timeout=5s; # Master Web frontend
        server 192.168.1.11:3000 weight=2 max_fails=2 fail_timeout=5s; # Worker 1 Web frontend
    }

    upstream eco_master_api {
        server 192.168.1.10:8080;                                      # Master API (writes, management, and local uploads)
    }

    server {
        listen 80;
        server_name your-domain.com;

        # Administration UI routed to Master
        location /manage {
            proxy_pass http://192.168.1.10:3000;
        }

        # Administration write/upload APIs routed to Master
        location /api/manage/ {
            proxy_pass http://eco_master_api;
        }

        # Custom uploaded static assets (logos, banners stored on Master disk)
        location /api/upload/ {
            proxy_pass http://eco_master_api;
        }

        # High-concurrency read-only APIs load balanced across cluster
        location /api/ {
            proxy_pass http://eco_cluster_api;
            proxy_next_upstream error timeout http_502 http_503 http_504;
            proxy_connect_timeout 2s;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        }

        # Default web browsing requests routed to web cluster (Active-Active load balanced, fails over if Master restarts)
        location / {
            proxy_pass http://eco_web_cluster;
            proxy_next_upstream error timeout http_502 http_503 http_504;
            proxy_connect_timeout 2s;
            proxy_read_timeout 15s;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        }
    }
    ```
- **Deployment order & data sync notes**:
  - **Start Master first, then scale Workers**: Workers poll Redis for the snapshot version/revision every 3s and auto-align with the Master's read model and search index; if Master has not yet published the first snapshot, Workers load it automatically once it appears — no restart needed.
  - **100% Stateless Worker deployment**: Snapshot data lives in the shared MySQL database (`film_list_snapshot` table) and syncs via Redis Pub/Sub; static uploads are served by Master via Nginx `/api/upload/`. Consequently, **Worker nodes do not need NFS mounts or local persistent volumes**, allowing instantaneous horizontal scaling.
  - **Workers are read-only at the application layer**: write endpoints under `/api/manage/*` (including uploads) are rejected directly by the backend (HTTP 403). Reverse-proxy routing is only the first layer of protection — defense in depth against accidental writes.

---

## Common commands

Methods 1 / 2:

```bash
docker compose ps
docker compose logs -f ecohub
docker compose restart ecohub
docker compose down
```

Method 3:

```bash
docker logs -f Eco-hub
docker restart Eco-hub
docker inspect Eco-hub --format '{{range .Config.Env}}{{println .}}{{end}}'
```

Data is in the install directory `data/` (Method 3 defaults to `~/ecohub/data`).

---

## Troubleshooting

- Health: `http://HOST:18080/api/health`
- Methods 1 / 2 restart loop: check `.env` passwords, `JWT_SECRET`, port conflicts, `docker pull ghcr.io/fe-spark/ecohub:latest`
- Method 3 restart loop: `docker logs Eco-hub`; use `docker inspect Eco-hub` for env and ports
- Site opens, APIs fail: reverse proxy should target Web only; do not set `API_URL` on the release image
- Telegram never sends: Methods 1 / 2 set `TG_PROXY` in `.env`; Method 3 add `-e TG_PROXY=...` to `docker run`; Token is configured in admin

More: [README-FAQ_EN.md](./README-FAQ_EN.md)

---

## Security

- Change default account passwords immediately
- Unique `JWT_SECRET` per environment
- Do not commit production passwords
- Prefer HTTPS; do not expose databases or `18080` to the public internet

---

## Related docs

- [README_EN.md](./README_EN.md) · [RELEASE.md](./RELEASE.md) · [FAQ](./README-FAQ_EN.md)
