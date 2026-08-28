测试版 **v2.4.0-beta.3**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.4.0-beta.3`。

### 升级指引

- **从已有版本升级**：拉取镜像 `ghcr.io/fe-spark/ecohub:v2.4.0-beta.3` 或配置 docker-compose 测试升级。本版本为测试版，不会覆盖 `:latest`。

---

### v2.4.0-beta.3 核心变更

1. **大盘 Bento Grid 错落有致排版**：
   - 告别单调对称布局，重构为动态交错网格：24 小时走势 (62%) + 终端分布 (38%)、行为分布 (38%) + 响应耗时分布 (62%)、热门点播与热搜 1:1 双榜单及全宽访问日志。
2. **APM 工业级耗时散点图 (`ScatterChart`)**：
   - 引入毫秒级真实请求时间散点映射，毫秒级毛刺与离群慢请求一目了然；支持 `Radio.Group` 单选按键在「散点分布」与「柱状梯队」之间自由切换。
3. **全端点播与 TVBox/App 归总全链路统计**：
   - 终端设备统一将 iOS、Android、HarmonyOS 归总为 `App 客户端`；
   - TVBox 电视点播拉流与 App 直调 API 全链路提取影片 ID 计入「热门点播 TOP 10」排行榜并关联 MySQL 真实片名与海报封面；
   - 大盘点播量聚合 Web/App 点播与 TVBox 拉流总和。
4. **自适应 1:1 无变形渲染 (`ResizeObserver`)**：
   - 彻底移除硬编码 SVG 宽高与强制拉伸，全图表智能像素自适应。
5. **全局浅色主题 Token 对比度与管理端组件视觉规范化**：
   - 重构全局浅色 `colorBorder`、`colorBorderSecondary` 与 `colorFillQuaternary` / `colorFillTertiary`，彻底解决各小模块白底融合失焦问题；
   - 规范化「分类管理」页面，去除强制发黄背景与标签过载，还原克制高级的 AntD 标准界面。
