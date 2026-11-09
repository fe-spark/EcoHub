<div align="center">

<img src="logo.png" alt="EcoHub" width="120" />

# EcoHub

[![Release](https://img.shields.io/github/v/release/fe-spark/EcoHub)](https://github.com/fe-spark/EcoHub/releases)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-16-black?logo=nextdotjs&logoColor=white)](https://nextjs.org/)
[![MySQL](https://img.shields.io/badge/MySQL-8-4479A1?logo=mysql&logoColor=white)](https://www.mysql.com/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?logo=redis&logoColor=white)](https://redis.io/)
[![License](https://img.shields.io/github/license/fe-spark/EcoHub)](./LICENSE)

**自托管影视聚合系统**

中文 | [English](./docs/README_EN.md)

[在线演示](https://eco.fe-spark.cn) · [v3 体验地址](http://148.135.106.141:3000) · [管理后台](https://eco.fe-spark.cn/manage) · [部署指南](./docs/README-Deploy.md) · [常见问题](./docs/README-FAQ.md) · [交流群组](https://t.me/ecohub_club)

</div>

> [!WARNING]
> v3.0 与 v2.x 不兼容，没有数据迁移。不要在旧的 `./data/mysql` 上 pull，也不要使用后台「立即升级」。用新目录或清空数据后重装，再全量采集。步骤见 [从 v2 升级到 v3](./docs/v2-to-v3.md)。当前正式版为 `3.0.1`，问题请提 [Issues](https://github.com/fe-spark/EcoHub/issues)。

作者时间有限，更新较慢。欢迎提 [PR](https://github.com/fe-spark/EcoHub/pulls)。

## 简介

多源采集、Web 观影和管理后台。客户端是独立仓库，以 submodule 放在 `app-for-ohos/`、`app-for-android/`：

- [EcoHub for OHOS](https://github.com/fe-spark/EcoHub-for-OHOS)
- [EcoHub for Android](https://github.com/fe-spark/EcoHub-for-Android)

## 在线演示

| 入口 | 地址 |
| --- | --- |
| 前台 | [https://eco.fe-spark.cn](https://eco.fe-spark.cn) |
| 管理后台 | [https://eco.fe-spark.cn/manage](https://eco.fe-spark.cn/manage) |
| v3 体验 | [http://148.135.106.141:3000](http://148.135.106.141:3000) |

访客 `guest` / `guest`，只读。演示站不支持移动线路，请用联通或电信。

## 推荐

| 类型 | 服务商 | 链接 |
| --- | --- | --- |
| 服务器 | CloudCone（演示站所用，磁盘 I/O 不限） | [选购](https://app.cloudcone.com/?ref=14393) |
| 网络 | 良心云，低至 2 元/月 | [注册](https://xn--9kqz23b19z.com/#/register?code=xAmvfdic) |
| 网络 | 赔钱机场，低至 1.5 元/月 | [注册](https://xn--mes358aby2apfg.com/register?code=FA4xlzHD&cover=sfw) |
| 网络 | DOVE | [注册](https://dovee.cc/a.php?asbcbO1PCgTx2JM66) |

## 安装

需要 Docker 20+ 和 Compose 2+，建议 2 核 2 GB。

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh \
  | ECOHUB_IMAGE=ghcr.io/fe-spark/ecohub:v3.0.1 sh
```

数据在 `~/ecohub`，命令是 `ecohub`。装完终端会打印 Web 端口、前台、后台和三条订阅。默认 Web 端口 `3000`。密码、反代、1Panel 见 [部署指南](./docs/README-Deploy.md)。

前台一开始没有影片。到后台采集中心做全量采集，影片随窗口出现，首次可能要数小时。

## 本地开发

本机先启动 MySQL 8 和 Redis 7。

```bash
cd server && cp .env.example .env && go run ./cmd/server
cd web && cp .env.example .env.local && npm install && npm run dev
```

前台 `http://127.0.0.1:3000`，后台 `/manage`，API `http://127.0.0.1:8080`。说明见 [服务端](./server/README.md)、[前端](./web/README.md)。

## 文档

| 文档 | 内容 |
| --- | --- |
| [部署指南](./docs/README-Deploy.md) | 安装、命令、密码、1Panel、反代 |
| [从 v2 升级到 v3](./docs/v2-to-v3.md) | 停旧栈、空库、全量采集 |
| [常见问题](./docs/README-FAQ.md) | 空站、主站、采集、缓存、登录 |
| [版本说明](./docs/RELEASE.md) | 本次变更 |
| [English](./docs/README_EN.md) | English overview |

> EcoHub 不提供、不存储影视文件。片源来自使用者自己的采集接口。请遵守当地法律和源站约定。

[PolyForm Noncommercial 1.0.0](./LICENSE) · [fe-spark/EcoHub](https://github.com/fe-spark/EcoHub) · [Issues](https://github.com/fe-spark/EcoHub/issues) · [Telegram](https://t.me/ecohub_club)
