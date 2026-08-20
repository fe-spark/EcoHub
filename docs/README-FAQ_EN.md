# FAQ and troubleshooting

[中文](./README-FAQ.md) | English

This page only covers what the current repo actually does.

## Collect and data

### Why do I have to configure a master?

The master stores film basics, categories, details, and the public list. Without a master, slave play URLs have nowhere to attach, and search / detail pages have no base data.

### How do master and slave split the work?

- Master writes the film record.
- Slaves write playlists.
- Detail and play pages attach slave sources under the master film.
- Admin can “update all sites” for one film.

### Why rebuild after switching the master?

The master owns basics, categories, and search. After a switch, rebuild with the new master so old and new categories, details, and lists do not mix.

### Why dedupe titles?

Douban ID is preferred to decide whether two rows are the same film. Without that ID, title and similar fields help. That cuts duplicate films and search hits.

### What happens if I stop a collect by hand?

Stop blocks new pages and cancels running jobs. Rows that already landed keep getting finished so the site can still show them.

If some pages wrote successfully before the stop, the site may only show that partial increment.

### Why is the site empty after I finish setup?

EcoHub does not ship media data. After the first install, configure collect sources in admin and run a collect.

The first full collect **can take several hours**, longer with more sources. While it is still running, the site, admin list, and TVBox stay empty or incomplete. That is expected.

When collect finishes, the system also prepares lists and filters. After that, home, categories, search, details, the admin film list, and TVBox / MacCMS APIs can show those titles.

## Snapshots, filters, and cache

### Why publish a snapshot after collect?

Data keeps changing during collect. After collect ends, the system prepares one copy for pages. Public lists, filters, and TVBox APIs read that prepared copy. Pages stay steadier and faster.

### Why can an incremental publish still take time?

Even a partial update still:

- Checks that films and details are valid
- Updates public list data
- Updates category, year, region, and other filters
- Refreshes in-process cache

A large batch still costs database writes and cache refresh.

### Why didn’t the site change after I edited config?

Common causes:

- Browser cache
- CDN or reverse-proxy cache
- The running instance is not the latest code
- You changed a field the public site does not show
- List and filter data after collect is not finished yet

The backend refreshes its own cache after a master switch, category rebuild, or list publish. It cannot control browser, CDN, or external proxy caches.

### Why should the public category page match TVBox?

They use the same query rules:

- Same category conditions
- Same plot, region, language, year filter meaning
- Same “other” handling
- Same sort rules

If they still differ, check cache, which instance is running, and request parameters first.

### What does “recently updated” actually use?

Recently updated only looks at the master’s resource update time.

That means:

- Master data changes move recently updated
- Syncing slave play sources does not change that sort
- Public category pages and TVBox lists sort the same way

## Login and permissions

### Why can I open admin pages but APIs say I am not logged in?

`/manage` only checks that a cookie exists on the frontend. Real JWT checks, Redis token checks, and permissions run on `/api/manage/*`.

Getting into the shell does not mean APIs will work. Trust the API response.

### Why cookies instead of localStorage?

Admin login uses an `HttpOnly` cookie:

- The frontend does not store the token itself
- The backend owns the session and auto-renewal
- Frontend scripts are less able to read the token

### What can a guest account do?

Guests can view admin data. Every write is rejected by the backend `WriteAccess` middleware.

Demo guest: `guest / guest`.

### Can I use the default accounts in production?

No. Defaults are for first boot and demos. After a public deploy, change passwords or replace the account system.

## Docker and deploy

### Is the release one container or two?

**v2.0+ release is one All-in-One image** `ghcr.io/fe-spark/ecohub`: Supervisord runs Next (`:3000`) and Go API (`:8080`) in the same container. Compose service is usually `ecohub` (container `Eco-hub`), plus bundled `mysql` / `redis`.

Old `ecohub-web` / `ecohub-server` images are retired. Root `docker-compose.yml` for source still splits `web` + `server` for local builds. That is not the release shape.

### Do I still set `API_URL` on the release?

**No.** Inside the image the default is `API_URL=http://127.0.0.1:8080` (loopback to Go in the same container).

**Source compose** (split web/server) injects something like:

```env
API_URL=http://server:${SERVER_PORT:-8080}
```

Do not treat “`127.0.0.1` inside the container” as the host backend. That only works in the All-in-One same-container case. Trailing `/api` is optional; the app normalizes it.

Two access models:

- Browser via the site: `/api/*` (Next rewrite → Go)
- Direct API port (`SERVER_PUBLIC_PORT`, default `18080`): path still starts with `/api`, e.g. `/api/health`

### Docker cannot reach a database on the host

Check first:

- `MYSQL_HOST` / `REDIS_HOST` is an address the container can reach
- `host.docker.internal` works on Linux (this repo’s compose already sets `extra_hosts`)
- The DB user allows the container network
- Firewall allows the port
- MySQL / Redis is not bound only to `127.0.0.1`

### Bundled MySQL / Redis fail to start

Check first:

- `docker compose logs -f mysql` / `redis`
- Passwords in `.env` match what compose injects
- Release bundled DBs are **not** mapped to host `3306`/`6379` by default; source compose may map `127.0.0.1:3306` for local `go run` — watch for conflicts

### Site opens, every API fails

Check first:

- Release: `docker compose logs -f ecohub`, and `http://HOST:18080/api/health`
- Source: `server` is healthy; web `API_URL` points at a resolvable `server` service
- Reverse proxy forwards `/api/*` correctly
- Browser is not blocked by HTTPS, CORS, or cookie policy

### `web` and `server` port clash in local development

- `server` defaults to `8080`, `web` to `3000`
- Do not move Next onto `8080`. After changing `PORT` in `web/.env.local`, restart the frontend

### What to watch when upgrading v1.x to v2.x

- Switch to the single-image compose (see [Deploy guide · Upgrade from v1.x](./README-Deploy_EN.md#5-upgrade-from-v1x))
- Back up data, then `pull` + `up -d`. Do not let old and new servers share one database
- Release tags overwrite `ghcr.io/fe-spark/ecohub:latest`

## Known notes

- `/api/config/basic` is still a public API
- Frontend lint may still warn about image optimization
- Bundled MySQL / Redis only start if compose includes those services
- Preparing a very large film set can bump memory for a short time

## Doc index

- [README (English)](./README_EN.md)
- [Release notes](./RELEASE.md)
- [Server](../server/README.md)
- [Web](../web/README.md)
- [Deploy guide (English)](./README-Deploy_EN.md)
