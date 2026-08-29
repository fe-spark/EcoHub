测试版 **v2.4.0-beta.7**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.4.0-beta.7`。

### 升级指引

- **从已有版本升级**：拉取镜像 `ghcr.io/fe-spark/ecohub:v2.4.0-beta.7` 或配置 docker-compose 测试升级。本版本为测试版，不会覆盖 `:latest`。

---

### 核心变更

1. **访问分析耗时散点图**
   - 修复生产环境只显示图例、图表空白：宽度测量绑在稳定容器上，数据到达后仍能画出散点。
   - 保持正圆与铺满卡片宽度。
