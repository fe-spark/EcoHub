<div align="center">

<img src="logo.png" alt="EcoHub" width="120" />

# EcoHub

[![Release](https://img.shields.io/github/v/release/fe-spark/EcoHub)](https://github.com/fe-spark/EcoHub/releases)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-16-black?logo=nextdotjs&logoColor=white)](https://nextjs.org/)
[![MySQL](https://img.shields.io/badge/MySQL-8-4479A1?logo=mysql&logoColor=white)](https://www.mysql.com/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?logo=redis&logoColor=white)](https://redis.io/)
[![License](https://img.shields.io/github/license/fe-spark/EcoHub)](./LICENSE)

**自托管影视聚合系统**

中文 | [English](./docs/README_EN.md)

[在线演示](https://eco.fe-spark.cn) · [管理后台](https://eco.fe-spark.cn/manage) · [部署指南](./docs/README-Deploy.md) · [常见问题](./docs/README-FAQ.md) · [交流群组](https://t.me/+6O6MiUdVSOplNjQ0)

</div>

> **使用须知**  
> EcoHub 不提供、不存储任何影视文件。片源来自使用者自行配置的采集接口。请遵守所在地区的法律法规以及各源站的使用约定，由此产生的风险由使用者自行承担。本项目仅供学习与技术交流。

## 简介

EcoHub 是一款高性能、现代化的全栈多源影视聚合系统。它不仅提供极致流畅的 Web 观影体验，还集成了强大的自动化采集与管理后台，旨在帮助开发者和影视爱好者快速搭建属于自己的私人影视库。

客户端是独立 App 仓库（本项目以 Git Submodule 形式接入在 `app-for-ohos/` 与 `app-for-android/`）：

- **EcoHub for OHOS** (鸿蒙客户端): [fe-spark/EcoHub-for-OHOS](https://github.com/fe-spark/EcoHub-for-OHOS)
- **EcoHub for Android** (安卓客户端): [fe-spark/EcoHub-for-Android](https://github.com/fe-spark/EcoHub-for-Android)


## 在线演示

| 入口 | 地址 |
| --- | --- |
| 前台 | [https://eco.fe-spark.cn](https://eco.fe-spark.cn) |
| 管理后台 | [https://eco.fe-spark.cn/manage](https://eco.fe-spark.cn/manage) |

只读演示账号：`guest` / `guest`。该账号不可保存配置，正式部署请使用自有账号并修改默认密码。

## 推荐

### 服务器

| 服务商 | 说明 | 链接 |
| --- | --- | --- |
| CloudCone | 1. 演示站点所用服务商<br>2. 高性价比 VPS，磁盘 I/O 不受限 | [前往选购](https://app.cloudcone.com/?ref=14393) |

### 网络服务

| 服务商 | 特点 / 价格 | 链接 |
| --- | --- | --- |
| 良心云 | 1. 2 元/月 100G，6 元 1000G（1T）<br>2. 直连 AWS 与 Oracle，协议支持 VLESS Reality 与 Hysteria2<br>3. 解锁 Netflix、Disney+、TikTok、ChatGPT，1 倍率无审计<br>4. 覆盖新疆、河南、福建等地区，高峰时段可流畅播放 4K<br>5. 新用户注册即获体验流量 | [前往注册](https://xn--9kqz23b19z.com/#/register?code=xAmvfdic) |
| 赔钱机场 | 1. 低至 1.5 元/月，超低价大流量与高性价比<br>2. 支持 VLESS、Hysteria 2、Shadowsocks 等多协议直连<br>3. 节点覆盖香港、日本、台湾、新加坡、美国等地区<br>4. 提供超低倍率大流量下载节点，适合高流量与备用需求 | [前往注册](https://xn--mes358aby2apfg.com/register?code=FA4xlzHD&cover=sfw) |

## 快速开始

要求 Docker 20+、Compose 2+，建议配置不低于 2 核 2 GB 内存。

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
cd ~/ecohub
```

编辑 `.env`：将 `openssl rand -hex 32` 的输出写入 `JWT_SECRET`，并修改 MySQL / Redis 密码，然后启动：

```bash
docker compose up -d
```

| 地址 | 说明 |
| --- | --- |
| `http://<主机>:3000` | 前台 |
| `http://<主机>:3000/manage` | 管理后台 |
| `http://<主机>:3000/api` | 客户端（[EcoHub for OHOS](https://github.com/fe-spark/EcoHub-for-OHOS) / [EcoHub for Android](https://github.com/fe-spark/EcoHub-for-Android)）服务接入地址 |
| `http://<主机>:3000/api/provide/config` | TVBox / 影视仓 订阅地址 |
| `http://<主机>:3000/api/provide/vod` | MacCMS 兼容接口 |
| `http://<主机>:18080/api/*` | API 直连（可选，仅供内网或特定播放器直连） |

默认账号：`admin` / `admin`（读写）、`guest` / `guest`（只读）。对外部署前须立即修改默认密码。

> **安全与网络建议**：生产环境建议通过 Nginx / 1Panel 配置反向代理与 HTTPS（80/443）。如需**不对公网暴露裸端口**，请在 `compose.yml` 的 `ports` 中显式绑定 `127.0.0.1:`（如 `127.0.0.1:3000:3000`），避免 Docker 默认规则绕过系统防火墙直接暴露端口；若无播放器直连需求，可直接注释 `18080` 端口映射。

安装完成后前台无数据，属预期行为。须在管理后台 **采集中心** 完成全量采集并发布后，前台才会展示影片。1Panel、外部数据库与反向代理见 [部署指南](./docs/README-Deploy.md)。

Telegram 通知在管理后台 **系统设置 → 通知配置** 中填写。境内服务器访问 Telegram 时经常出现超时，可在 `.env` 中设置 `TG_PROXY=http://host.docker.internal:7890`。详见 [server/notify.md](./server/notify.md)。

## 本地开发

请先在本地启动 MySQL 8 与 Redis 7。后端与前端需两个终端，均从仓库根目录执行。

API：

```bash
cd server
cp .env.example .env
go run ./cmd/server
```

Web：

```bash
cd web
cp .env.example .env.local
npm install
npm run dev
```

前台 `http://127.0.0.1:3000`，后台 `/manage`，API `http://127.0.0.1:8080`。[服务端](./server/README.md) · [前端](./web/README.md)

## 文档

| 文档 | 内容 |
| --- | --- |
| [部署指南](./docs/README-Deploy.md) | 安装脚本、1Panel、手动部署、反向代理与升级 |
| [常见问题](./docs/README-FAQ.md) | 空站、采集、缓存、登录 |
| [版本说明](./docs/RELEASE.md) | 变更记录、镜像 tag |
| [服务端](./server/README.md) / [前端](./web/README.md) | 环境变量、接口、本地启动 |
| [English](./docs/README_EN.md) | English overview |

## 交流社区

- Telegram 交流群：[https://t.me/+6O6MiUdVSOplNjQ0](https://t.me/+6O6MiUdVSOplNjQ0)

---

[PolyForm Noncommercial 1.0.0](./LICENSE) · [fe-spark/EcoHub](https://github.com/fe-spark/EcoHub) · [Issues](https://github.com/fe-spark/EcoHub/issues) · [Telegram Group](https://t.me/+6O6MiUdVSOplNjQ0)
