# 开发指南

中文 | [English](./README-Dev_EN.md)

本文说明如何在本地搭建 EcoHub 的开发环境。后端和前端的详细说明见 [服务端说明](../server/README.md) 和 [前端说明](../web/README.md)。

## 环境要求

- Go 1.27+
- Node.js 20.9+
- MySQL 8
- Redis 7

## 开发编排

仓库根目录的 `docker-compose.yml` 仅用于本地开发，不适用于生产部署。它与发布编排 [deploy/release/compose.yml](../deploy/release/compose.yml) 的区别如下：

| | 根目录 `docker-compose.yml` | `deploy/release/compose.yml` |
| --- | --- | --- |
| 应用 | 从 `server/`、`web/` 源码本地构建，前后端分别运行在 `Eco-server`、`Eco-web` 两个容器中 | 拉取 All-in-One 镜像，运行单个 `Eco-hub` 容器 |
| MySQL、Redis 端口 | 映射到本机 `127.0.0.1:3306`、`127.0.0.1:6379` | 不映射 |
| 数据 | Docker 命名卷 `eco-mysql-data`、`eco-redis-data`、`eco-server-uploads` | 安装目录下的 `data/` |

## 方式一：仅在容器中运行数据库

适用于日常开发：数据库运行在容器中，前后端在本机运行。数据库账号和密码与 `server/.env.example` 一致。

1. 启动数据库：

   ```bash
   docker compose up -d mysql redis
   ```

2. 在两个终端中分别启动后端和前端：

   ```bash
   cd server && cp .env.example .env && go run ./cmd/server
   ```

   ```bash
   cd web && cp .env.example .env.local && npm install && npm run dev
   ```

前台地址为 `http://127.0.0.1:3000`，后台地址为 `http://127.0.0.1:3000/manage`，后端 API 地址为 `http://127.0.0.1:8080`。

## 方式二：从源码构建全部容器

适用于验证 Dockerfile 或联调：

```bash
docker compose up -d --build
```

前台地址为 `http://127.0.0.1:3000`，后端 API 地址为 `http://127.0.0.1:18080`。端口和密码可在仓库根目录的 `.env` 中修改（从 [.env.example](../.env.example) 复制；不创建时使用默认值）。

## 注意事项

- 开发编排的容器名 `Eco-mysql`、`Eco-redis` 与发布编排相同，同一台机器上不能同时运行开发编排和正式部署。
- 本机已有 MySQL 或 Redis 占用 `3306`、`6379` 时，开发编排无法启动。请先停止本机服务，或直接使用本机数据库。

## 相关文档

[总览](../README.md) · [服务端说明](../server/README.md) · [前端说明](../web/README.md) · [部署指南](./README-Deploy.md)
