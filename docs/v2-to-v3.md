# 从 v2 升级到 v3

v3 与 v2 的库不兼容，没有迁移。停掉旧栈，用空的 MySQL 和 Redis 重新部署，再全量采集。

安装、密码、端口和反代见 [部署指南](./README-Deploy.md)。变更见 [版本说明](./RELEASE.md)。

## 不要做的事

- 在已有 `./data/mysql` 上执行 `docker compose pull && docker compose up -d`
- 使用后台「立即升级」
- 拷贝 `data/mysql`、`data/redis`、`data/uploads`

容器名 `Eco-hub`、`Eco-mysql`、`Eco-redis` 和端口 `3000`、`18080` 会挡住新栈。先停旧容器。

空库只有管理员和 12 个预设采集站。旧 `.env` 里的密钥可以沿用。

## 新目录

```bash
cd ~/ecohub
docker compose down
```

旧目录还在时换一个路径。确认新站能采到片之后再删旧目录。

```bash
ECOHUB_INSTALL_DIR=~/ecohub-v3 \
ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.0-preview \
  curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
```

要在第一次初始化前改密码，把 `JWT_SECRET`、`ECOHUB_ADMIN_PASSWORD`、`MYSQL_ROOT_PASSWORD`、`MYSQL_PASSWORD`、`REDIS_PASSWORD` 加在命令前面。内置库保持 `MYSQL_HOST=mysql`、`REDIS_HOST=redis`。密码何时生效见部署指南。

1Panel 新建一套编排和工作目录，内容用 [deploy/release/compose.yml](../deploy/release/compose.yml)，不要复用旧的 `./data`。

## 原目录清空

确认旧数据可以丢弃：

```bash
cd ~/ecohub
docker compose down
rm -rf data/mysql data/redis data/uploads
```

已有 `.env` 会保留。把预览镜像加在命令前面再装：

```bash
ECOHUB_INSTALL_DIR=~/ecohub \
ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.0-preview \
  curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
```

## 自备数据库

不要把 v3 指回 v2 正在用的库。新建空库，或删掉旧库后让 v3 自己建表。容器内不要写 `127.0.0.1`。库在宿主机上时用 `host.docker.internal`。

## 怎样算完成

- `ecohub status` 里三个容器都在跑
- 打印出的前台能打开，`/api/config/basic` 有响应
- 后台登录后全量采集，前台出现影片

反代仍指向 `127.0.0.1:3000` 时不用改。端口没释放，说明旧容器还在。
