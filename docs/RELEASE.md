预发布版 **v3.0.0-beta.18**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v3.0.0-beta.18`。不会覆盖 `latest`。

### 升级指引

- **试用本版**：把 Compose 镜像版本指定为 `ghcr.io/fe-spark/ecohub:v3.0.0-beta.18`，然后执行 `docker compose pull ecohub && docker compose up -d ecohub`。
- **破坏性变更与数据兼容**：**不兼容，不迁移。** 本版本为 v3.0 重大架构重构版本，完全不向后兼容 v2.x 历史数据。已入库的播放列表也不会改写；重复选集要重新采集对应站点才会拆开。请勿直接挂载 v2.x 历史数据库升级。

---

### v3.0.0-beta.18 核心变更

- **站点配置导入与采集站分类树自动同步联动**：
  - 导入站点备份配置启用「采集站」模块时，自动触发异步全量采集站分类树拉取同步（`SpiderSvc.SyncAllSourceCategories`）；
  - 彻底根除导入采集站后分类树未就绪需手动同步的问题，形成站点配置迁移与分类数据就绪的闭环；
  - 完善配置导入联动分类同步的单元测试，保障配置恢复链路健壮性。
