<div align="center">

<img src="logo.png" alt="EcoHub Logo" width="128" />

# EcoHub

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8.svg)](https://golang.org/)
[![Next.js](https://img.shields.io/badge/Next.js-16-black.svg)](https://nextjs.org/)
[![MySQL](https://img.shields.io/badge/MySQL-8-4479A1.svg)](https://www.mysql.com/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D.svg)](https://redis.io/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED.svg)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](./LICENSE)

**自己搭一个影视站：电脑看、电视看、手机看**

中文 | [English](./docs/README_EN.md)

一条命令装好网站和管理后台。填上采集源、点一下采集，就能在浏览器、TVBox、影视仓里用。

[演示站点](https://eco.fe-spark.cn) · [管理后台](https://eco.fe-spark.cn/manage) · [部署指南](./docs/README-Deploy.md) · [常见问题](./docs/README-FAQ.md)

</div>

## ⚠️ 使用前请读

- **仅供学习交流**。本项目不提供、不存储任何影视资源，片源来自你自己配置的采集接口。
- **合规使用**。请遵守所在地区法律法规，以及各采集源的使用约定。由此产生的风险由使用者自行承担。
- **欢迎 Star**。觉得好用请点个 ⭐，这是对项目最大的支持。

## ❤️ 推荐

### 服务器

[CloudCone](https://app.cloudcone.com/?ref=14393) — 演示站用的就是这家。**不限制 IO**（不少便宜 VPS 会卡 IO，跑数据库和采集容易卡死）。海外机器最大的好处是**免备案、拿到就能用**，部署爬虫、做这种站很顺手，顺便还能搭个自己用的代理。注册与购买可使用邀请链接支持此项目。

### 机场（科学上网）

部署海外服务器、采集源、调试接口时，稳定的代理几乎是刚需。这里推荐目前性价比极高的直连机场 **良心云**：

- **2 元 / 月 100G**，6 元直接 1000G（1T），真正的价格屠夫
- 全直连 AWS + 甲骨文高配机器，VLESS Reality + Hysteria2 双协议
- 全节点解锁 Netflix / Disney+ / TikTok / ChatGPT，无审计、1 倍流量
- 支持新疆、河南、福建等特殊地区，晚高峰也能看 4K
- 新用户注册即送体验流量

👉 专属优惠注册链接（支持本项目）：[注册良心云](https://xn--9kqz23b19z.com/#/register?code=xAmvfdic)

> 个人实测稳定，适合当主力备用梯 + 大流量下载，可每月购买防跑路。

## ✨ 它能干什么

| 你想做的事 | EcoHub 怎么帮你 |
| --- | --- |
| 给自己 / 家人搭一个影视站 | 浏览器打开就能搜片、分类、在线播放 |
| 电视、盒子里看 | 把一条配置地址贴进 **TVBox / 影视仓** 就能用 |
| 多个网站的片子合到一起 | **主站**定片库（有哪些片子、分类、简介），**附属站**给同一部片子补播放线路 |
| 不想天天手动更新 | 打开计划任务，定时自动采集 |
| 知道哪部剧更新了 | 可接 **Telegram**，采集结果推到手机 |
| 改首页、轮播、分类 | 全部在网页后台点，不用改代码 |

> 第一次启动已经内置一批采集源。你要做的主要是：**启用 → 采集 → 等它跑完 → 开始看**。

## 📺 先看演示

不想先装的，可以直接打开演示站摸摸手感：

| 入口 | 地址 |
| --- | --- |
| 前台（看片） | [https://eco.fe-spark.cn](https://eco.fe-spark.cn) |
| 管理后台 | [https://eco.fe-spark.cn/manage](https://eco.fe-spark.cn/manage) |
| 演示账号 | `guest` / `guest`（只读，改不了配置） |

当前正式版 **v2.1** 是单个 Docker 镜像 `ghcr.io/fe-spark/ecohub`，网站和管理后台装在一起。版本记录见 [RELEASE.md](./docs/RELEASE.md)。

## 🚀 五分钟装好

机器上先装好 [Docker](https://docs.docker.com/get-docker/)（20+）和 Docker Compose（2+）。建议 **2 核 2G** 起步。

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
cd ~/ecohub
# 打开 .env，至少改掉 JWT_SECRET 和数据库密码
docker compose up -d
```

装好后用浏览器打开：

| 地址 | 干什么 |
| --- | --- |
| `http://服务器IP:3000` | 前台看片 |
| `http://服务器IP:3000/manage` | 管理后台 |
| `http://服务器IP:3000/api/provide/config` | 给 TVBox / 影视仓 用的配置 |

默认账号（**对外必须马上改密码**）：

| 谁 | 账号 | 密码 | 能做什么 |
| --- | --- | --- | --- |
| 管理员 | `admin` | `admin` | 改配置、采集、管影片 |
| 访客 | `guest` | `guest` | 只能看，不能改 |

> 用 1Panel 图形界面装、换自己的数据库、配域名反代：见 [部署指南](./docs/README-Deploy.md)。

刚装完前台没有片子，这是正常的。进后台 **采集中心** 跑第一次全量采集，**可能要数个小时**；采完并发布后，前台才会出片。后台登录后有新手引导。

## 📱 用在什么地方

装好的是你自己的站点，下面这些地方都能接：

### 浏览器

电脑、手机浏览器直接打开站点地址就能看。适合家人分享一个网址。

### TVBox / 影视仓（电视、盒子）

1. 打开 TVBox 或影视仓的「配置 / 订阅」
2. 填入（把 IP 换成你的）：

```text
http://服务器IP:3000/api/provide/config
```

3. 保存后刷新，列表里会出现 EcoHub 源，搜索、分类、筛选和网站上是一套的

有域名并配好 HTTPS 时，把上面的 `http://服务器IP:3000` 换成你的域名即可。

### 其它兼容客户端

MacCMS 格式的接口也可以直接用：

```text
http://服务器IP:3000/api/provide/vod
```

适合已经在用这类接口的播放器或自己写的小工具。

### Telegram（可选）

想在手机上收到「采集完了 / 哪部剧更新了」：到后台 **系统设置 · 通知配置** 填 Bot Token 和 Chat ID。国内服务器访问 Telegram 经常超时，Docker 里可在 `.env` 加：

```env
TG_PROXY=http://host.docker.internal:7890
```

把端口换成你本机代理的端口。

## 🛠️ 本地开发

改代码、自己编译时再用这一段。普通自用直接走上面的 Docker 即可。

1. 本机先准备好 MySQL 8 和 Redis 7。
2. 启动后端：

```bash
cd server
cp .env.example .env
go run ./cmd/server
```

3. 启动前端：

```bash
cd web
cp .env.example .env.local
npm install
npm run dev
```

4. 浏览器打开：

- 前台：`http://127.0.0.1:3000`
- 后台：`http://127.0.0.1:3000/manage`
- 后端：`http://127.0.0.1:8080`

环境变量和更细的说明：

- [服务端说明](./server/README.md)
- [前端说明](./web/README.md)

## 📚 更多文档

| 文档 | 什么时候看 |
| --- | --- |
| [部署指南](./docs/README-Deploy.md) | 安装脚本、1Panel、源码版、反代 |
| [常见问题](./docs/README-FAQ.md) | 看不到片子、采集、缓存、登录、Docker |
| [版本说明](./docs/RELEASE.md) | 升级注意、镜像 tag |
| [服务端说明](./server/README.md) | 环境变量、接口、鉴权（开发用） |
| [前端说明](./web/README.md) | 页面和本地启动（开发用） |
| [English README](./docs/README_EN.md) | English docs |

## 常见卡点

| 现象 | 先检查 |
| --- | --- |
| 网站能开，但一部片都没有 | 第一次全量采集可能要数小时。没采完、没发布前，前台不会出片。去采集中心看进度 |
| 采集一直失败 | 源站地址失效、服务器访问不了源站（海外机 / 代理） |
| 后台能进，改不了东西 | 是不是用了 `guest`？访客只能看 |
| 电视上搜不到 | 配置地址是否写成了 `.../api/provide/config`，以及采集是否已经完成 |

更多排障见 [README-FAQ.md](./docs/README-FAQ.md)。

## Star History

<a href="https://star-history.dera.page/#fe-spark/EcoHub&Date">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://star-history.dera.page/svg?repos=fe-spark/EcoHub&type=Date&theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://star-history.dera.page/svg?repos=fe-spark/EcoHub&type=Date" />
    <img alt="Star History Chart" src="https://star-history.dera.page/svg?repos=fe-spark/EcoHub&type=Date" />
  </picture>
</a>

---

<div align="center">

MIT License · [fe-spark/EcoHub](https://github.com/fe-spark/EcoHub)

如果这个项目帮到你，欢迎点个 ⭐ Star。

</div>
