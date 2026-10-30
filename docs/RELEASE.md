预发布版 **v3.0.0-beta.20**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v3.0.0-beta.20`。不会覆盖 `latest`。

### 升级指引

- **试用本版**：把 Compose 镜像版本指定为 `ghcr.io/fe-spark/ecohub:v3.0.0-beta.20`，然后执行 `docker compose pull ecohub && docker compose up -d ecohub`。
- **破坏性变更与数据兼容**：**不兼容，不迁移。** 本版本为 v3.0 重大架构重构版本，完全不向后兼容 v2.x 历史数据。已入库的播放列表也不会改写；重复选集要重新采集对应站点才会拆开。请勿直接挂载 v2.x 历史数据库升级。

---

### v3.0.0-beta.20 核心变更

- **测试环境彻底解耦 CGO 与纯 Go SQLite 迁移**：
  - 测试驱动迁移：全面将单元测试与集成测试中的 SQLite 驱动从 `gorm.io/driver/sqlite`（绑定 CGO 版 `go-sqlite3`）迁移至纯 Go 实现的 `github.com/glebarez/sqlite`；
  - 零 CGO 编译与 CI 就绪：彻底消除非 CGO 环境下测试报错 Stub 的问题，单测与集成测试在 `CGO_ENABLED=0` 与 `CGO_ENABLED=1` 下均 100% 通过；
  - 新增 CI 自动化工作流：添加 GitHub Actions 流水线，在推送与 PR 时自动校验 `CGO_ENABLED=0` 下的后端测试与编译构建。
- **核心依赖升级与依赖链对齐**：
  - Gin 框架升级至 `v1.12.0`；
  - GORM 升级至 `v1.31.2`，MySQL 驱动升级至 `v1.6.0`，底层驱动 `go-sql-driver/mysql` 升级至 `v1.8.1`；
  - 彻底清理废弃的 `mattn/go-sqlite3` 间接依赖，精简依赖图。
- **检索内存索引倒排增量修剪缺陷修复**：
  - 修复 `cloneAndPatchSearchPostings` 在增量更新既有影片时重复追加倒排记录、导致切片乱序与二分查找命中失效的缺陷；
  - 变更记录未改动检索名称时复用既有倒排指针，杜绝索引膨胀与搜索结果条数统计异常。
