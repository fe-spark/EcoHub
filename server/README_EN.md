# Server

[中文](./README.md) | English

`server/` contains the EcoHub Go backend, which handles collect, the film library, lists and caches, subscription APIs, and admin authentication.

For internal behavior (streaming publish, matching, scraping, caching), see [Core mechanics and data flow](../docs/en/dev/architecture.md). For scheduled task ordering, see [Scheduled task conflicts](../docs/en/dev/cron-task-conflict.md). For Docker deployment variables, see [Operations and configuration](../docs/en/guide/operations.md#environment-variables).

## Run locally

Requirements: Go 1.27+, MySQL 8+, and Redis 7+.

If no database is available, start only the database containers with the development compose file (see the [Development guide](../docs/en/dev/setup.md#development-compose-file)):

```bash
docker compose up -d mysql redis
```

Then run:

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

- The process reads `.env` from the current directory on startup and must therefore be run inside `server/`. Existing environment variables are not overridden by `.env`.
- The API listens on `:8080` by default.
- The first startup creates the tables and writes the site config, the admin account `admin`, the 12 preset collect sources, and 4 scheduled tasks.
- For a public deployment, replace `JWT_SECRET` with a random string, which can be generated with `openssl rand -hex 32`.

### Run the server in a container

The `server` service in the development compose file is built from source with `server/Dockerfile`. It contains only the Go backend, and the container is named `Eco-server`:

```bash
docker compose up -d --build server
```

- Run the command in the repository root. `server` declares no `depends_on` and does not start the databases; run `docker compose up -d mysql redis` first.
- The API is mapped to `18080` on the host (`SERVER_PORT`), for example `http://127.0.0.1:18080/api/config/basic`.
- Inside the container, the databases are reached by the service names `mysql` and `redis`. Variables are taken from the `.env` in the repository root (or the compose defaults if it is absent); `server/.env` is not read.

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
| `NODE_NAME` | host name | No | Service node name recorded in analytics. When the host name is longer than 12 characters, the last 6 characters are used. Used to distinguish instances in multi-instance deployments |
| `ECOHUB_TRUSTED_PROXIES` | `127.0.0.1,::1` | No | Comma-separated trusted proxy IPs or CIDR ranges; `X-Forwarded-*` headers are honored only when matched |
| `GIN_MODE` | `release` | No | Runs in release mode when unset |

The process panics when a required value is missing: any of `MYSQL_HOST`, `MYSQL_PORT`, `MYSQL_USER`, `MYSQL_DBNAME` is empty, any of `REDIS_HOST`, `REDIS_PORT` is empty, or `JWT_SECRET` is not set.

`WEB_PORT`, `SERVER_PORT`, `MYSQL_ROOT_PASSWORD`, and `ECOHUB_IMAGE` are used only by Compose and are not read by the Go process.

## Startup sequence

1. Check upgrade helper mode and initialize runtime logging; connect to Redis, then MySQL, with 30 attempts each at 2-second intervals, exiting on failure.
2. Wire cross-module callbacks (including recommendation chart auto-sync and category/search cache cleanup after collect index resealing), then start Redis and MySQL health checks.
3. Under a MySQL advisory lock, run pending schema migrations in `internal/migration/` in order, run `AutoMigrate`, and align indexes; on a new database or when no admin exists, seed the 12 preset collect sources.
4. Initialize the admin account `admin`, upload directories, taxonomy rules and main categories, site config, default poster sources, and analytics config.
5. Mark interrupted collect records, clean up expired records (older than 30 days) and missing source categories in the background, register the 4 scheduled tasks, start the Telegram bot poller, release poller, and analytics collector, bootstrap the active read model, and wait for the in-memory search index build.
6. Apply trusted proxy and request policy middleware, start listening, warm the main source's home page cache in the background, and shut down gracefully on exit signals by stopping collect and flushing buffered analytics first.

If `admin` already exists, its password is not changed. Other accounts are created under "账号管理" (Accounts) in the admin panel.

## APIs

Site APIs (require a login or a subscription key when "私有化访问" (Private access) is enabled in the admin panel):

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

Subscription APIs (when private access is enabled, the subscription key is required as `?key=` or the `X-Provide-Key` header):

| Method | Path | Use |
| --- | --- | --- |
| GET | `/api/provide/app` | EcoHub client |
| GET | `/api/provide/tvbox` | TVBox / YingShiCang |
| GET | `/api/provide/vod` | MacCMS |

APIs not limited by private access:

| Method | Path | Use |
| --- | --- | --- |
| GET | `/api/config/basic` | Basic site config; also used as the health check |
| POST | `/api/login` | Sign in |
| POST | `/api/logout` | Sign out; requires a signed-in session |
| GET | `/api/upload/pic/poster/*name` | Uploaded images |

All admin APIs are under `/api/manage/*`. For the full list, see `internal/router/router.go`.

### Authentication

- After a successful sign-in, the backend sets the HttpOnly cookie `ecohub_auth_token`, valid for 10 days. The `Authorization: Bearer <token>` header is also accepted.
- `/api/manage/*` and `/api/logout` check both the JWT and the current token stored in Redis. When the same account signs in elsewhere, the old token becomes invalid. After the JWT expires, the user must sign in again.
- Visitor accounts can send only GET requests. `WriteAccess` rejects write operations with `403`.
- Some operations (site config, account management, upgrade, etc.) are restricted to the super user and enforced by `AdminAccess`.

## Directory layout

```text
server/
├── cmd/server/                 # Entry point main.go (wiring, health checks, graceful shutdown)
├── internal/access/            # Access statistics (analytics)
├── internal/admin/             # Admin overview and upgrade (`upgrade/` is the upgrade helper)
├── internal/backup/            # Data backup and restore
├── internal/banner/            # Homepage banners
├── internal/bootstrap/         # Startup initialization and cross-module callback wiring
├── internal/collect/           # Collect scheduling and engine (`engine/`, `ledger/`, `progress/`)
├── internal/collectctl/        # Admin APIs for collect sources, records, and scheduled tasks
├── internal/config/            # Environment variables, constants, trusted proxies, concurrency profiles
├── internal/danmaku/           # Danmaku read, post, and network sniffing
├── internal/file/              # Local uploads and gallery management
├── internal/film/              # Film core (`cache/`, `catalog/`, `converter/`, `query/`, `shared/`, `snapshot/`, `writer/`)
├── internal/hotchart/          # Douban / Bangumi chart fetching and caching
├── internal/httpx/             # HTTP client and proxy transport helpers
├── internal/infra/             # db (MySQL and Redis connections), syslog (run logs)
├── internal/mapping/           # Cross-source film match keys
├── internal/mediaproxy/        # Media playback and image proxy settings
├── internal/middleware/        # Auth, private access, access log, CORS
├── internal/migration/         # Versioned schema migrations (MySQL advisory lock; append-only)
├── internal/model/             # Table schemas and DTO models (`dto/`)
├── internal/notify/            # Telegram notifications and callback menu polling
├── internal/portal/            # Public site and TVBox / MacCMS / App subscription APIs
├── internal/proxy/             # Admin outbound network proxy configuration
├── internal/recommend/         # Homepage recommendation groups and chart auto-sync
├── internal/repobase/          # Shared persistence, config access, and migration runner entry
├── internal/repository/film/   # Static asset migration helpers (`static/`)
├── internal/router/            # Routes, request policy, and maintenance guard middleware
├── internal/siteops/           # Site config and data reset operations
├── internal/sourcecatalog/     # Collect source catalog, main source, and source stats cache
├── internal/taxonomy/          # Category recognition and mapping rule engine
├── internal/testutil/          # Test helpers
├── internal/tmdb/              # TMDB scraping and config management
├── internal/user/              # Sign-in, accounts, and permissions
├── internal/utils/             # Shared utilities
└── test/integration/           # Cross-module integration tests
```

Unit tests are located in each package under `internal/`. Cross-module tests are located in `test/integration/`.

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

## Related documents

[Overview](../docs/en/README.md) · [Core mechanics](../docs/en/dev/architecture.md) · [Web](../web/README_EN.md) · [Deploy guide](../docs/en/guide/deploy.md) · [Operations](../docs/en/guide/operations.md)
