测试版 **v2.6.0-beta.1**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.6.0-beta.1`。

### 升级指引

- **从已有版本升级**：
  - **1Panel / Compose**：执行 `docker compose pull ecohub && docker compose up -d ecohub` 即可（或后台「检查更新」一键平滑升级）。
  - **数据结构与兼容性**：完全向下兼容现有 MySQL 与 Redis 数据结构，无破坏性变更。

---

### v2.6.0-beta.1 核心变更

#### 1. 判更机制彻底回归主分支纯粹基准
- **移除 Remarks 文本判更与过度设计**：
  - 彻底移除 `masterShouldBumpStamp` 及其引发线上存量老片更新时间戳冲高的隐患；
  - 坚决杜绝任何基于备注文案（Remarks）的字符串猜测（无 `Contains("完结")` / `Contains("HD")` 等脆弱启发式规则）；
  - 判定标准 100% 严格回归主分支最纯粹法则：**有且仅当真实分集数严格增长（`isEpisodeCountHigher`）** 时才允许刷新 `update_stamp`，其余情况（包括纯备注文案更迭、老片全量重爬）一律完整继承库内历史 `update_stamp`；
  - 彻底解决线上因历史数据缺乏备注字段导致全量老片霸榜 `/filmClassifySearch`（最近更新）的排序错乱问题。

#### 2. 系统设置统一整合与超级管理员权限收敛
- **系统设置标签页整合**：将分散的页面（通知配置、数据安全、运行日志）收归统一的「系统设置」二级 Tab 视图，清理历史遗留冗余路由，界面结构更为紧凑有序；
- **全链路超级管理员鉴权加固**：
  - 通知配置查询/保存/测试（`/api/manage/config/notify*`）、运行日志流式查询（`/api/manage/system/logs/delta`）全面接入 `middleware.AdminAccess()` 路由拦截器；
  - 各 Handler 内部增加 `model.IsAdmin` 防御性二次校验（Defense-in-depth）；
  - Next.js 服务端页面（`/manage/system/page.tsx`）与客户端菜单布局联动校验，未授权账号自动重定向，彻底杜绝越权访问与信息泄露。

#### 3. 数据安全模块极简体验重构
- **极简数据清理交互**：
  - 数据安全中的「数据分析清理」彻底移除繁冗的技术指标与数据描述面板，结构对齐「数据重置」卡片，保持极致简洁；
  - 彻底剔除底层开发术语，全面换用面向用户的直白语言；
  - 支持按天数保留（7/14/30天）或全部清空，校验管理密码安全执行。

#### 4. 读模型与数据库跨方言高可用加固
- **分类与读模型性能优化**：延迟关联（Deferred Join）与快照直查优化，快照搜索索引字段长度对齐；
- **跨数据库方言完全兼容**：将标签聚合保存调整为标准 `clause.AssignmentColumns`，使代码在 MySQL 与 SQLite 测试环境下均能无缝兼容。

#### 5. 质量与回归验证
- **全仓测试**：`go test -count=1 ./...` 后端全模块所有 16 个包 100% PASS 通过；
- **前端构建**：Next.js 生产编译与页面优化 `npm run build` 0 错误顺利通过。
