# 从 v2 升级到 v3

中文 | [English](./v2-to-v3_EN.md)

## 概述

v3 无法读取 v2 的数据库，也不提供数据迁移。升级方法为：删除旧容器，使用空的 MySQL 和 Redis 重新安装 v3，然后执行一次全量采集。

安装、密码、端口和反向代理见 [部署指南](./README-Deploy.md)，变更内容见 [版本说明](./RELEASE.md)。

## 注意事项

以下操作不受支持：

- 在已有 `./data/mysql` 的目录中直接执行 `docker compose pull && docker compose up -d`。
- 在 v2 后台使用「立即升级」。
- 将旧的 `data/mysql`、`data/redis`、`data/uploads` 复制到新目录。

v2 和 v3 的编排使用相同的容器名 `Eco-hub`、`Eco-mysql`、`Eco-redis` 和相同的端口 `3000`、`18080`。旧容器存在时新服务无法启动，因此安装 v3 前须先删除旧容器：

```bash
docker rm -f Eco-hub Eco-mysql Eco-redis
```

该命令只删除容器，v2 安装目录下 `data/` 中的旧数据不受影响。

安装完成后数据库为空，只包含管理员账号、12 个预设采集站和默认的计划任务。

## 方式一：安装到新目录（推荐）

此方式保留旧数据，待确认新站点能够正常采集后再删除。

1. 按上文删除旧容器。
2. 安装到新目录，例如 `~/ecohub-v3`：

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_INSTALL_DIR=~/ecohub-v3 ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.5 sh
```

如需在首次初始化前设置密码，将 `JWT_SECRET`、`ECOHUB_ADMIN_PASSWORD`、`MYSQL_ROOT_PASSWORD`、`MYSQL_PASSWORD`、`REDIS_PASSWORD` 一并写在 `sh` 之前，写法见 [部署指南](./README-Deploy.md#第一次安装就设好密码)。使用内置数据库时保持 `MYSQL_HOST=mysql`、`REDIS_HOST=redis`。

安装完成后，`ecohub` 命令会记录新目录，`ecohub status`、`ecohub update` 均操作 `~/ecohub-v3`。

使用 1Panel 时：新建编排和工作目录，内容使用 [deploy/release/compose.yml](../deploy/release/compose.yml)，不要复用旧的 `./data`。

## 方式二：在原目录清空后重装

仅在确认旧数据可以丢弃时使用此方式。将下文的 `<旧安装目录>` 替换为 v2 的安装目录，即包含 `docker-compose.yml`、`.env` 和 `data/` 的目录。

1. 按上文删除旧容器，然后删除旧数据：

```bash
rm -rf <旧安装目录>/data/mysql <旧安装目录>/data/redis <旧安装目录>/data/uploads
```

2. 重新安装。脚本会保留已有的 `.env`，只改写写在 `sh` 之前的变量，因此请先检查 `.env` 中的值。由于 `.env` 已存在，脚本不会自动写入镜像版本，须在 `sh` 之前指定 v3 镜像：

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_INSTALL_DIR=<旧安装目录> ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.5 sh
```

## 方式三：自备数据库

不得将 v3 指向 v2 正在使用的数据库。请新建空库，或删除旧库后由 v3 自动建表。容器内不得使用 `127.0.0.1`；数据库位于宿主机时，使用 `host.docker.internal`。

## 验证

满足以下条件即表示升级完成：

- `ecohub status` 显示三个容器均在运行。
- 终端输出的前台地址可以打开，`/api/config/basic` 返回 JSON。
- 登录后台执行一次全量采集后，前台出现影片。

原有反向代理已指向 `127.0.0.1:3000` 时无需修改。新服务无法启动并提示端口或容器名被占用时，表示旧容器仍然存在。

## 相关文档

[部署指南](./README-Deploy.md) · [版本说明](./RELEASE.md) · [常见问题](./README-FAQ.md)
