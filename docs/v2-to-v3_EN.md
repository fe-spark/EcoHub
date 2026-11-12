# Upgrading from v2 to v3

[中文](./v2-to-v3.md) | English

## Summary

v3 cannot read a v2 database, and no data migration is provided. The upgrade procedure is: remove the old containers, install v3 on an empty MySQL and Redis, and run a full collect.

For installation, passwords, ports, and reverse proxy, see the [Deploy guide](./README-Deploy_EN.md). For changes, see the [Release notes](./RELEASE_EN.md).

## Precautions

The following operations are not supported:

- Running `docker compose pull && docker compose up -d` in a directory that already contains `./data/mysql`.
- Using "立即升级" (Upgrade now) in the v2 admin panel.
- Copying the old `data/mysql`, `data/redis`, or `data/uploads` into the new directory.

The v2 and v3 compose files use the same container names `Eco-hub`, `Eco-mysql`, `Eco-redis` and the same ports `3000` and `18080`. The new stack cannot start while the old containers exist, so remove them before installing v3:

```bash
docker rm -f Eco-hub Eco-mysql Eco-redis
```

This command removes only the containers. The old data in `data/` under the v2 install directory is not affected.

After installation the database is empty and contains only the admin account, the 12 preset collect sources, and the default scheduled tasks.

## Method 1: install into a new directory (recommended)

This method keeps the old data until the new site is confirmed to collect films.

1. Remove the old containers as described above.
2. Install into a new directory, for example `~/ecohub-v3`:

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_INSTALL_DIR=~/ecohub-v3 ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.5 sh
```

To set passwords before the first initialization, also place `JWT_SECRET`, `ECOHUB_ADMIN_PASSWORD`, `MYSQL_ROOT_PASSWORD`, `MYSQL_PASSWORD`, and `REDIS_PASSWORD` before `sh`. See the [Deploy guide](./README-Deploy_EN.md#set-passwords-on-the-first-install). With the bundled databases, keep `MYSQL_HOST=mysql` and `REDIS_HOST=redis`.

After installation, the `ecohub` command records the new directory, and `ecohub status` and `ecohub update` operate on `~/ecohub-v3`.

For 1Panel: create a new compose project and working directory using the contents of [deploy/release/compose.yml](../deploy/release/compose.yml). Do not reuse the old `./data`.

## Method 2: clear and reinstall in the original directory

Use this method only when the old data can be discarded. Replace `<old-install-dir>` below with the v2 install directory, which contains `docker-compose.yml`, `.env`, and `data/`.

1. Remove the old containers as described above, then delete the old data:

```bash
rm -rf <old-install-dir>/data/mysql <old-install-dir>/data/redis <old-install-dir>/data/uploads
```

2. Reinstall. The script keeps the existing `.env` and changes only the variables placed before `sh`; review the values in `.env` first. Because `.env` already exists, the script does not write an image version automatically. Specify the v3 image before `sh`:

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_INSTALL_DIR=<old-install-dir> ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.5 sh
```

## Method 3: external database

Do not point v3 to a database that v2 is using. Create an empty database, or drop the old database and let v3 create the tables. Do not use `127.0.0.1` inside a container. When the database runs on the host, use `host.docker.internal`.

## Verification

The upgrade is complete when all of the following are true:

- `ecohub status` shows all three containers running.
- The site URL printed in the terminal opens, and `/api/config/basic` returns JSON.
- After signing in to the admin panel and running a full collect, films appear on the site.

A reverse proxy that already points to `127.0.0.1:3000` requires no changes. If the new stack fails to start because a port or container name is in use, the old containers still exist.

## Related documents

[Deploy guide](./README-Deploy_EN.md) · [Release notes](./RELEASE_EN.md) · [FAQ](./README-FAQ_EN.md)
