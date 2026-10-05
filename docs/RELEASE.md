预发布版 **v2.7.9-beta.0**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.7.9-beta.0`。不会覆盖 `latest`。

### 升级指引

- **试用本版**：把 Compose 里的镜像改为 `ghcr.io/fe-spark/ecohub:v2.7.9-beta.0`，然后执行 `docker compose pull ecohub && docker compose up -d ecohub`。
- **数据兼容**：全自动平滑升级，无需手动执行 SQL 或重新初始化配置。稳定版的「检查更新」不会升到这个预发布版。

---

### v2.7.9-beta.0 核心变更

- **分类同步**：源站已给出 `type_pid` 时保持原层级，不再按名称改挂。修复新增或切换主站后父子分类保存失败、前台分类为空的问题。
