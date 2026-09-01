测试版（Pre-release），Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.0-beta.16`。

### 核心变更

#### 1. 前台 SSR 渲染类型安全与异常防御修复
- **影片卡片年份防御**：修复 `FilmList` 组件中 `buildFilmMetaTags` 在部分接口返回 `number` 类型年份时调用 `.slice()` 引发未捕获 `TypeError` 导致 Next.js SSR 500 崩溃的致命问题。
- **首页焦点图辅助函数简化与类型加固**：重构并简化 `HomeHero` 焦点图中的画质识别（`resolveQualityBadge`）、类型标签解析（`parseClassTags`）与剧情简介提取（`resolveHeroSummary`）逻辑，全面防御空值与非字符串数据类型。

#### 2. 管理后台轮播表单健壮性提升
- **轮播提交参数防御**：轮播新增与编辑弹窗在构建提交载荷时增加强类型转换，防止异常输入导致提交失败。

#### 3. 系统启动与快照稳定性优化
- **启动缓存清理策略优化**：在服务冷启动阶段明确保留当前活跃快照与构建快照版本号，避免版本标记丢失导致全站兜底重算与启动耗时突增。
