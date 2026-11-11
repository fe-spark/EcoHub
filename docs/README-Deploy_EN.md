# EcoHub Deploy Guide

[中文](./README-Deploy.md) | English

EcoHub ships one all-in-one image, `ghcr.io/fe-spark/ecohub`. One container runs the Go API (port `8080` inside the container) and the Next.js web app (port `3000` inside the container). Add one MySQL and one Redis and it works.

Which image tag to use:

| Tag | Points to | When to use |
| --- | --- | --- |
| `:v3.0.2` and the like | A fixed release | Recommended. Change the tag yourself to upgrade |
| `:latest` | The newest release, updated on every release | You always want the newest version |
| Tags ending in `-beta` | Test builds | Not for production. Deleted automatically after the next release |

Files used on this page: [compose file deploy/release/compose.yml](../deploy/release/compose.yml) · [variable template .env.example](../.env.example) · [release notes](./RELEASE.md) (Chinese). Coming from v2? Read [v2 to v3](./v2-to-v3.md) (Chinese) first.

## Prerequisites

- Docker 20+ and Docker Compose 2+ (the manual `docker run` method does not need Compose).
- At least 2 CPU cores and 2 GB RAM.
- The server can pull from `ghcr.io` (app image) and `docker.io` (MySQL and Redis images).

Two public ports:

| Port | Default | Purpose |
| --- | --- | --- |
| Web port | `3000` | Site, admin, and subscription URLs all use this port |
| Direct API port | `18080` | Maps to `8080` in the container, for players that call the backend directly. Remove it if you do not need it |

Prepare a random key for `JWT_SECRET`:

```bash
openssl rand -hex 32
```

## Method 1: install script (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2 sh
```

What the script does:

1. Creates `data/mysql`, `data/redis`, and `data/uploads` in the install directory (default `~/ecohub`).
2. Downloads the compose file as `docker-compose.yml` and downloads `.env.example`. If there is no `.env`, it creates one from the template. An existing `.env` is kept.
3. Writes the variables placed before `sh` into `.env` (see below). On a fresh install with no image given, it writes `ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2`.
4. Installs the `ecohub` command into `/usr/local/bin`. If that directory is not writable, it uses `~/.local/bin` and tells you to add it to `PATH`.
5. Remembers the install directory (in `~/.ecohub/install-dir`). Later `ecohub` commands work on that directory.
6. Starts the containers and prints the site, admin, initial account, and three subscription URLs.

### Set passwords on the first install

Put the variables before `sh`. Variables placed before `curl` do not reach the script. For example:

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2 \
    JWT_SECRET="$(openssl rand -hex 32)" \
    ECOHUB_ADMIN_PASSWORD='use-a-strong-password' \
    MYSQL_ROOT_PASSWORD='use-a-strong-password' \
    MYSQL_PASSWORD='use-a-strong-password' \
    REDIS_PASSWORD='use-a-strong-password' \
    sh
```

`install` and `update` write only these 8 variables into `.env`: `ECOHUB_IMAGE`, `WEB_PORT`, `SERVER_PORT`, `JWT_SECRET`, `ECOHUB_ADMIN_PASSWORD`, `MYSQL_ROOT_PASSWORD`, `MYSQL_PASSWORD`, `REDIS_PASSWORD`. Variables you do not pass are left alone, and other lines in `.env` are kept.

If you pass no passwords, `.env` uses the public defaults from the template (`JWT_SECRET` is a sample value checked into the repository, the database passwords are `ecohub`, and the admin password is `admin`). Change them before exposing the server to the internet.

To use another install directory, also put `ECOHUB_INSTALL_DIR=~/ecohub-v3` before `sh`.

### When password and port changes take effect

| Variable | Edit `.env`, then `ecohub restart` | Notes |
| --- | --- | --- |
| `WEB_PORT`, `SERVER_PORT` | Takes effect | |
| `REDIS_PASSWORD` | Takes effect | Redis sets its password from this value on every start |
| `JWT_SECRET` | Takes effect | Everyone who is logged in has to log in again |
| `ECOHUB_ADMIN_PASSWORD` | No effect | Written only when the `admin` account is first created. Change the password in admin afterwards |
| `MYSQL_PASSWORD`, `MYSQL_ROOT_PASSWORD` | No effect, and breaks things | MySQL sets its passwords only when `data/mysql` is first initialized |

Example: after install you change `MYSQL_PASSWORD` in `.env` from `ecohub` to `abc` and run `ecohub restart`. The app connects with `abc`, but MySQL still stores `ecohub`. The app cannot connect and the container keeps restarting.

`ecohub start` and `ecohub restart` only read `.env`. They do not copy variables from your current shell into `.env`.

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

| Command | What it does |
| --- | --- |
| `ecohub install` | Downloads the compose file, starts, and prints URLs. Running plain `ecohub` is the same |
| `ecohub status` | Prints URLs and container status |
| `ecohub logs` | Shows the last 100 log lines. `logs -f` follows, `logs ecohub` shows only the app |
| `ecohub start` | Starts with the current `.env` |
| `ecohub stop` | Stops containers and keeps data |
| `ecohub restart` | Recreates containers so `.env` changes apply |
| `ecohub update` | Downloads the compose file again, pulls the image in `ECOHUB_IMAGE` from `.env`, and recreates |
| `ecohub uninstall` | Removes containers. Keeps `data/`, `.env`, and the `ecohub` command |
| `ecohub uninstall --purge` | Also deletes `data/mysql`, `data/redis`, and `data/uploads`. `.env` and the `ecohub` command stay |
| `ecohub help` | Shows usage |

To upgrade: change `ECOHUB_IMAGE` in `.env` to the new tag and run `ecohub update`, or run `ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:<new-version> ecohub update`.

Note: `ecohub update` downloads `docker-compose.yml` again every time, so your manual edits to the compose file are overwritten. `.env` is not touched.

The admin button "立即升级并重启" (upgrade now and restart) pulls `:latest`. It ignores the tag pinned in `.env`.

These containers show up in the 1Panel container list. Keep managing them with `ecohub`. Do not create a second compose project with the same container names.

## Method 2: 1Panel

1. **Containers** → **Compose** → **Create**. Working directory, for example `/opt/1panel/apps/ecohub`.
2. Paste the full [deploy/release/compose.yml](../deploy/release/compose.yml).
3. Fill in the environment:

```env
ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2
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

4. Save and start. Check that `Eco-hub`, `Eco-mysql`, and `Eco-redis` are running.
5. **Websites** → **Reverse proxy**, pointing to `http://127.0.0.1:3000`.

To update: set the new `ECOHUB_IMAGE`, then run this in the working directory:

```bash
docker compose pull && docker compose up -d
```

## Method 3: manual docker run

No Compose. Replace the passwords below with strong random values first.

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
  ghcr.io/fe-spark/ecohub:v3.0.2
```

- Inside the app container, the database hosts are the container names `Eco-mysql` and `Eco-redis`.
- The `until` loop waits for MySQL's first initialization, which can take tens of seconds. If it never finishes, read `docker logs Eco-mysql`.
- The `/var/run/docker.sock` mount is for the admin button "立即升级并重启". Remove it if you do not use that button.

Logs: `docker logs -f Eco-hub`. To update: `docker pull` the new tag, `docker rm -f Eco-hub`, then run the same `docker run` with the new tag.

## Bring your own MySQL and Redis

1. Remove the `mysql` and `redis` services and the `depends_on` block under `ecohub` from the compose file. For a manual deploy, do not start `Eco-mysql` and `Eco-redis`.
2. Point `MYSQL_*` and `REDIS_*` at your instances.
3. The app runs in a container, so `127.0.0.1` means the container itself. For a database on the host, use `host.docker.internal`.

Example with the databases on the host:

```env
MYSQL_HOST=host.docker.internal
REDIS_HOST=host.docker.internal
```

The app creates its tables and initial data on start. Create the database beforehand. It can be empty.

## URLs

| URL | Purpose |
| --- | --- |
| `http://<server-ip>:3000` | Site |
| `http://<server-ip>:3000/manage` | Admin |
| `http://<server-ip>:3000/api/provide/app` | EcoHub client subscription |
| `http://<server-ip>:3000/api/provide/tvbox` | TVBox / YingShiCang subscription |
| `http://<server-ip>:3000/api/provide/vod` | MacCMS subscription |

`3000` is the default web port. Use the URLs the terminal prints. The admin account is `admin`. The password is `ECOHUB_ADMIN_PASSWORD`, or `admin` when unset. Change it right after you log in.

When "私有化访问" (private access) is on in admin, subscription URLs need the subscription key, for example `http://<server-ip>:3000/api/provide/tvbox?key=your-key`. See the [FAQ](./README-FAQ_EN.md#how-do-subscriptions-work-with-private-access-on).

Data locations (install directory defaults to `~/ecohub`):

| Directory | Contents |
| --- | --- |
| `data/mysql` | MySQL data |
| `data/redis` | Redis data |
| `data/uploads` | Images uploaded in admin, mounted at `/app/static/upload` in the container |

App logs go to `docker logs` (`ecohub logs`) and to `/app/server/logs` inside the container. The in-container copy is not mounted, so recreating the container loses it.

The site is empty at first. In admin, open "采集中心" (Collect center), check the 12 preset collect sources, and run one full collect. Films appear while the collect runs. The first run can take several hours.

## Reverse proxy

For production:

- Point the reverse proxy for the whole site to `127.0.0.1:3000` and enable HTTPS. You do not need to send `/api` to `18080`. The web port already forwards `/api/*` to the Go server in the container.
- To keep `3000` off the public internet, change the port line in the compose file to `127.0.0.1:${WEB_PORT:-3000}:3000`. If players do not connect directly, delete the `18080` line. With the install script, remember that `ecohub update` overwrites manual compose edits, so redo them after each update.
- Open only `80` and `443` in the cloud security group.

## Environment variables

"Read by": Compose means the value is only used by the compose file. App means the Go process reads it.

| Variable | Default | Read by | Notes |
| --- | --- | --- | --- |
| `ECOHUB_IMAGE` | `ghcr.io/fe-spark/ecohub:latest` (the install script writes `:v3.0.2` on a fresh install) | Compose | Image tag |
| `WEB_PORT` | `3000` | Compose | Host web port, mapped to `3000` in the container |
| `SERVER_PORT` | `18080` | Compose | Host direct API port, mapped to `8080` in the container |
| `JWT_SECRET` | Sample value from the repository | App | Login signing key. Must be a random string. Changing it logs everyone out |
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

To send collect, TMDB scraping, Telegram notifications, or the version check through a proxy, open admin → "网络代理" (Network proxy, `/manage/system/proxy`), enter the proxy address (`http://`, `https://`, or `socks5://`), then turn it on per module. This is the only place to configure a proxy.

Go-only variables such as `NODE_NAME` and `ECOHUB_LOG_DIR` are not passed into the container by the release compose file, so putting them in `.env` has no effect. Add them under `environment` in the compose file if you need them. Their meaning is in the [server README](../server/README.md#环境变量) (Chinese).

The release image fixes the web app's `API_URL` to `http://127.0.0.1:8080` at build time. Do not set `API_URL` when deploying.

## Troubleshooting

- Health check: open `http://<server-ip>:3000/api/config/basic`. A JSON response means both the web app and the API are up. If `18080` is mapped, `http://<server-ip>:18080/api/config/basic` checks the API directly.
- Container keeps restarting: read `ecohub logs` or `docker logs Eco-hub` first. Common causes are a database password that no longer matches the first init, or a port already in use. On start the API waits for Redis and then MySQL, 30 tries each, 2 seconds apart. If it still cannot connect, it exits and supervisord in the container starts it again.
- Site loads but API calls fail: check that the reverse proxy sends the whole site to the web port.
- Port already in use: usually an old container is still there. Find it with `docker ps -a` and remove it.

Behavior questions (why the site is empty, why the site did not change, and so on): [FAQ](./README-FAQ_EN.md).

## Related

[Overview](./README_EN.md) · [v2 to v3](./v2-to-v3.md) (Chinese) · [FAQ](./README-FAQ_EN.md) · [Release notes](./RELEASE.md) (Chinese)
