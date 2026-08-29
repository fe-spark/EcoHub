测试版 **v2.4.0-beta.6**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.4.0-beta.6`。

### 升级指引

- **从已有版本升级**：拉取镜像 `ghcr.io/fe-spark/ecohub:v2.4.0-beta.6` 或配置 docker-compose 测试升级。本版本为测试版，不会覆盖 `:latest`。

---

### 核心变更

1. **访问分析耗时散点图**
   - 去掉强制拉伸，圆点保持正圆；图表仍按容器宽度铺满。
