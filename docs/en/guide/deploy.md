# EcoHub Deploy Guide

[中文](../../guide/deploy.md) | English

EcoHub is distributed as a single all-in-one image, `ghcr.io/fe-spark/ecohub`. One container runs the Go API (port `8080` inside the container) and the Next.js web app (port `3000` inside the container), alongside MySQL 8 and Redis 7.

Image tags:

| Tag | Points to | Use case |
| --- | --- | --- |
| `:v3.2.2`, etc. | A fixed release | Recommended. Change the tag manually to upgrade |
| `:latest` | The latest release, updated on every release | Always tracking the latest version |
| Tags ending in `-beta` | Test builds | Not for production use. Deleted automatically after the next release |

Referenced files: [compose file deploy/release/compose.yml](../../../deploy/release/compose.yml) · [variable template .env.example](../../../.env.example) · [release notes](../RELEASE.md). Before upgrading from v2, read [Upgrading from v2 to v3](./v2-to-v3.md).

## Prerequisites

- Docker 20+ and Docker Compose (either the `docker compose` plugin or standalone `docker-compose`; manual `docker run` does not require Compose).
- At least 2 CPU cores and 2 GB of RAM are recommended.
- Network access from the server to `ghcr.io` (app image) and `docker.io` (MySQL and Redis images).

Exposed ports:

| Port | Default | Purpose |
| --- | --- | --- |
| Web port | `3000` | Serves the site, the admin panel, and the subscription URLs |
| Direct API port | `18080` | Maps to `8080` in the container and serves `/api/*` directly; subscription URLs can also use this port; remove if unused |

Generate a random key for `JWT_SECRET`:

```bash
openssl rand -hex 32
```

## Method 1: install script (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.2.2 sh
```

The script performs these steps:

1. Creates `data/mysql`, `data/redis`, and `data/uploads` under the install directory (default `~/ecohub`).
2. Downloads the compose file as `docker-compose.yml` and downloads `.env.example`. When `.env` does not exist, it is generated from the template; an existing `.env` is kept, and `docker-compose.override.yml` is never created or modified.
3. Writes the variables placed before `sh` into `.env`. On a fresh install without a specified image, it writes `ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.7`.
4. Installs the `ecohub` command into `/usr/local/bin` (or `~/.local/bin` if not writable, with a prompt to add it to `PATH`), and records the install directory in `~/.ecohub/install-dir`.
5. Starts the containers and prints the site URL, admin URL, initial account, and three subscription URLs.

### Set passwords on the first install

Variables must be placed before `sh`; variables placed before `curl` are not passed to the script:

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.2.2 \
    JWT_SECRET="$(openssl rand -hex 32)" \
    ECOHUB_ADMIN_PASSWORD='use-a-strong-password' \
    MYSQL_ROOT_PASSWORD='use-a-strong-password' \
    MYSQL_PASSWORD='use-a-strong-password' \
    REDIS_PASSWORD='use-a-strong-password' \
    sh
```

`install` and `update` write only these 8 variables into `.env`: `ECOHUB_IMAGE`, `WEB_PORT`, `SERVER_PORT`, `JWT_SECRET`, `ECOHUB_ADMIN_PASSWORD`, `MYSQL_ROOT_PASSWORD`, `MYSQL_PASSWORD`, and `REDIS_PASSWORD`; other custom lines are kept. Without overrides, `.env` uses the public template defaults (admin `admin`, database `ecohub`), which must be changed before public exposure.

To use a different install directory, also place `ECOHUB_INSTALL_DIR=~/ecohub-v3` before `sh`. For day-to-day commands (`ecohub update`, `ecohub logs`, password/port reload behavior), see [Operations and configuration](./operations.md).

## Method 2: 1Panel

1. Create a Compose project in 1Panel with a working directory such as `/opt/1panel/apps/ecohub`.
2. Paste the contents of [deploy/release/compose.yml](../../../deploy/release/compose.yml).
3. Set the environment variables:

```env
ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.2.2
WEB_PORT=3000
SERVER_PORT=18080
JWT_SECRET=replace-with-a-long-random-string
ECOHUB_ADMIN_PASSWORD=use-a-strong-password
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

4. Save and start the project, verifying that `Eco-hub`, `Eco-mysql`, and `Eco-redis` are running.
5. Point a reverse proxy for the site to `http://127.0.0.1:3000`.

To upgrade, update `ECOHUB_IMAGE` and run in the working directory:

```bash
docker compose pull && docker compose up -d
```

## Method 3: manual docker run

This method does not use Compose. Replace the passwords below with strong random values before running:

```bash
mkdir -p ~/ecohub/data/mysql ~/ecohub/data/redis ~/ecohub/data/uploads
docker network inspect Eco-network >/dev/null 2>&1 || docker network create Eco-network

MYSQL_ROOT_PASSWORD='use-a-strong-password'
MYSQL_PASSWORD='use-a-strong-password'
REDIS_PASSWORD='use-a-strong-password'
JWT_SECRET="$(openssl rand -hex 32)"

docker pull mysql:8.4
docker pull redis:7.4-alpine
docker pull ghcr.io/fe-spark/ecohub:v3.2.2

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
  -e ECOHUB_ADMIN_PASSWORD="use-a-strong-password" \
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
  ghcr.io/fe-spark/ecohub:v3.2.2
```

- Inside the app container, the database hosts are the container names `Eco-mysql` and `Eco-redis`.
- The `until` loop waits for the first MySQL initialization to finish; inspect `docker logs Eco-mysql` if it stalls.
- `/var/run/docker.sock` is required only for the admin "立即升级并重启" (Upgrade now and restart) button and can be omitted if unused.

Logs: `docker logs -f Eco-hub`. Update: `docker pull` the new tag, run `docker rm -f Eco-hub`, and start a new container with the same arguments and new tag.

## URLs

| URL | Purpose |
| --- | --- |
| `http://<server-ip>:3000` | Site |
| `http://<server-ip>:3000/manage` | Admin |
| `http://<server-ip>:3000/api/provide/app` | EcoHub client subscription |
| `http://<server-ip>:3000/api/provide/tvbox` | TVBox / YingShiCang subscription |
| `http://<server-ip>:3000/api/provide/vod` | MacCMS subscription |

Subscription URLs can also use the direct API port (`18080`); response URLs are built from the incoming request address, while web pages are served only on the web port.

The initial admin account is `admin`, with password from `ECOHUB_ADMIN_PASSWORD` (default `admin`). Change it after the first sign-in. For subscription usage with private access enabled, see the [FAQ](./faq.md#how-do-subscriptions-work-with-private-access-on).

The site is empty at first. Open "采集中心" (Collect center) in the admin panel, review the 12 preset collect sources, and run a full collect.

## Reverse proxy

Recommendations for production:

- Point the reverse proxy for the entire site to `127.0.0.1:3000` and enable HTTPS; `/api` does not need a separate rule to `18080` because the web port forwards `/api/*` to Go inside the container.
- To avoid exposing `3000` publicly or to remove `18080`: use [docker-compose.override.yml](./operations.md#custom-compose-settings) for script-based installs, or edit `ports` directly in custom compose files.
- Allow only `80` and `443` in the cloud security group.

## Related documents

[Operations and configuration](./operations.md) · [Upgrading from v2 to v3](./v2-to-v3.md) · [FAQ](./faq.md) · [Release notes](../RELEASE.md)
