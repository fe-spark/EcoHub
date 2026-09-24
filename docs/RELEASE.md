正式版 **v2.7.8**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.7.8` 与 `ghcr.io/fe-spark/ecohub:latest`。

### 升级指引

- **平滑升级**：后台「检查更新」一键升级，或执行 `docker compose pull ecohub && docker compose up -d ecohub`。
- **数据兼容**：全自动平滑升级，无需手动执行 SQL 或重新初始化配置。

---

### v2.7.8 核心变更

- **公告 Markdown**：支持编写、预览与前台渲染。
