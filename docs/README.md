# EcoHub 文档索引 / Docs Index

文档按使用对象分为「使用与运维（`guide/`）」和「开发与架构（`dev/`）」，英文镜像统一置于 [en/](./en/README.md)。

## 项目入口 / Overview

| 主题 | 中文 | English | 说明 |
| --- | --- | --- | --- |
| 总览 | [README](../README.md) | [en/README](./en/README.md) | 项目简介、核心功能、快速开始、目录结构 |
| 版本说明 | [RELEASE](./RELEASE.md) | [en/RELEASE](./en/RELEASE.md) | 当前及历史版本变更（`docs/RELEASE.md` 用作 GitHub Release 正文） |

## 使用与运维 / User & Operations Guide

| 主题 | 中文 | English | 说明 |
| --- | --- | --- | --- |
| 部署指南 | [guide/deploy](./guide/deploy.md) | [en/guide/deploy](./en/guide/deploy.md) | 准备条件、脚本/1Panel/手动部署、访问地址与反代 |
| 运维与配置 | [guide/operations](./guide/operations.md) | [en/guide/operations](./en/guide/operations.md) | `ecohub` 命令、密码与端口规则、自定义编排、外置库、环境变量与排障 |
| 从 v2 升级到 v3 | [guide/v2-to-v3](./guide/v2-to-v3.md) | [en/guide/v2-to-v3](./en/guide/v2-to-v3.md) | 停用旧容器、空库重装与验证 |
| 常见问题 | [guide/faq](./guide/faq.md) | [en/guide/faq](./en/guide/faq.md) | 面向使用与排障的常见问题解答 |

## 开发与架构 / Developer Docs

| 主题 | 中文 | English | 说明 |
| --- | --- | --- | --- |
| 开发指南 | [dev/setup](./dev/setup.md) | [en/dev/setup](./en/dev/setup.md) | 本地环境要求与开发编排 |
| 核心机制与数据链路 | [dev/architecture](./dev/architecture.md) | [en/dev/architecture](./en/dev/architecture.md) | 流式发布、索引封口、去重匹配、缓存策略、弹幕缩放、双端鉴权 |
| 计划任务运行冲突 | [dev/cron-task-conflict](./dev/cron-task-conflict.md) | [en/dev/cron-task-conflict](./en/dev/cron-task-conflict.md) | 采集与维护任务并发裁决及维护闸门 |
| 服务端说明 | [server/README](../server/README.md) | [server/README_EN](../server/README_EN.md) | Go 后端本地启动、环境变量、启动流程、接口与目录结构 |
| 前端说明 | [web/README](../web/README.md) | [web/README_EN](../web/README_EN.md) | Next.js 本地启动、`API_URL`、鉴权与目录结构 |

## 维护规范 / Style Guide

- [文档写作规范](./STYLE.md)（仅中文）：目录组织、文风、术语表与中英文同步规则。

交流群组：[Telegram](https://t.me/ecohub_club)
