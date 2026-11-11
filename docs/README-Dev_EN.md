# Development Guide

[中文](./README-Dev.md) | English

This guide describes how to set up a local development environment for EcoHub. For details on the backend and the front end, see [Server](../server/README_EN.md) and [Web](../web/README_EN.md).

## Requirements

- Go 1.27+
- Node.js 20.9+
- MySQL 8
- Redis 7

## Development compose file

The `docker-compose.yml` in the repository root is intended for local development only and must not be used for deployment. It differs from the release file [deploy/release/compose.yml](../deploy/release/compose.yml) as follows:

| | Root `docker-compose.yml` | `deploy/release/compose.yml` |
| --- | --- | --- |
| App | Built locally from the `server/` and `web/` sources, running as two containers, `Eco-server` and `Eco-web` | Pulls the All-in-One image and runs a single `Eco-hub` container |
| MySQL and Redis ports | Mapped to `127.0.0.1:3306` and `127.0.0.1:6379` on the host | Not mapped |
| Data | Docker named volumes `eco-mysql-data`, `eco-redis-data`, `eco-server-uploads` | `data/` in the install directory |

## Mode 1: databases in containers only

Intended for day-to-day development: the databases run in containers, and the server and web app run on the host. The database credentials match `server/.env.example`.

1. Start the databases:

   ```bash
   docker compose up -d mysql redis
   ```

2. Start the server and the web app in two separate terminals:

   ```bash
   cd server && cp .env.example .env && go run ./cmd/server
   ```

   ```bash
   cd web && cp .env.example .env.local && npm install && npm run dev
   ```

The site is available at `http://127.0.0.1:3000`, the admin panel at `http://127.0.0.1:3000/manage`, and the API at `http://127.0.0.1:8080`.

## Mode 2: build all containers from source

Intended for validating the Dockerfiles or for integration testing:

```bash
docker compose up -d --build
```

The site is available at `http://127.0.0.1:3000` and the API at `http://127.0.0.1:18080`. Ports and passwords can be changed in a `.env` file in the repository root (copied from [.env.example](../.env.example); the defaults apply when the file is absent).

## Precautions

- The container names `Eco-mysql` and `Eco-redis` are the same as in the release file. Do not run the development stack and a production deployment on the same machine.
- If a MySQL or Redis instance on the host already uses port `3306` or `6379`, the development stack cannot start. Stop the host service, or use the host database instead.

## Related documents

[Overview](./README_EN.md) · [Server](../server/README_EN.md) · [Web](../web/README_EN.md) · [Deploy guide](./README-Deploy_EN.md)
