测试版 **v2.4.0-beta.5**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.4.0-beta.5`。

### 升级指引

- **从已有版本升级**：拉取镜像 `ghcr.io/fe-spark/ecohub:v2.4.0-beta.5` 或配置 docker-compose 测试升级。本版本为测试版，不会覆盖 `:latest`。

---

### 核心变更

1. **访问分析耗时散点图**
   - Y 轴改为对数刻度（10ms → 50ms → 200ms → 500ms → 1s），慢请求不再把正常点压在底边。
   - 图表宽度撑满卡片，不再左右留白。
