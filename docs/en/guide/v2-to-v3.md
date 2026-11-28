# Upgrading from v2 to v3

[中文](../../guide/v2-to-v3.md) | English

## Summary

v3 cannot read a v2 database, and no data migration is provided. The upgrade procedure is: remove the old containers, install v3 on an empty MySQL and Redis, and run a full collect.

For installation, passwords, ports, and reverse proxy, see the [Deploy guide](./deploy.md). For changes, see the [Release notes](../RELEASE.md).

## Precautions

The following operations are not supported:

- Running `docker compose pull && docker compose up -d` in a directory that already contains `./data/mysql`.
- Using "立即升级" (Upgrade now) in the v2 admin panel.
- Copying the old `data/mysql`, `data/redis`, or `data/uploads` into the new directory.

The v2 and v3 compose files use the same container names (`Eco-hub`, `Eco-mysql`, `Eco-redis`) and ports (`3000`, `18080`). Remove the old containers before installing v3:

```bash
docker rm -f Eco-hub Eco-mysql Eco-redis
```

This removes only the containers; data under `data/` in the v2 install directory is preserved. After v3 initializes, the database contains only the admin account, 12 preset collect sources, and default scheduled tasks.

## Method 1: install into a new directory (recommended)

This method keeps old data until the new deployment is confirmed working.

1. Remove the old containers as shown above.
2. Install into a new directory such as `~/ecohub-v3`:

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_INSTALL_DIR=~/ecohub-v3 ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.2.2 sh
```

To set passwords before initialization, also pass `JWT_SECRET`, `ECOHUB_ADMIN_PASSWORD`, `MYSQL_ROOT_PASSWORD`, `MYSQL_PASSWORD`, and `REDIS_PASSWORD` before `sh` (see [Set passwords on the first install](./deploy.md#set-passwords-on-the-first-install)). Keep `MYSQL_HOST=mysql` and `REDIS_HOST=redis` for bundled databases.

After installation, `ecohub` records the new directory, so `ecohub status` and `ecohub update` operate on `~/ecohub-v3`. For 1Panel, create a new compose project and working directory with [deploy/release/compose.yml](../../../deploy/release/compose.yml) instead of reusing `./data`.

## Method 2: clear and reinstall in the original directory

Use this method only when old data can be discarded. Replace `<old-install-dir>` with the v2 directory containing `docker-compose.yml`, `.env`, and `data/`.

1. Remove the old containers, then delete old data:

```bash
rm -rf <old-install-dir>/data/mysql <old-install-dir>/data/redis <old-install-dir>/data/uploads
```

2. Reinstall. The script keeps `.env` and updates only variables specified before `sh`; verify existing values first and pass the v3 image tag:

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_INSTALL_DIR=<old-install-dir> ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.2.2 sh
```

## Method 3: external database

Do not point v3 to a database in use by v2. Create a new empty database or drop the old one before startup. Do not use `127.0.0.1` inside containers; use `host.docker.internal` for a host database.

## Verification

The upgrade is complete when:

- `ecohub status` shows all three containers running.
- The site URL opens and `/api/config/basic` returns JSON.
- Signing in to the admin panel and running a full collect populates films on the site.

An existing reverse proxy targeting `127.0.0.1:3000` needs no changes. Port or name conflict errors mean old containers still exist.

## Related documents

[Deploy guide](./deploy.md) · [Operations and configuration](./operations.md) · [FAQ](./faq.md) · [Release notes](../RELEASE.md)
