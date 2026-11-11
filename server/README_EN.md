# Server

[中文](./README.md) | English

`server/` is the EcoHub Go backend: collect, the film library, lists and caches, subscription APIs, and admin authentication all live here.

Behavior (main source, matching, scraping, caching): [FAQ](../docs/README-FAQ_EN.md). Order of scheduled tasks: [Scheduled task conflicts](../docs/cron-task-conflict_EN.md). Variables for Docker deployment: [Deploy guide](../docs/README-Deploy_EN.md#environment-variables).

## Run locally

You need Go 1.27+, MySQL 8+, and Redis 7+.

If you have no database at hand, use the dev compose file `docker-compose.yml` in the repository root to start only the database containers (this file is for local development only; how it differs from the release file: [Overview](../docs/README_EN.md#local-development)). They are mapped to `127.0.0.1:3306` and `127.0.0.1:6379` on the host, with the same credentials as `server/.env.example`:

```bash
docker compose up -d mysql redis
```

Then:

```bash
cd server
cp .env.example .env
go run ./cmd/server
```

Contents of `.env.example`:

```env
PORT=8080
JWT_SECRET=ecohub_2026!local@dev_secret$001
ECOHUB_ADMIN_PASSWORD=admin
MYSQL_HOST=127.0.0.1
MYSQL_PORT=3306
MYSQL_USER=eco
MYSQL_PASSWORD=ecohub
MYSQL_DBNAME=eco
REDIS_HOST=127.0.0.1
REDIS_PORT=6379
REDIS_PASSWORD=ecohub
REDIS_DB=0
```

- The process reads `.env` from the current directory on start, so run it inside `server/`. Environment variables that already exist are not overridden by `.env`.
- The API listens on `:8080` by default.
- The first start creates the tables and writes the site config, the admin account `admin`, the 12 preset collect sources, and 4 scheduled tasks.
- For a public deployment, replace `JWT_SECRET` with a random string, for example from `openssl rand -hex 32`.

### Run the server in a container

The `server` service in the dev compose file is built from source with `server/Dockerfile`. It contains only the Go backend, and the container is named `Eco-server`:

```bash
docker compose up -d --build server
```

- Run it in the repository root. `server` declares no `depends_on`, so it does not start the databases. Run `docker compose up -d mysql redis` first.
- The API is mapped to `18080` on the host (`SERVER_PORT`), for example `http://127.0.0.1:18080/api/config/basic`.
- In the container it reaches the databases by the service names `mysql` and `redis`. Variables come from the `.env` in the repository root (or the compose defaults when there is none). It does not read `server/.env`.

## Environment variables

| Variable | Default | Required | Notes |
| --- | --- | --- | --- |
| `PORT` | `8080` | No | API listen port. Fixed to `8080` in the release image |
| `JWT_SECRET` | none | Yes | Login signing key. Panics when unset |
| `ECOHUB_ADMIN_PASSWORD` | `admin` | No | Written only when `admin` is first created |
| `MYSQL_HOST` | none | Yes | `127.0.0.1` on the host, the service name `mysql` in Compose, `host.docker.internal` to reach the host from a container |
| `MYSQL_PORT` | none | Yes | Usually `3306` |
| `MYSQL_USER` | none | Yes | |
| `MYSQL_PASSWORD` | empty | No | |
| `MYSQL_DBNAME` | none | Yes | |
| `REDIS_HOST` | none | Yes | `127.0.0.1` on the host, the service name `redis` in Compose |
| `REDIS_PORT` | none | Yes | Usually `6379` |
| `REDIS_PASSWORD` | empty | No | |
| `REDIS_DB` | `0` | No | Ignored when not a number; `0` is used |
| `ECOHUB_LOG_DIR` | `logs/` under the module root | No | Run log directory. When no `go.mod` is found going up, `logs/` under the current directory is used |
| `NODE_NAME` | host name | No | Service node name recorded in analytics. When the host name is longer than 12 characters, the last 6 characters are used. Use it to tell instances apart |
| `GIN_MODE` | `release` | No | Runs in release mode when unset |

The process panics when a required value is empty: any of `MYSQL_HOST`, `MYSQL_PORT`, `MYSQL_USER`, `MYSQL_DBNAME`, or any of `REDIS_HOST`, `REDIS_PORT`, or no `JWT_SECRET`.

`WEB_PORT`, `SERVER_PORT`, `MYSQL_ROOT_PASSWORD`, and `ECOHUB_IMAGE` are for Compose only. The Go process does not read them.

## What happens on start

1. Connect to Redis, then MySQL. 30 tries each, 2 seconds apart. If it still cannot connect, it panics and exits.
2. Create and update table schemas (`AutoMigrate`), then run the migrations in `internal/migration/` that have not run yet, in order.
3. On a new database, or when there is no admin, write the 12 preset collect sources.
4. Initialize categories, the admin account `admin`, the upload directory, site config, default poster sources, and analytics config.
5. Register scheduled tasks, load snapshots and in-memory list data, and wait for the search index to be built.
6. Start listening, and warm the main source's home page cache in the background.

If `admin` already exists, its password is not changed. Create other accounts in admin under "账号管理" (Accounts).

## APIs

Site APIs (need a login or subscription key when "私有化访问" (private access) is on in admin):

| Method | Path |
| --- | --- |
| GET | `/api/home` |
| GET | `/api/daily-updates` |
| GET | `/api/categories` |
| GET | `/api/categories/overview` |
| GET | `/api/films/play-info` |
| GET | `/api/films/related` |
| GET | `/api/films/search` |
| GET | `/api/films/filter` |
| GET | `/api/search/hot-keywords` |
| POST | `/api/stats/view` |
| GET, POST | `/api/film/danmaku` |

Subscription APIs (with private access on, they need the subscription key as `?key=` or the `X-Provide-Key` header):

| Method | Path | Use |
| --- | --- | --- |
| GET | `/api/provide/app` | EcoHub client |
| GET | `/api/provide/tvbox` | TVBox / YingShiCang |
| GET | `/api/provide/vod` | MacCMS |

APIs not limited by private access:

| Method | Path | Use |
| --- | --- | --- |
| GET | `/api/config/basic` | Basic site config; also used as the health check |
| POST | `/api/login` | Log in |
| POST | `/api/logout` | Log out; needs a login |
| GET | `/api/upload/pic/poster/*name` | Uploaded images |

All admin APIs are under `/api/manage/*`. For the full list see `internal/router/router.go`.

### Authentication

- After login, the backend sets the HttpOnly cookie `ecohub_auth_token`, valid for 10 days. The `Authorization: Bearer <token>` header is also accepted.
- `/api/manage/*` and `/api/logout` check both the JWT and the current token stored in Redis. When the same account logs in elsewhere, the old token stops working. After the JWT expires, log in again.
- Visitor accounts can only send GET. `WriteAccess` rejects writes with `403`.
- Some actions (site config, account management, upgrade, and so on) are for the super user only, enforced by `AdminAccess`.

## Directory layout

```text
server/
├── cmd/server/                 # Entry point main.go
├── internal/access/            # Access statistics (analytics)
├── internal/config/            # Environment variables, constants, collect concurrency levels
├── internal/handler/           # HTTP handlers
├── internal/infra/             # db (MySQL and Redis connections), syslog (run logs)
├── internal/middleware/        # Auth, private access, access log, CORS
├── internal/migration/         # Schema migrations: one empty baseline, new changes only appended at the end
├── internal/model/             # Table schemas and data models
├── internal/repository/        # Data access; film/ has cache, query, shared, snapshot, writer
├── internal/router/            # Routes
├── internal/service/           # Business logic; upgrade/ is the admin "立即升级并重启" (upgrade now and restart)
├── internal/spider/            # Collect; subdirectories converter, fetcher, progress, scheduler
├── internal/testutil/          # Test helpers
├── internal/utils/             # Shared utilities
└── test/integration/           # Cross-module integration tests
```

Unit tests live in each package under `internal/`. Cross-module tests go in `test/integration/`.

## Commands

```bash
cd server
go run ./cmd/server
go build ./...
go test ./internal/...
go test ./test/...
go test ./...
```

If the local Go cache directory is not writable:

```bash
GOCACHE=/tmp/ecohub-go-cache go test ./...
```

## Related

[Overview](../docs/README_EN.md) · [Web](../web/README_EN.md) · [Deploy guide](../docs/README-Deploy_EN.md) · [FAQ](../docs/README-FAQ_EN.md)
