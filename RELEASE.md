测试版 **v2.1.2-beta.4**，镜像 `ghcr.io/fe-spark/ecohub:v2.1.2-beta.4`，**不会**覆盖 `:latest`。

已部署 **v2.1.0** 及以上：把 compose 镜像改成 `ghcr.io/fe-spark/ecohub:v2.1.2-beta.4` 后执行 `docker compose pull && docker compose up -d`。正式版后台不会把 beta 当作可升级版本。

从 **v2.1.0 之前** 升级的，请先按 [v2.1.0 说明](https://github.com/fe-spark/EcoHub/releases/tag/v2.1.0) 做完素材卷迁移，再升到本版本。

### 本版本变更

- **彻底重构读模型与查询架构**：废除 10 万+ 快照宽表全量内存常驻读模型（ActiveReadModel），常驻内存占用从 ~3GB 骤降至 ~30MB，彻底根除高数据量下的 OOM Killer (SIGKILL) 崩溃重启。
- **相关推荐毫秒级优化**：废除 4 万条全分类 $O(N)$ 遍历打分机制，引入同系列/同细类/核心词漏斗候选精准召回与 Redis 二级缓存，相关推荐耗时从 75 秒大幅缩减至 <10ms（缓存命中 <1ms）。
- **分类与多维筛选直查**：分类列表、热播与多维标签筛选全部改由 MySQL 联合索引直接分页输出，消除全量大切片内存分配与 GC 压力。
- **安全与性能增强**：搜索与筛选引入 SQL LIKE 通配符安全转义（`escapeLikePattern`），标签聚合支持流式分批扫描（FindInBatches），全链路补充结构化观测日志。

