# Development Guide

[中文](../../dev/setup.md) | English

This guide describes how to set up a local development environment for EcoHub. For module details, see [Server](../../../server/README_EN.md) and [Web](../../../web/README_EN.md).

## Requirements

- Go 1.27+
- Node.js 20.9+
- MySQL 8
- Redis 7

## Development compose file

The `docker-compose.yml` in the repository root is for local development only and must not be used for production deployment. Differences from [deploy/release/compose.yml](../../../deploy/release/compose.yml):

| | Root `docker-compose.yml` | `deploy/release/compose.yml` |
| --- | --- | --- |
| App | Built locally from `server/` and `web/`, running in `Eco-server` and `Eco-web` | Pulls the All-in-One image and runs a single `Eco-hub` container |
| MySQL and Redis ports | Mapped to host `127.0.0.1:3306` and `127.0.0.1:6379` | Not mapped |
| Data | Docker named volumes `eco-mysql-data`, `eco-redis-data`, `eco-server-uploads` | `data/` under the install directory |

## Mode 1: databases in containers only

Intended for day-to-day development: databases run in containers while the Go API and Next.js run on the host. Credentials match `server/.env.example`.

1. Start databases:

   ```bash
   docker compose up -d mysql redis
   ```

2. Start backend and frontend in separate terminals:

   ```bash
   cd server && cp .env.example .env && go run ./cmd/server
   ```

   ```bash
   cd web && cp .env.example .env.local && npm install && npm run dev
   ```

Site: `http://127.0.0.1:3000`, admin panel: `http://127.0.0.1:3000/manage`, API: `http://127.0.0.1:8080`.

## Mode 2: build all containers from source

For validating Dockerfiles or integration runs:

```bash
docker compose up -d --build
```

Site: `http://127.0.0.1:3000`, direct API: `http://127.0.0.1:18080`. Customize ports and passwords in root `.env` (copied from [.env.example](../../../.env.example)).

## Precautions

- Container names `Eco-mysql` and `Eco-redis` collide with the release compose file; do not run both stacks on the same host.
- If host MySQL or Redis already binds `3306` or `6379`, stop them first or point `server/.env` directly to the host services.

## Related documents

[Core mechanics and data flow](./architecture.md) · [Scheduled task conflicts](./cron-task-conflict.md) · [Server](../../../server/README_EN.md) · [Web](../../../web/README_EN.md)
