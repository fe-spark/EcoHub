测试版 **v2.1.2-beta.1**，镜像 `ghcr.io/fe-spark/ecohub:v2.1.2-beta.1`，**不会**覆盖 `:latest`。

已部署 **v2.1.0** 及以上：把 compose 镜像改成 `ghcr.io/fe-spark/ecohub:v2.1.2-beta.1` 后执行 `docker compose pull && docker compose up -d`。正式版后台不会把 beta 当作可升级版本。

从 **v2.1.0 之前** 升级的，请先按 [v2.1.0 说明](https://github.com/fe-spark/EcoHub/releases/tag/v2.1.0) 做完素材卷迁移，再升到本版本。

### 本版本变更

- 每日更新接口：不传 `limit` 时返回近 24h 全部条目（原先默认随机抽 6 条）；传 `limit` 仍随机抽取
- 仓库纳入鸿蒙客户端 [EcoHarmony](https://github.com/fe-spark/EcoHarmony) 作为 submodule（`harmony/`）
- 文档整理：部署 / FAQ / README 收入 `docs/`，并补英文版
