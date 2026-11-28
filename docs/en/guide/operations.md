# Operations and Configuration

[中文](../../guide/operations.md) | English

This guide covers daily `ecohub` commands, when password and port edits take effect, compose overrides, external databases, the complete environment variable reference, and troubleshooting.

## The ecohub command

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
| `ecohub install` | Downloads the compose file, starts services, and prints URLs; running `ecohub` alone is equivalent |
| `ecohub status` | Prints access URLs and container statuses |
| `ecohub logs` | Shows the last 100 log lines; `logs -f` follows output, `logs ecohub` shows app logs only |
| `ecohub start` | Starts containers using the current `.env` |
| `ecohub stop` | Stops containers and preserves data |
| `ecohub restart` | Recreates containers to apply `.env` changes |
| `ecohub update` | Re-downloads the compose file, pulls `ECOHUB_IMAGE` from `.env`, and recreates containers |
| `ecohub uninstall` | Removes containers while preserving `data/`, `.env`, and `ecohub` |
| `ecohub uninstall --purge` | Also deletes `data/mysql`, `data/redis`, and `data/uploads`, while keeping `.env` and `ecohub` |
| `ecohub help` | Shows usage |

To upgrade, update `ECOHUB_IMAGE` in `.env` and run `ecohub update`, or run `ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:<new-version> ecohub update`.

- `ecohub update` overwrites `docker-compose.yml` on every run; put custom compose edits in [Custom compose settings](#custom-compose-settings).
- The admin button "立即升级并重启" (Upgrade now and restart) pulls `:latest` rather than the tag pinned in `.env`.

## When password and port changes take effect

`ecohub start` and `ecohub restart` read only `.env` and do not capture temporary shell variables.

| Variable | Edit `.env`, then `ecohub restart` | Notes |
| --- | --- | --- |
| `WEB_PORT`, `SERVER_PORT` | Takes effect | |
| `REDIS_PASSWORD` | Takes effect | Redis sets its password from this value on every start |
| `JWT_SECRET` | Takes effect | All signed-in users must sign in again |
| `ECOHUB_ADMIN_PASSWORD` | No effect | Written only when `admin` is first created; change it in the admin panel afterwards |
| `MYSQL_PASSWORD`, `MYSQL_ROOT_PASSWORD` | No effect, and causes connection failures | MySQL sets passwords only on the first initialization of `data/mysql` |

Example: changing `MYSQL_PASSWORD` in `.env` from `ecohub` to `abc` after initialization and running `ecohub restart` causes the app to connect with `abc` while MySQL still stores `ecohub`, leading to repeated restarts.

## Custom compose settings

Create `docker-compose.override.yml` in the install directory (default `~/ecohub`) with only the fields to override. `ecohub install` and `ecohub update` never create, overwrite, or delete this file; when present, every `ecohub` command applies it, and `ecohub status` reports "已叠加 docker-compose.override.yml".

Example: bind the web port to localhost only (behind a reverse proxy) and remove the direct API port:

```yaml
services:
  ecohub:
    ports: !override
      - "127.0.0.1:${WEB_PORT:-3000}:3000"
```

Run `ecohub restart` afterwards:

- `!override` replaces the original `ports` list rather than merging with it.
- `!override` requires Docker Compose v2.24.4 or later; standalone `docker-compose` v1 does not support it.
- To revert, delete the override file and run `ecohub restart`.

## External MySQL and Redis

1. Remove the `mysql` and `redis` services and `depends_on` under `ecohub` from the compose file; for manual installs, do not launch `Eco-mysql` and `Eco-redis`.
2. Point `MYSQL_*` and `REDIS_*` to the external instances.
3. Inside a container, `127.0.0.1` refers to the container itself; use `host.docker.internal` for databases running on the host.

Example with host databases:

```env
MYSQL_HOST=host.docker.internal
REDIS_HOST=host.docker.internal
```

The app creates tables and initial data automatically on startup. The database must already exist and can be empty.

## Data directories and logs

Under the install directory (default `~/ecohub`):

| Directory | Contents |
| --- | --- |
| `data/mysql` | MySQL data |
| `data/redis` | Redis data |
| `data/uploads` | Admin uploaded images, mounted at `/app/static/upload` |

App logs go to both `docker logs` (`ecohub logs`) and `/app/server/logs` inside the container. In-container log files are not mounted to the host and are lost when the container is recreated.

## Environment variables

In the "Read by" column, Compose means used only by compose orchestration, and App means read by the Go process.

| Variable | Default | Read by | Notes |
| --- | --- | --- | --- |
| `ECOHUB_IMAGE` | `ghcr.io/fe-spark/ecohub:latest` (fresh script installs write the default v3 tag) | Compose | Image tag |
| `WEB_PORT` | `3000` | Compose | Host web port mapped to `3000` in the container |
| `SERVER_PORT` | `18080` | Compose | Host direct API port mapped to `8080` in the container |
| `JWT_SECRET` | Sample value from repo | App | Signing key; must be random; changing it signs out all users |
| `ECOHUB_ADMIN_PASSWORD` | `admin` | App | Written only when `admin` is first created |
| `MYSQL_ROOT_PASSWORD` | `ecohub` | Bundled MySQL | Applies only on the first init of `data/mysql` |
| `MYSQL_HOST` | `mysql` | App | Do not use `127.0.0.1` inside a container |
| `MYSQL_PORT` | `3306` | App | |
| `MYSQL_USER` | `eco` | App, bundled MySQL | |
| `MYSQL_PASSWORD` | `ecohub` | App, bundled MySQL | Bundled MySQL sets it only on first init |
| `MYSQL_DBNAME` | `eco` | App, bundled MySQL | |
| `MYSQL_INNODB_BUFFER_POOL_SIZE` | `1G` | Bundled MySQL | InnoDB buffer pool size |
| `MYSQL_INNODB_REDO_LOG_CAPACITY` | `512M` | Bundled MySQL | InnoDB redo log capacity |
| `REDIS_HOST` | `redis` | App | |
| `REDIS_PORT` | `6379` | App | |
| `REDIS_PASSWORD` | `ecohub` | App, bundled Redis | Edit `.env`, then `ecohub restart` |
| `REDIS_DB` | `0` | App | |

- Outbound proxies are configured solely in "网络代理" (Network proxy, `/manage/system/proxy`) in the admin panel (`http://`, `https://`, `socks5://`, or `socks5h://`); container environment variables do not affect outbound requests. See the [FAQ](./faq.md#which-requests-use-the-admin-network-proxy).
- Go-only variables such as `NODE_NAME`, `ECOHUB_LOG_DIR`, and `ECOHUB_TRUSTED_PROXIES` are not passed into the container by the default release compose file; add them under `environment` if needed. See the [server README](../../../server/README_EN.md#environment-variables).
- The release image fixes `API_URL` to `http://127.0.0.1:8080` at build time; do not set `API_URL` when deploying.

## Troubleshooting

- **Health check**: open `http://<server-ip>:3000/api/config/basic`; a JSON response confirms both web and API are up (or check `18080` directly if exposed).
- **Repeated container restarts**: check `ecohub logs` or `docker logs Eco-hub`. Typical causes are mismatched database passwords or port conflicts. On startup the API retries Redis and then MySQL 30 times at 2-second intervals before exiting and being restarted by supervisord.
- **Site opens but API fails**: verify the reverse proxy forwards the entire site to the web port.
- **Port in use**: usually caused by leftover containers; locate with `docker ps -a` and remove them.

For behavioral questions, see the [FAQ](./faq.md).

## Related documents

[Deploy guide](./deploy.md) · [Upgrading from v2 to v3](./v2-to-v3.md) · [FAQ](./faq.md) · [Release notes](../RELEASE.md)
