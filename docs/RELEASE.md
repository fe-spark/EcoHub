测试版 **v2.1.2-beta.7**，镜像 `ghcr.io/fe-spark/ecohub:v2.1.2-beta.7`，**不会**覆盖 `:latest`。

已部署 **v2.1.0** 及以上：把 compose 镜像改成 `ghcr.io/fe-spark/ecohub:v2.1.2-beta.7` 后执行 `docker compose pull && docker compose up -d`。正式版后台不会把 beta 当作可升级版本。

从 **v2.1.0 之前** 升级的，请先按 [v2.1.0 说明](https://github.com/fe-spark/EcoHub/releases/tag/v2.1.0) 做完素材卷迁移，再升到本版本。

### 本版本变更

- **全内存极速片名搜索索引**：构建 `filmSearchMemoryIndex` 内存索引（20w 条常驻内存仅占 ~16MB），前台搜片与 TVBox/MacCMS 搜片全面接入，搜索耗时从数秒直降至 **1~2ms**。
- **每日更新架构读写分离与零查库抽取**：重构每日更新候选池，采集写路径自动补齐至 120 部，读请求纯 Redis 内存抽取并随机采样，彻底消除 MySQL 临时表全量排序与重复查库，耗时从 4.57s 降至 **< 0.2ms**。
- **相关推荐候选内存极速召回**：优化相关推荐核心片名候选集，优先内存秒级匹配，彻底移除 `sub_title` 的 TEXT 大字段慢扫描，推荐耗时从 1.43s 降至 **5ms 以内**。
- **快照状态与活跃分类索引直查**：重构 `LiveUpdateRemarksByMIDs` 与活跃分类树构建，消除大 JSON 反序列化与全表 DISTINCT 扫描，首页加载由 10.5s 降至 **秒开**。
- **Redis 批量删除优化与 Bug 修复**：优化缓存清理为 100 个 Key 批量并发删除；修复标签快照分批扫描缺少主键 `id` 导致的报错。
