测试版 **v2.6.0-beta.5**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.6.0-beta.5`。

### 升级指引

- **从已有版本升级**：
  - **1Panel / Compose**：执行 `docker compose pull ecohub && docker compose up -d ecohub` 即可（或后台「检查更新」一键平滑升级）。
  - **数据结构与兼容性**：完全向下兼容现有 MySQL 与 Redis 数据结构，无破坏性变更。

---

### v2.6.0-beta.5 核心变更

#### 1. 采集通知列表与每日更新对齐排序
- 将通知变更批次聚合计划（`BuildCategoryPlanForMids`）中各分类及全部分页列表，与前台每日更新接口（`/api/dailyUpdates` / `/filmClassifySearch`）统一对齐为 `update_stamp DESC, mid DESC` 稳定排序；
- 彻底解决由于写库防死锁升序写入导致通知列表展示顺序与前台每日更新完全相反的问题，确保通知列表第一页展示最新更新影视。

#### 2. 精简通知列表影片展示排版
- 移除影视列表单行中尾部多余的采集源名称后缀，避免长名称或多源场景在移动端折行导致页面杂乱；
- 保持片名链接直达站内播放页，单行排版更加简洁紧凑。

#### 3. 增强无数据库环境下的系统容灾
- 分类树加载函数 `buildTreeHelper` 及 `navTopCategories` 增加 `db.Mdb == nil` 保护，彻底杜绝无 DB 场景下的空指针异常。

#### 4. 质量与回归验证
- **全仓测试**：Go 后端全模块单元测试 100% 通过（新增针对批次排序与每日更新对齐的自动化测试）；
- **前端检查**：Next.js `npx tsc --noEmit` 0 错误。

