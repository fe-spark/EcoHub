# EcoHub Deploy Guide

[中文](./README-Deploy.md) | English

EcoHub is distributed as a single all-in-one image, `ghcr.io/fe-spark/ecohub`. One container runs the Go API (port `8080` inside the container) and the Next.js web app (port `3000` inside the container). A MySQL instance and a Redis instance are also required.

Image tags:

| Tag | Points to | Use case |
| --- | --- | --- |
| `:v3.0.4`, etc. | A fixed release | Recommended. Change the tag manually to upgrade |
| `:latest` | The latest release, updated on every release | Always tracking the latest version |
| Tags ending in `-beta` | Test builds | Not for production use. Deleted automatically after the next release |

Files referenced in this guide: [compose file deploy/release/compose.yml](../deploy/release/compose.yml) · [variable template .env.example](../.env.example) · [release notes](./RELEASE_EN.md). Before upgrading from v2, read [Upgrading from v2 to v3](./v2-to-v3_EN.md).

## Prerequisites

- Docker 20+ and Docker Compose (either the `docker compose` plugin or standalone `docker-compose`; the manual `docker run` method does not need Compose).
- At least 2 CPU cores and 2 GB of RAM are recommended.
- Network access from the server to `ghcr.io` (app image) and `docker.io` (MySQL and Redis images).

Exposed ports:

| Port | Default | Purpose |
| --- | --- | --- |
| Web port | `3000` | Serves the site, the admin panel, and the subscription URLs |
| Direct API port | `18080` | Maps to `8080` in the container and serves the backend `/api/*` directly. Subscription URLs can also use this port; see [URLs](#urls). Can be removed if not needed |

Generate a random key for `JWT_SECRET`:

```bash
openssl rand -hex 32
```

## Method 1: install script (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.4 sh
```

The script performs the following steps:

1. Creates `data/mysql`, `data/redis`, and `data/uploads` in the install directory (default `~/ecohub`).
2. Downloads the compose file as `docker-compose.yml` and downloads `.env.example`. If `.env` does not exist, it is generated from the template; an existing `.env` is preserved. `docker-compose.override.yml` is never created or modified.
3. Writes the variables placed before `sh` into `.env` (see below). On a fresh install without a specified image, it writes `ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.4`.
4. Installs the `ecohub` command into `/usr/local/bin`. If that directory is not writable, the command is installed into `~/.local/bin` and the script prompts to add it to `PATH`.
5. Records the install directory in `~/.ecohub/install-dir`. Subsequent `ecohub` commands operate on that directory.
6. Starts the containers and prints the site URL, the admin URL, the initial account, and the three subscription URLs.

### Set passwords on the first install

Variables must be placed before `sh`. Variables placed before `curl` are not passed to the script. Example:

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.4 \
    JWT_SECRET="$(openssl rand -hex 32)" \
    ECOHUB_ADMIN_PASSWORD='use-a-strong-password' \
    MYSQL_ROOT_PASSWORD='use-a-strong-password' \
    MYSQL_PASSWORD='use-a-strong-password' \
    REDIS_PASSWORD='use-a-strong-password' \
    sh
```

`install` and `update` write only these 8 variables into `.env`: `ECOHUB_IMAGE`, `WEB_PORT`, `SERVER_PORT`, `JWT_SECRET`, `ECOHUB_ADMIN_PASSWORD`, `MYSQL_ROOT_PASSWORD`, `MYSQL_PASSWORD`, `REDIS_PASSWORD`. Variables that are not passed remain unchanged, and other lines in `.env` are preserved.

If no passwords are passed, `.env` uses the public defaults from the template (`JWT_SECRET` is a sample value committed to the repository, the database passwords are `ecohub`, and the admin password is `admin`). These values must be changed before the server is exposed to the internet.

To use a different install directory, also place `ECOHUB_INSTALL_DIR=~/ecohub-v3` before `sh`.

### When password and port changes take effect

| Variable | Edit `.env`, then `ecohub restart` | Notes |
| --- | --- | --- |
| `WEB_PORT`, `SERVER_PORT` | Takes effect | |
| `REDIS_PASSWORD` | Takes effect | Redis sets its password from this value on every start |
| `JWT_SECRET` | Takes effect | All signed-in users must sign in again |
| `ECOHUB_ADMIN_PASSWORD` | No effect | Written only when the `admin` account is first created. Change the password in the admin panel afterwards |
| `MYSQL_PASSWORD`, `MYSQL_ROOT_PASSWORD` | No effect, and causes connection failures | MySQL sets its passwords only when `data/mysql` is first initialized |

Example: after installation, `MYSQL_PASSWORD` in `.env` is changed from `ecohub` to `abc`, followed by `ecohub restart`. The app connects with `abc`, but MySQL still stores `ecohub`. The connection fails and the container restarts repeatedly.

`ecohub start` and `ecohub restart` only read `.env`. They do not write variables from the current shell into `.env`.

### The ecohub command

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

| Command | Description |
| --- | --- |
| `ecohub install` | Downloads the compose file, starts the services, and prints the URLs. Running `ecohub` without arguments is equivalent |
| `ecohub status` | Prints the URLs and the status of each container |
| `ecohub logs` | Shows the last 100 log lines. `logs -f` follows the output; `logs ecohub` shows only the app logs |
| `ecohub start` | Starts with the current `.env` |
| `ecohub stop` | Stops the containers and keeps the data |
| `ecohub restart` | Recreates the containers to apply changes in `.env` |
| `ecohub update` | Downloads the compose file again, pulls the image set in `ECOHUB_IMAGE` in `.env`, and recreates the containers |
| `ecohub uninstall` | Removes the containers. Keeps `data/`, `.env`, and the `ecohub` command |
| `ecohub uninstall --purge` | Additionally deletes `data/mysql`, `data/redis`, and `data/uploads`. `.env` and the `ecohub` command are kept |
| `ecohub help` | Shows usage |

To upgrade, change `ECOHUB_IMAGE` in `.env` to the new tag and run `ecohub update`, or run `ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:<new-version> ecohub update`.

`ecohub update` downloads `docker-compose.yml` again on every run, so direct edits to that file are overwritten. Place compose changes in `docker-compose.override.yml` instead; see [Custom compose settings](#custom-compose-settings).

The admin button "立即升级并重启" (Upgrade now and restart) pulls `:latest` and does not use the tag pinned in `.env`.

### Custom compose settings

Create `docker-compose.override.yml` in the install directory (default `~/ecohub`) containing only the settings to change. `ecohub install` and `ecohub update` never create, overwrite, or delete this file. When it exists, every `ecohub` command (`start`, `restart`, `update`, `status`, and so on) applies it, and `ecohub status` prints "已叠加 docker-compose.override.yml" (override applied).

Example: the web port listens on localhost only (behind a reverse proxy), and the direct API port is not exposed:

```yaml
services:
  ecohub:
    ports: !override
      - "127.0.0.1:${WEB_PORT:-3000}:3000"
```

Run `ecohub restart` afterwards.

- `!override` replaces the original `ports` list. Without it, Compose merges both lists and the original binding on all interfaces remains.
- `!override` requires Docker Compose v2.24.4 or later. Check the version with `docker compose version`. Standalone `docker-compose` v1 does not support it.
- To revert, delete the file and run `ecohub restart`.

## Method 2: 1Panel

1. Create a Compose project in 1Panel, with a working directory such as `/opt/1panel/apps/ecohub`.
2. Paste the complete contents of [deploy/release/compose.yml](../deploy/release/compose.yml).
3. Set the environment variables:

```env
ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.4
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

4. Save and start the project. Verify that `Eco-hub`, `Eco-mysql`, and `Eco-redis` are running.
5. Configure a reverse proxy for the site that points to `http://127.0.0.1:3000`.

To update, set the new `ECOHUB_IMAGE` and run the following in the working directory:

```bash
docker compose pull && docker compose up -d
```

## Method 3: manual docker run

This method does not use Compose. Replace the passwords below with strong random values before running the commands.

```bash
mkdir -p ~/ecohub/data/mysql ~/ecohub/data/redis ~/ecohub/data/uploads
docker network inspect Eco-network >/dev/null 2>&1 || docker network create Eco-network

MYSQL_ROOT_PASSWORD='use-a-strong-password'
MYSQL_PASSWORD='use-a-strong-password'
REDIS_PASSWORD='use-a-strong-password'
JWT_SECRET="$(openssl rand -hex 32)"

docker pull mysql:8.4
docker pull redis:7.4-alpine
docker pull ghcr.io/fe-spark/ecohub:v3.0.4

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
  ghcr.io/fe-spark/ecohub:v3.0.4
```

- Inside the app container, the database hosts are the container names `Eco-mysql` and `Eco-redis`.
- The `until` loop waits for the first MySQL initialization to complete. If it does not complete, check `docker logs Eco-mysql`.
- The `/var/run/docker.sock` mount is required by the admin button "立即升级并重启". Remove it if that feature is not used.

Logs: `docker logs -f Eco-hub`. To update, run `docker pull` for the new tag, run `docker rm -f Eco-hub`, and then run the same `docker run` command with the new tag.

## External MySQL and Redis

1. Remove the `mysql` and `redis` services and the `depends_on` block under `ecohub` from the compose file. For a manual deploy, do not start `Eco-mysql` and `Eco-redis`.
2. Point `MYSQL_*` and `REDIS_*` to the external instances.
3. The app runs in a container, where `127.0.0.1` refers to the container itself. For a database on the host, use `host.docker.internal`.

Example with the databases on the host:

```env
MYSQL_HOST=host.docker.internal
REDIS_HOST=host.docker.internal
```

The app creates its tables and initial data on startup. The database must be created in advance and can be empty.

## URLs

| URL | Purpose |
| --- | --- |
| `http://<server-ip>:3000` | Site |
| `http://<server-ip>:3000/manage` | Admin |
| `http://<server-ip>:3000/api/provide/app` | EcoHub client subscription |
| `http://<server-ip>:3000/api/provide/tvbox` | TVBox / YingShiCang subscription |
| `http://<server-ip>:3000/api/provide/vod` | MacCMS subscription |

`3000` is the default web port. The URLs printed in the terminal take precedence.

Subscription URLs can also use the direct API port, for example `http://<server-ip>:18080/api/provide/tvbox`. All three subscriptions are `/api/provide/*` and are handled by the Go backend. The web port only forwards `/api/*` to Go unchanged, so both ports return the same content. URLs in the response (TVBox site APIs, the client's `api_base`) are built from the request address, so a subscription via `18080` returns `18080` URLs. The site and admin pages are served only on the web port.

The admin account is `admin`. The password is the value of `ECOHUB_ADMIN_PASSWORD`, or `admin` if it is not set. Change the password immediately after the first sign-in.

When "私有化访问" (Private access) is enabled in the admin panel, subscription URLs must include the subscription key, for example `http://<server-ip>:3000/api/provide/tvbox?key=your-key`. See the [FAQ](./README-FAQ_EN.md#how-do-subscriptions-work-with-private-access-on).

Data locations (the install directory defaults to `~/ecohub`):

| Directory | Contents |
| --- | --- |
| `data/mysql` | MySQL data |
| `data/redis` | Redis data |
| `data/uploads` | Images uploaded in the admin panel, mounted at `/app/static/upload` in the container |

App logs are written to `docker logs` (`ecohub logs`) and to `/app/server/logs` inside the container. The in-container logs are not mounted and are lost when the container is recreated.

The site is empty at first. In the admin panel, open "采集中心" (Collect center), review the 12 preset collect sources, and run a full collect. Films appear as the collect progresses. The first collect can take several hours.

## Reverse proxy

Recommendations for production:

- Point the reverse proxy for the entire site to `127.0.0.1:3000` and enable HTTPS. `/api` does not need to be routed to `18080`; the web port forwards `/api/*` to the Go server in the container.
- To keep `3000` off the public internet and remove `18080`: for script-based installs, use [docker-compose.override.yml](#custom-compose-settings); for self-managed compose files, edit `ports` directly.
- Allow only `80` and `443` in the cloud security group.

## Environment variables

In the "Read by" column, Compose means the value is used only by the compose file, and App means the value is read by the Go process.

| Variable | Default | Read by | Notes |
| --- | --- | --- | --- |
| `ECOHUB_IMAGE` | `ghcr.io/fe-spark/ecohub:latest` (the install script writes `:v3.0.4` on a fresh install) | Compose | Image tag |
| `WEB_PORT` | `3000` | Compose | Host web port, mapped to `3000` in the container |
| `SERVER_PORT` | `18080` | Compose | Host direct API port, mapped to `8080` in the container |
| `JWT_SECRET` | Sample value from the repository | App | Login signing key. Must be a random string. Changing it signs out all users |
| `ECOHUB_ADMIN_PASSWORD` | `admin` | App | Written only when `admin` is first created |
| `MYSQL_ROOT_PASSWORD` | `ecohub` | Bundled MySQL | Applies only on the first init of `data/mysql` |
| `MYSQL_HOST` | `mysql` | App | Do not use `127.0.0.1` inside a container |
| `MYSQL_PORT` | `3306` | App | |
| `MYSQL_USER` | `eco` | App, bundled MySQL | |
| `MYSQL_PASSWORD` | `ecohub` | App, bundled MySQL | Bundled MySQL sets it only on first init |
| `MYSQL_DBNAME` | `eco` | App, bundled MySQL | |
| `REDIS_HOST` | `redis` | App | |
| `REDIS_PORT` | `6379` | App | |
| `REDIS_PASSWORD` | `ecohub` | App, bundled Redis | Edit `.env`, then `ecohub restart` |
| `REDIS_DB` | `0` | App | |

To route collect, TMDB scraping, Telegram notifications, the version check, or danmaku fetching through a proxy, open "网络代理" (Network proxy, `/manage/system/proxy`) in the admin panel, enter the proxy address (`http://`, `https://`, or `socks5://`), and enable it per module. The proxy can be configured only on this page; requests without an enabled proxy always connect directly. For the requests covered by each module and their defaults, see the [FAQ](./README-FAQ_EN.md#which-requests-use-the-admin-network-proxy).

Go-only variables such as `NODE_NAME` and `ECOHUB_LOG_DIR` are not passed into the container by the release compose file, so setting them in `.env` has no effect. Add them under `environment` in the compose file if required. For their meaning, see the [server README](../server/README_EN.md#environment-variables).

The release image sets the web app's `API_URL` to `http://127.0.0.1:8080` at build time. Do not set `API_URL` when deploying.

## Troubleshooting

- Health check: open `http://<server-ip>:3000/api/config/basic`. A JSON response indicates that both the web app and the API are running. If `18080` is mapped, `http://<server-ip>:18080/api/config/basic` checks the API directly.
- Container restarts repeatedly: check `ecohub logs` or `docker logs Eco-hub`. Common causes are a database password that does not match the value from the first initialization, or a port that is already in use. On startup the API waits for Redis and then MySQL, with 30 attempts each at 2-second intervals. If the connection still fails, the process exits and supervisord in the container restarts it.
- The site loads but API calls fail: verify that the reverse proxy routes the entire site to the web port.
- Port already in use: usually an old container still exists. Locate it with `docker ps -a` and remove it.

For behavior-related questions (for example, an empty site or site content not updating), see the [FAQ](./README-FAQ_EN.md).

## Related documents

[Overview](./README_EN.md) · [Upgrading from v2 to v3](./v2-to-v3_EN.md) · [FAQ](./README-FAQ_EN.md) · [Release notes](./RELEASE_EN.md)
