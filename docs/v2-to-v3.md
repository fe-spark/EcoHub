# 从 v2 升级到 v3

中文 | [English](./v2-to-v3_EN.md)

## 结论

v3 读不了 v2 的数据库，也没有数据迁移。升级的做法是：删掉旧容器，用空的 MySQL 和 Redis 重新装 v3，再做一次全量采集。

安装、密码、端口和反向代理见 [部署指南](./README-Deploy.md)，变更内容见 [版本说明](./RELEASE.md)。

## 不要做的事

- 在已有 `./data/mysql` 的目录里直接 `docker compose pull && docker compose up -d`。
- 在 v2 后台点「立即升级」。
- 把旧的 `data/mysql`、`data/redis`、`data/uploads` 拷到新目录。

v2 和 v3 的编排用的是同样的容器名 `Eco-hub`、`Eco-mysql`、`Eco-redis` 和同样的端口 `3000`、`18080`。旧容器还在时，新栈起不来，所以装 v3 之前先删掉旧容器：

```bash
docker rm -f Eco-hub Eco-mysql Eco-redis
```

这条命令只删容器。旧数据在 v2 安装目录下的 `data/` 里，不受影响。

装好后是一个空库：只有管理员账号、12 个预设采集站和默认的计划任务。

## 方式一：装到新目录（推荐）

旧数据先留着，确认新站能采到片再删。

1. 按上文删掉旧容器。
2. 装到一个新目录，例如 `~/ecohub-v3`：

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_INSTALL_DIR=~/ecohub-v3 ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2 sh
```

想在第一次初始化前就设好密码，把 `JWT_SECRET`、`ECOHUB_ADMIN_PASSWORD`、`MYSQL_ROOT_PASSWORD`、`MYSQL_PASSWORD`、`REDIS_PASSWORD` 也写在 `sh` 前面，写法见 [部署指南](./README-Deploy.md#第一次安装就设好密码)。内置数据库保持 `MYSQL_HOST=mysql`、`REDIS_HOST=redis`。

装完以后 `ecohub` 命令会记住新目录，`ecohub status`、`ecohub update` 都操作 `~/ecohub-v3`。

用 1Panel 的：新建一套编排和工作目录，内容用 [deploy/release/compose.yml](../deploy/release/compose.yml)，不要复用旧的 `./data`。

## 方式二：在原目录清空重装

确认旧数据可以丢掉再做。下面的 `<旧安装目录>` 换成 v2 的安装目录，也就是放着 `docker-compose.yml`、`.env` 和 `data/` 的那个目录。

1. 按上文删掉旧容器，再删除旧数据：

```bash
rm -rf <旧安装目录>/data/mysql <旧安装目录>/data/redis <旧安装目录>/data/uploads
```

2. 重新安装。脚本会保留已有的 `.env`，只改写写在 `sh` 前面的变量，所以先检查一遍 `.env` 里的值。因为 `.env` 已经存在，脚本不会自动写入镜像版本，要在 `sh` 前面写上 v3 镜像：

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_INSTALL_DIR=<旧安装目录> ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.2 sh
```

## 方式三：自备数据库

不要把 v3 指向 v2 正在用的库。新建一个空库，或者删掉旧库后让 v3 自己建表。容器里不要写 `127.0.0.1`，数据库在宿主机上时写 `host.docker.internal`。

## 怎样算完成

- `ecohub status` 里三个容器都在运行。
- 终端打印的前台地址能打开，`/api/config/basic` 返回 JSON。
- 登录后台做一次全量采集，前台出现影片。

反向代理原来就指向 `127.0.0.1:3000` 的，不用改。新栈起不来、提示端口或容器名被占用，说明旧容器还在。

## 相关

[部署指南](./README-Deploy.md) · [版本说明](./RELEASE.md) · [常见问题](./README-FAQ.md)
