预发布版 **v2.5.5-beta.3**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.5-beta.3`。

### 升级指引

- **从已有版本升级**：执行 `docker compose pull && docker compose up -d` 即可（或后台「检查更新」一键平滑升级）。
- **兼容性说明**：本版本完全向下兼容现有 MySQL 与 Redis 数据结构，无破坏性变更。

---

### v2.5.5-beta.3 核心变更

#### 1. Web / App / TVBox 高频分类 TOP 10 物理隔离与按端精准统计
- **分端独立 Redis 聚合**：新增 `web:top:classify`、`app:all:top:classify`、`app:{platform}:top:classify` 与 `tvbox:top:classify`，彻底解决各端分类榜单相互冲刷污染问题，并保持全局 ZSet 双写兜底。
- **离线快照与查询路由对齐**：每日 Rollup 定时落库支持 `web_classify`、`app_classify`、`{platform}_classify` 与 `tvbox_classify` 快照；`QueryTopsScope` 支持实时与历史按端按平台精准路由。
- **管理后台多端看板视图适配**：Web 独立视图、移动 App 视图与 TVBox 视图分别请求对应端的高频分类接口，端侧分类流量分布清晰呈现。

#### 2. TVBox 概览关键统计与调用分布大盘对齐
- **影视点播量与寻片搜索量**：优先读取全天概览 Action 真实汇总数据（`overview.action.play` / `search`），不再以局部 TOP 10 截断求和，保障大盘数据真实准确。
- **调用构成比例优化**：优先基于全天概览汇总计算各类别调用量，无概览时平滑回退至实时流水采样。

#### 3. 接口访问审计与缓存高并发健壮性加固
- **批量落库重试脏主键防护**：后台批量写日志重试时自动重置对象主键 `ID = 0`，防止 GORM 回填主键后在重试时引发 Duplicate Key 冲突。
- **影视元数据缓存硬顶防泄漏**：淘汰过期项后若容量仍大于 5000，触发硬顶重建重置，彻底防止突发请求下的无界内存增长。
- **埋点上报降级与前端展示健壮性**：`trackPageView` 识别 `sendBeacon` 队列状态，满载时自动降级走 `fetch` 保活上报；系统日志错误率补齐 NaN 兜底保护。
- **寻片搜索跳转参数对齐**：管理后台流水跳转公共搜索页参数由 `keyword` 修正为 `search`，确保带词检索即时生效。
