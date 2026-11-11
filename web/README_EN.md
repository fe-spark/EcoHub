# Web

[中文](./README.md) | English

`web/` contains the EcoHub Next.js front end, which includes the public site, the login page, and the admin panel.

Stack: Next.js 16, React 19, TypeScript, Ant Design 6, Axios, Less, Artplayer, Hls.js.

## Request forwarding

The browser requests only `/api/*` on the same origin, and Next forwards these requests to the Go backend at `API_URL`. During server-side rendering, `src/lib/server-api.ts` calls the backend at `API_URL` directly.

```mermaid
flowchart LR
    Public["src/app/(public)"] --> ServerAPI["src/lib/server-api.ts"]
    Public --> ClientUI["Client components"]
    ClientUI --> ClientAPI["src/lib/client-api.ts"]
    Manage["src/app/manage"] --> ClientAPI
    ServerAPI --> Backend["Go API"]
    ClientAPI --> Rewrite["next.config.ts forwards /api/*"]
    Rewrite --> Backend
```

Example: with `API_URL=http://127.0.0.1:8080`, the browser requests `http://127.0.0.1:3000/api/home` and Next forwards it to `http://127.0.0.1:8080/api/home`.

## Run locally

Requirements: Node.js 20.9+ and a running Go backend (see [Server](../server/README_EN.md)).

```bash
cd web
cp .env.example .env.local
npm install
npm run dev
```

Contents of `.env.example`:

```env
PORT=3000
API_URL=http://127.0.0.1:8080
```

The site is available at `http://127.0.0.1:3000`, the login page at `/login`, and the admin panel at `/manage`.

### Run the web app in a container

The development compose file `docker-compose.yml` in the repository root includes a `web` service that is built from source with `web/Dockerfile`; the container is named `Eco-web`. This file is intended for local development only. For its differences from the release file, see [Overview](../docs/README_EN.md#local-development).

```bash
docker compose up -d mysql redis
docker compose up -d --build web
```

- Run the commands in the repository root. `web` depends on the `server` health check, so `server` is also built and started.
- `API_URL` is `http://server:8080` at both build time and run time, which refers to the `server` service in the same compose file. `web/.env.local` is not read.
- The site is mapped to `3000` on the host (`WEB_PORT`).

## Environment variables

| Variable | Default | Required | Notes |
| --- | --- | --- | --- |
| `PORT` | `3000` | No | Local development port |
| `API_URL` | none | Yes | Go backend address. Both build and start fail when it is not set |

Notes on `API_URL`:

- The `/api` suffix and the trailing `/` are optional: `http://127.0.0.1:8080` and `http://127.0.0.1:8080/api/` are equivalent.
- If the backend runs on another machine, use that machine's address.
- Restart Next after changing the value. `API_URL` is embedded in the build output, so production builds require `npm run build` again after a change.
- Inside a container, `127.0.0.1` refers to the container itself.
- The release image sets it to `http://127.0.0.1:8080` at build time; it is not set during deployment.

## Authentication

- The sign-in state is stored in the HttpOnly cookie `ecohub_auth_token` set by the backend.
- Access to `/manage` passes two checks: `src/proxy.ts` first checks that the cookie exists and redirects to `/login` if not; `src/app/manage/layout.tsx` then requests `/api/manage/user/info` and also redirects to `/login` if the backend rejects it.
- The backend validates the JWT, the token in Redis, and write permission. `src/lib/client-api.ts` redirects to `/login` on `401` and shows a no-permission message on `403`.

## Directory layout

```text
web/
├── src/app/(public)/    # Public site
├── src/app/login/       # Login page
├── src/app/manage/      # Admin panel
├── src/components/      # Shared components
├── src/lib/             # server-api.ts, client-api.ts, api-base.ts, etc.
├── src/proxy.ts         # Cookie check for /manage
├── patches/             # Patches applied by patch-package after npm install
└── next.config.ts       # /api/* forwarding, standalone output
```

## Commands

| Command | Description |
| --- | --- |
| `npm run dev` | Development mode |
| `npm run build` | Production build, standalone output |
| `npm run start` | Run the production build |
| `npm run lint` | Run ESLint |

## Related documents

[Overview](../docs/README_EN.md) · [Server](../server/README_EN.md) · [Deploy guide](../docs/README-Deploy_EN.md) · [FAQ](../docs/README-FAQ_EN.md)
