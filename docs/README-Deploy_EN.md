# EcoHub Deploy Guide

[中文](./README-Deploy.md) | English

`ghcr.io/fe-spark/ecohub:latest` points to the latest formal release, v3. Pin a version with `ghcr.io/fe-spark/ecohub:v3.0.2`. Leaving `ECOHUB_IMAGE` unset makes compose use `:latest`.

The release image is all-in-one: Go API `:8080` and Next.js `:3000` in one container. The old `ecohub-web` / `ecohub-server` pair is retired.

Compose: [deploy/release/compose.yml](../deploy/release/compose.yml) · env template: [.env.example](../.env.example) · notes: [RELEASE.md](./RELEASE.md)

`ecohub` starts the app, MySQL, and Redis with Docker Compose. Local development does not use that command. See the [README](../README.md).

## Prerequisites

Docker 20+. The install script and 1Panel also need Compose 2+. Aim for 2 CPU cores and 2 GB RAM. The host must be able to pull `ghcr.io` and `docker.io`.

Web defaults to `3000`. The site, admin, and subscriptions use that port. Direct API defaults to `18080` for players only. Do not put subscriptions on that port.

```bash
openssl rand -hex 32
```

Own MySQL / Redis: remove the `mysql` and `redis` services and `depends_on`, then point `MYSQL_*` and `REDIS_*` at your instances. Do not use `127.0.0.1` from inside a container. A database on the host is `host.docker.internal`.

Upgrading from v2: [v2 to v3](./v2-to-v3.md).

## Install script

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2 sh
```

Data defaults to `~/ecohub`. Put `ECOHUB_INSTALL_DIR=~/ecohub-v3` before `sh` to use another directory. A variable before `curl` does not reach the script. The script writes compose and `.env`, installs `ecohub`, starts the stack, and prints the site, admin, three subscription URLs, and the initial account. Later commands are just `ecohub`.

If `/usr/local/bin` is not writable, the command goes to `~/.local/bin`. Add that directory to `PATH` when the shell says so.

Set `JWT_SECRET`, `ECOHUB_ADMIN_PASSWORD`, `MYSQL_ROOT_PASSWORD`, `MYSQL_PASSWORD`, and `REDIS_PASSWORD` before the first start. `install` and `update` write those exported values, plus `ECOHUB_IMAGE`, `WEB_PORT`, and `SERVER_PORT`, into `.env`. Other existing lines stay. Bundled databases keep `MYSQL_HOST=mysql` and `REDIS_HOST=redis`.

After the data directory is initialized, changing `ECOHUB_ADMIN_PASSWORD` or `MYSQL_PASSWORD` does not change the stored password. Changing the MySQL password and then running `ecohub restart` makes the app unable to connect. `JWT_SECRET`, ports, and `REDIS_PASSWORD` apply after you edit `.env` and run `ecohub restart`. `start` and `restart` do not copy the current shell environment over `.env`.

```bash
ecohub status
ecohub logs
ecohub logs -f
ecohub restart
ecohub update
ecohub stop
ecohub start
ecohub uninstall
ecohub uninstall --purge
```

`uninstall` removes containers and keeps data. `uninstall --purge` also deletes `data/mysql`, `data/redis`, and `data/uploads`. Both keep `.env` and the `ecohub` command. `ecohub update` keeps the `ECOHUB_IMAGE` already in `.env`. In-app upgrade pulls `:latest`.

| URL | What |
| --- | --- |
| `http://SERVER:3000` | Site |
| `http://SERVER:3000/manage` | Admin |
| `http://SERVER:3000/api/provide/app` | EcoHub client source |
| `http://SERVER:3000/api/provide/tvbox` | TVBox / YingShiCang |
| `http://SERVER:3000/api/provide/vod` | MacCMS |

`3000` is the default web port. Use the URLs the terminal prints. The account is `admin`. The password is `ECOHUB_ADMIN_PASSWORD`, or `admin` when unset. Change it after login.

Data is in `~/ecohub/data/mysql`, `data/redis`, and `data/uploads`.

The site has no films at first. Check the 12 preset sources and run a full collect. Films appear as each window is published. The first run can take several hours.

In production, reverse-proxy to `127.0.0.1:3000` and enable HTTPS. Do not send `/api` to `18080` by itself. To keep the raw port off the public internet, publish `127.0.0.1:${WEB_PORT:-3000}:3000`. Drop the `18080` mapping when players do not connect directly. Open only `80` / `443`.

These containers show up in 1Panel. Keep managing them with `ecohub`. Do not create a second compose project with the same container names.

## 1Panel

1. **Containers** → **Compose** → **Create**. Working directory for example `/opt/1panel/apps/ecohub`.
2. Paste [deploy/release/compose.yml](../deploy/release/compose.yml).
3. Environment:

```env
ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2
WEB_PORT=3000
SERVER_PORT=18080
JWT_SECRET=replace-with-a-long-random-string
ECOHUB_ADMIN_PASSWORD=replace-with-a-strong-password
MYSQL_ROOT_PASSWORD=replace-with-a-strong-password
MYSQL_HOST=mysql
MYSQL_PORT=3306
MYSQL_USER=eco
MYSQL_PASSWORD=replace-with-a-strong-password
MYSQL_DBNAME=eco
REDIS_HOST=redis
REDIS_PORT=6379
REDIS_PASSWORD=replace-with-a-strong-password
REDIS_DB=0
```

4. Save and start. Confirm `Eco-hub`, `Eco-mysql`, and `Eco-redis` are running.
5. **Website** → **Reverse proxy** → `http://127.0.0.1:3000`. Update with `docker compose pull && docker compose up -d` in that directory.

## Manual deploy

No Compose. Replace the passwords before running.

```bash
mkdir -p ~/ecohub/data/mysql ~/ecohub/data/redis ~/ecohub/data/uploads
docker network inspect Eco-network >/dev/null 2>&1 || docker network create Eco-network

MYSQL_ROOT_PASSWORD='use-a-strong-password'
MYSQL_PASSWORD='use-a-strong-password'
REDIS_PASSWORD='use-a-strong-password'
JWT_SECRET="$(openssl rand -hex 32)"

docker pull mysql:8.4
docker pull redis:7.4-alpine
docker pull ghcr.io/fe-spark/ecohub:v3.0.2

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
  -e ECOHUB_ADMIN_PASSWORD="replace-with-a-strong-password" \
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
  ghcr.io/fe-spark/ecohub:v3.0.2
```

Inside the app container the database hosts are `Eco-mysql` and `Eco-redis`. If you bring your own database, do not start the MySQL and Redis containers above. First-time MySQL init can take tens of seconds. If the ready check fails, read `docker logs Eco-mysql`.

Logs: `docker logs -f Eco-hub`. To update, `docker pull` the current tag, remove `Eco-hub`, and `docker run` it again with the same flags.

## Environment variables

| Variable | Default | Notes |
| --- | --- | --- |
| `ECOHUB_IMAGE` | `ghcr.io/fe-spark/ecohub:latest` | Image tag. See the top of this page |
| `WEB_PORT` | `3000` | Site, admin, subscriptions |
| `SERVER_PORT` | `18080` | Direct API. Not for subscriptions |
| `JWT_SECRET` | none, required | Login signing key |
| `ECOHUB_ADMIN_PASSWORD` | `admin` | Applied only when the admin user is first created |
| `MYSQL_HOST` | `mysql` | Do not use `127.0.0.1` inside a container |
| `MYSQL_PORT` | `3306` | |
| `MYSQL_USER` | `eco` | |
| `MYSQL_PASSWORD` | `ecohub` | Applied only on first data-directory init |
| `MYSQL_DBNAME` | `eco` | |
| `MYSQL_ROOT_PASSWORD` | `ecohub` | First init of the bundled MySQL only |
| `REDIS_HOST` | `redis` | |
| `REDIS_PORT` | `6379` | |
| `REDIS_PASSWORD` | `ecohub` | Applies on `ecohub restart` after editing `.env` |
| `REDIS_DB` | `0` | |
| `HTTPS_PROXY` / `HTTP_PROXY` / `ALL_PROXY` | empty | Outbound proxy for collect |
| `NODE_NAME` | hostname | Multi-node only |
| `ECOHUB_LOG_DIR` | `logs/` | Log directory |

## Troubleshooting

- Health: `http://HOST:3000/api/config/basic`. Port `18080` also works when it is published.
- Restart loop: `ecohub logs` or `docker logs Eco-hub`. Check that `JWT_SECRET` and the database passwords still match first init, and that the ports are free.
- Site opens, APIs fail: the proxy must target the web port. Do not set `API_URL` on the release image.

Behavior questions: [FAQ](./README-FAQ_EN.md).

## Related

[README](./README_EN.md) · [Release notes](./RELEASE.md) · [v2 to v3](./v2-to-v3.md)
