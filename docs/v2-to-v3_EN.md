# Upgrading from v2 to v3

[中文](./v2-to-v3.md) | English

## Summary

v3 cannot read a v2 database, and there is no migration. To upgrade: remove the old containers, install v3 on an empty MySQL and Redis, then run a full collect.

Install, passwords, ports, and reverse proxy: [Deploy guide](./README-Deploy_EN.md). What changed: [Release notes](./RELEASE_EN.md).

## Do not

- Run `docker compose pull && docker compose up -d` in a directory that already has `./data/mysql`.
- Click "立即升级" (upgrade now) in the v2 admin.
- Copy the old `data/mysql`, `data/redis`, or `data/uploads` into the new directory.

The v2 and v3 compose files use the same container names `Eco-hub`, `Eco-mysql`, `Eco-redis` and the same ports `3000` and `18080`. While the old containers exist, the new stack cannot start, so remove them before installing v3:

```bash
docker rm -f Eco-hub Eco-mysql Eco-redis
```

This only removes the containers. The old data stays in `data/` under the v2 install directory.

After install you have an empty database: only the admin account, the 12 preset collect sources, and the default scheduled tasks.

## Method 1: install into a new directory (recommended)

Keep the old data until the new site collects films, then delete it.

1. Remove the old containers as shown above.
2. Install into a new directory, for example `~/ecohub-v3`:

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_INSTALL_DIR=~/ecohub-v3 ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2 sh
```

To set passwords before the first initialization, also put `JWT_SECRET`, `ECOHUB_ADMIN_PASSWORD`, `MYSQL_ROOT_PASSWORD`, `MYSQL_PASSWORD`, and `REDIS_PASSWORD` before `sh`. See [Deploy guide](./README-Deploy_EN.md#set-passwords-on-the-first-install). With the bundled databases, keep `MYSQL_HOST=mysql` and `REDIS_HOST=redis`.

After install, the `ecohub` command remembers the new directory. `ecohub status` and `ecohub update` work on `~/ecohub-v3`.

With 1Panel: create a new compose project and working directory with the content of [deploy/release/compose.yml](../deploy/release/compose.yml). Do not reuse the old `./data`.

## Method 2: wipe and reinstall in the original directory

Only do this when the old data can go. Replace `<old-install-dir>` below with the v2 install directory, the one that holds `docker-compose.yml`, `.env`, and `data/`.

1. Remove the old containers as shown above, then delete the old data:

```bash
rm -rf <old-install-dir>/data/mysql <old-install-dir>/data/redis <old-install-dir>/data/uploads
```

2. Reinstall. The script keeps the existing `.env` and only changes the variables placed before `sh`, so check the values in `.env` first. Because `.env` already exists, the script does not write an image version on its own. Put the v3 image before `sh`:

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_INSTALL_DIR=<old-install-dir> ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2 sh
```

## Method 3: bring your own database

Do not point v3 at a database v2 is using. Create an empty database, or drop the old one and let v3 create the tables. Inside a container do not use `127.0.0.1`. When the database runs on the host, use `host.docker.internal`.

## How to tell it worked

- `ecohub status` shows all three containers running.
- The site URL printed in the terminal opens, and `/api/config/basic` returns JSON.
- Log in to admin, run a full collect, and films appear on the site.

If your reverse proxy already points to `127.0.0.1:3000`, leave it. If the new stack fails because a port or container name is in use, the old containers are still there.

## Related

[Deploy guide](./README-Deploy_EN.md) · [Release notes](./RELEASE_EN.md) · [FAQ](./README-FAQ_EN.md)
