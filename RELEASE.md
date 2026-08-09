# v1.1.5-beta.15

> **预发布（prerelease）**：前台视觉与主题细节优化、登录页亮色模式支持、影视卡片无图占位兜底与系统设置页滚动布局收口，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.15`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.15`

## 相对 beta.14

- **登录页亮色模式支持**：登录页由固定暗色改为跟随全局主题（亮/暗），亮色下提供渐变背景、白底卡片与表单样式，并修复浏览器自动填充导致的输入框黑底问题
- **前台视觉与主题细节优化**：
  - 亮色主题文字色阶加深（`--public-text-2~6`），提升正文/辅助文字对比度；搜索框 placeholder 颜色同步修正
  - Ant Design Card 亮/暗显式背景、边框与阴影规则，亮色下卡片更清爽、暗色下更通透
  - 全局亮色 token 微调（`colorBgLayout`、`colorBorderSecondary`、Card 边框色）
- **影视卡片列表升级**：
  - 无封面/封面加载失败时展示占位兜底（图标 + 片名），不再留空图裂
  - 卡片标签组改为深色毛玻璃风格，年份徽标橙色渐变高亮，亮色主题下保持高对比
  - 悬停主色光晕与扫光 shimmer 细节优化
- **分类筛选激活态**：激活 Tab 改为橙色渐变（`#fa8c16 → #fa541c`）并带光晕，与全局主色观感统一
- **系统设置页滚动布局收口**：各 Tab 内容区独立滚动（`tabPaneScrollable`），系统设置页整体高度透传，日志终端不再撑出外层页面滚动条
- **系统日志终端样式**：深色终端风格升级（边框、内阴影、字号与行距微调）

## 部署（beta.15）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.15
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.15
docker compose pull && docker compose up -d
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.14

> **预发布（prerelease）**：系统设置 UI 风格统一与重构、多余 Alert 提示精简、通知事件规则网格自适应等高对齐与全屏日志高度透传，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.14`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.14`

## 相对 beta.13

- **系统设置 UI 风格统一与样式细节优化**：
  - 收拢为统一 4 主 Tabs 结构（网站配置、通知配置、数据安全、系统日志），移除了侧边栏多余菜单项
  - 统一所有卡片的头部 Icon 规范、Title 格式、`12px` 容器圆角与 `52px` 按钮垂直居中内边距
  - 彻底清除多余堆叠的 Alert 提示框，将保存、刷新、还原默认、添加封面等主操作按钮统一收拢至 Card 头部右上角
  - 修复说明文字换行受限问题，取消 `max-width: 640px` 限制，支持宽屏下单行自然展开
  - 重构通知配置事件订阅网格，采用自适应响应式布局 (`minmax(320px, 1fr)`) 与 94px 强制像素级等高对齐
  - 全面贯通系统日志 Flex 高度透传，实现 Log 终端充满 100% 页面剩余可见高度且零外层页面滚动条

## 部署（beta.14）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.14
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.14
docker compose pull && docker compose up -d
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.13


> **预发布（prerelease）**：用户角色权限管理与账号体系升级、侧栏菜单与导航文案规范统一，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.13`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.13`

## 相对 beta.12

- **用户角色权限管理与账号体系升级**：
  - 服务端支持用户角色定义（`role`: 0 普通用户 / 1 超级管理员 / 2 访客只读），完善账号创建、编辑与权限校验规则
  - 特殊账号保护：系统预置 `admin` 与 `visitor` 账号防越权禁用或删除
  - 前端「账号管理」页面重构：搜索栏整合数据量统计、模态框新增角色设置与表单额外提示、删除操作增加危险二次确认
- **侧边栏与导航文案规范统一**：
  - 侧边栏菜单与工作台快捷入口统一命名规范（如「采集站」→「采集中心」、「图片素材」→「素材中心」、「用户管理」→「账号管理」）

## 部署（beta.13）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.13
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.13
docker compose pull && docker compose up -d
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.12

> **预发布（prerelease）**：系统设置 UI 架构重构统一、配置备份导入导出与站点展示配置收敛，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.12`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.12`

## 相对 beta.11

- **系统设置页面 UI 重构与统一**：
  - 侧栏「系统设置」下收敛为统一 Tabs 页面（`/manage/system`），支持 URL Query（`?tab=website|notify|security|logs`）持久化与深层链接：
    - **网站配置**（`website`）：融合网站基本信息与首页封面管理，移除原二级独立封面页面与冗余跳转
    - **通知配置**（`notify`）：Telegram Bot 通知与消息测试配置
    - **数据安全**（`security`）：全新数据安全模块，合并配置备份与数据重置
    - **系统日志**（`logs`）：系统日志与采集操作记录
  - 统一操作按钮规范与头部描述，规范无框样式与响应式网格布局
- **站点配置备份导入/导出**：
  - 新增配置备份能力（`GET /manage/system/backup/export`、`POST /manage/system/backup/import`），支持一键导出包含网站配置、采集站、定时任务、首页封面、通知配置与映射规则的 JSON 备份文件（不含影视库存与账号密码）
  - 导入支持**按模块勾选恢复**与**管理密码二次验证**
  - 导入采集站或定时任务时自动安全停止正在运行的采集任务并清理限流器
- **站点展示配置与初始化收敛**：
  - 统一网站基本信息与首页封面的初始化逻辑，默认封面保持为空（由后台维护）
  - 重置网站基本信息时同步还原默认展示配置并清空首页封面
- **数据重置模块整合**：
  - 「数据重置」入口统一归入「系统设置 → 数据安全」选项卡下，不再作为独立菜单分散放置

## 部署（beta.12）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.12
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.12
docker compose pull && docker compose up -d
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.11

> **预发布（prerelease）**：采集站管理交互收敛、批量总进度与 2C2G 采集默认提速，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.11`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.11`

## 相对 beta.10

- **采集站页交互收敛**：
  - 去掉页头常驻「终止全部 / 清理失效源 / 新增」按钮堆叠
  - 批量采集启动后出现**总进度条**，进行中可终止任务；总进度文案按阶段展示（采集中 / 收尾 / 完成），不再显示无意义的 `0/N 站`
  - 工具栏批量操作顺序：批量采集 → 批量启用 → 批量禁用 → 批量删除（标准按钮样式）
  - 「清理失效采集源 / 新增采集站」改为工具栏次级文字链接；附属站网格末尾虚线瓦片新增
- **采集站数量上限 12**：前后端一致校验；达上限隐藏新增入口
- **批量删除**：勾选后一键删除；主站与采集中站点自动跳过并提示原因
- **采集默认提速（2C2G 速度档）**：
  - 写阀默认：inflight 3 / 24 页/秒 / burst 8 / 单站队列 48 / 全局 144
  - 页并发：`COLLECT_PAGE_WORKERS=6`，单站 solo 时 `10` 吃满写阀
  - 站点并发：默认同时跑 6 站（`COLLECT_SOURCE_CONCURRENCY`，0=不限制）；主站优先派发
  - 默认请求间隔 100ms
- **收尾进度不误超时**：`page_done` / `waiting_publish` / `finalizing` 不再按单站 stale 标 failed
- **移除** `diagnose-notify` 诊断命令（脚本仍在 `server/scripts/`，需时可自行恢复）

## 部署（beta.11）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.11
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.11
docker compose pull && docker compose up -d
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.10

> **预发布（prerelease）**：采集站管理 UI 重构、进度生命周期与失效源清理，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.10`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.10`

## 相对 beta.9

- **采集站管理页重构**：表格改为主采集站横条 + 附属采集站卡片网格；启用/禁用用左边条与状态胶囊区分；同排卡片等高、底部操作沉底对齐
- **主站 / 附属站分类展示**：主采集站单卡操作条，附属采集站多卡网格；全局操作（清理失效源、终止全部、新增采集站）上移页头，工具栏仅保留批量选择操作
- **采集进度体验**：
  - 点击开始采集立即展示 **0% / 排队中**（单站与批量均同步标记 `starting`，前端乐观更新）
  - 终态进度（done/failed/stopped）在仍有活跃采集时全部保留，**全部结束后统一消失**，避免部分卡片进度条先没
  - 进度区恢复阶段文案：等待收尾 / 收尾发布中 等（`resolveCollectStatusText`）
- **失效源清理**：批量检测采集站接口连通性，弹窗确认后批量删除失效源
- **文案统一**：采集相关 UI 统一为「采集站 / 主采集站 / 附属采集站」；侧栏菜单采集中心排在内容管理之前
- **工作台**：运行概览 / 当前主采集站移至工作台展示（轻量轮询），采集站页专注列表与批量操作
- **初始化**：移除首页封面/轮播示例预设数据，新环境不再自动写入

## 修复

- 批量采集启动后进度条迟迟不出现（改为同步 `PrepareBatchCollectStart`）
- 失败站进度单独超时消失导致网格参差、有的有进度有的没有
- 卡片进度区缺失「等待收尾 / 收尾发布中」阶段展示
- 清理失效源 loading 使用 antd 6 废弃的 `tip`，改为 `description`

## 部署（beta.10）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.10
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.10
docker compose pull && docker compose up -d
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.9

> **预发布（prerelease）**：数据重置重构（异步 + 真实进度 + 独立入口 + 影响面统计），**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.9`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.9`

## 相对 beta.8

- **数据重置重构**：
  - 重置改为**异步执行**：点击确认后立即返回「数据重置已发起」，后台真实执行；前端轮询进度接口，按**关键节点**展示真实进度（停止任务 → 清空影视库存 → 清空派生数据 → 清空分类映射 → 清理缓存），失败时透出真实错误
  - 重置范围收敛为纯采集数据：不再重建分类（分类属采集数据，全量采集时自动从主站同步，消除外部网络依赖）、不再重置任何配置类数据（网站配置、轮播、映射规则、采集源、定时任务）与账号
  - 修复清表后采集统计内存缓冲回写旧数据（`ResetCollectStatsCoalescer`）
  - 入口迁移为侧边栏**独立顶级菜单「数据重置」**（红色警示样式），页面提供**影响面统计**（影视库存 / 列表快照 / 分类 / 失败记录），重置完成后自动刷新归零
- **采集站点列表**：各列设置合理宽度，站点 URI 超长截断 + 悬停查看完整地址
- **数据重置进度/统计接口**：`GET /manage/spider/clear/progress`、`GET /manage/spider/clear/stats`

## 修复

- 数据重置清空影视数据后，内存采集统计缓冲可能回写旧 `last_collect_time`
- 数据重置不再因主站分类树拉取失败而受影响（分类重建改由全量采集自动完成）

## 部署（beta.9）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.9
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.9
docker compose pull && docker compose up -d
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.8

> **预发布（prerelease）**：更新列表防抖与共享匹配键去噪，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.8`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.8`

## 相对 beta.7

- **附属站共享匹配键去噪**：同一影片多条目共享匹配键（如「XXX英语」「XXX国语」同豆瓣 ID）按落库后写覆盖去重，多条目并存不再误判为结构变化；某 key 本次无内容（源站改名/条目消失残留）不进列表
- **防源站剧集抖动**：更新列表仅计「新增集数/新增线路」（集数标签无序多重集比较）；集数相同（仅链接/顺序变化）不通知；集数回退（源站/CDN 返回不稳定，如 16↔18）不通知且不覆盖已存内容，DB 保持已见最大集数，杜绝连载剧发布期同一 mid 每批反复上报；主站写路径同步保护
- **批次回调错误分类**：批次加载区分不存在/过期/为空，回调提示不再一律「列表已过期」（多实例共用 Bot Token 被另一实例消费时提示「批次不存在」）
- **诊断工具判定对齐**：`diagnose-notify` 输出区分「新增集数/线路」「集数相同（仅顺序/链接）」「集数回退（抖动）」，并按 live key 补查历史行，避免把已存在行误判为「首次写入」

## 修复

- 连续批量采集更新列表反复出现同一影片（共享匹配键多条目 + 源站剧集抖动两类根因）
- 附属站「英语/国语」等双条目共享豆瓣键导致的每批重复通知
- 连载剧发布期源站返回集数不稳定导致的震荡上报
- 更新列表回调「刚收到就过期」误提示（区分不存在/过期/为空）

## 部署（beta.8）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.8
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.8
docker compose pull && docker compose up -d
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.7

> **预发布（prerelease）**：修复连续采集更新列表反复推送相同影片，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.7`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.7`

## 相对 beta.6

- **附属站更新列表去噪**：同一 `match_key` 命中多个 mid（如「烬九州：第二季」与「烬九州第二季」标点双包）时只通知最优 mid，避免一次变更推两条近似片名
- **FirstInsert 通知**：写库前若该附属站已为该 mid 写过 playlist，仅扩展 match key 不再当「新片上线」刷屏
- **播放源摘要刷新收窄**：只刷新本批有 playlist 写入的 mid，不再把页内全部匹配片刷进 finalizer（消除「每次 mid_count=76」类误导日志与多余快照压力）
- **诊断工具**：`server/cmd/diagnose-notify`（`scripts/diagnose-notify.sh`）分析当天变更批次、库/源站结构对比与双次拉取抖动

## 修复

- 连续批量采集 TG「更新列表」反复出现同一批/近似片名影片
- 附属站 match_key 一对多 mid 导致 stamp/通知扩散
- 收尾 PlaySummary / 快照范围过宽

## 部署（beta.7）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.7
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.7
docker compose pull && docker compose up -d
```

可选：库存中标点双包片（同一季两个 `vod_*`）仍可能并存于前台，可按 `movie_match_key` 多 mid 分组人工合并；通知侧已不再双推。

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.6

> **预发布（prerelease）**：Telegram 通知健壮性与系统日志结构化升级，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.6`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.6`

## 相对 beta.5

- **Telegram Bot 轮询多实例安全**：Redis 领导锁 + getUpdates Conflict 退避，多 EcoHub 实例共享 Redis 时仅 leader 长轮询；修复并发启停的 WaitGroup 竞态（潜在进程 panic）
- **系统日志结构化级别**：级别在写入时确定（INFO/WARN/ERROR），服务端落盘打结构化标签、重启可恢复；前端不再按正文关键词猜测；控制台（stdout）保持无标签原始输出
- **批量源配置变更聚合通知**：批量启用/禁用聚合为「批量 N 个」，超长按页拆分不丢源；部分源失败仍推送已生效变更（限流按源集合指纹，不同源集合互不限流）
- **定时任务事件补齐**：模型 0/1/2 任务级 `cron_task_done` / `cron_task_failed` 全覆盖（含未配置源、类型废弃等结构性失败）
- **0 页采集进度对齐**：无新内容时批量 `waiting_publish` / 单站 `page_done`，前端显示「无新内容」而非卡「即将开始采集」
- **变更批次过期判定**：改 Go 侧比较，修复时区差异导致回调「刚收到就过期」

## 修复

- 并发调用 `EnsureBotPoller` 可能触发 `sync: WaitGroup misuse` panic（改为按代句柄等待）
- 批量启用/禁用中途失败时静默丢失已生效源的变更通知
- Telegram webhook 未清除错误被误判为多实例冲突（现保持领导权并重新清除）
- 变更批次 `expire_at` SQL 时区比较误判过期
- 0 页站点采集列表状态与生命周期不一致

## 部署（beta.6）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.6
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.6
docker compose pull && docker compose up -d
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.5

> **预发布（prerelease）**：取消启动 bulk ContentKey 迁移，改为写路径懒兼容，**不会**覆盖 `:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.5`
- `ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.5`

## 相对 beta.4 / beta.3

- **取消**启动 / 写库前 bulk 迁移 `name_*` → `vod_*`（不再卡启动、不再挡写库）
- **写路径兼容**：主站按 `mid`（= 源站 vod_id）冲突更新；重采即懒升 `content_key`（业务无变更也会升）；未再采的旧行保持 `name_*`，展示/播放不受影响
- 新片仍用 `vod_{源站id}`，避免片名误合并
- 软删占键自动释放；**活跃行**占着目标 `content_key` 时写库失败并打日志（不抢键）
- 历史误合并片需再次采集对应 vod 后才会拆成多条

## 破坏性变更（BREAKING）

### 主站 `ContentKey` 身份键

- **行为变更**：新写入优先 `vod_{源站 vod_id}`。
- **旧库存**：不改写；随采集懒升。
- **建议**：升级后正常增量即可；若曾出现片名误合并，对相关片或主站做一次全量采集。

## 修复

- 启动迁移卡死 / 迁移体验差（改为不迁旧数据）
- 主站更新列表反复推送相同影片（ContentKey 片名误合并）
- 后台文案：「恢复默认值」→「重置站点数据」
- `.env.example` / compose 对齐 Telegram 代理与采集写阀变量

## 部署（beta.5）

```bash
# compose 镜像 tag 示例（不会覆盖 :latest）：
#   image: ghcr.io/fe-spark/ecohub-server:v1.1.5-beta.5
#   image: ghcr.io/fe-spark/ecohub-web:v1.1.5-beta.5
# 可选：TELEGRAM_PROXY=http://host.docker.internal:7890
# 1. 建议先停自动/定时采集
# 2. 拉取并启动（无需清库、无需启动迁移等待）
docker compose pull && docker compose up -d
# 3. 正常增量采集即可；曾误合并的片建议主站全量采一次
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。

---

# v1.1.5-beta.4

预发布：曾用 MySQL 批量 bulk 迁移 ContentKey；**beta.5 起取消 bulk 迁移**，改写路径懒兼容。镜像：`…:v1.1.5-beta.4`。

---

# v1.1.5-beta.3

预发布：ContentKey 自动迁移与后台公告。镜像：`…:v1.1.5-beta.3`。

---

# v1.1.5-beta.1

预发布：ContentKey 改为 `vod_{vod_id}`；后台「重置站点数据」文案。镜像：`…:v1.1.5-beta.1`。

---

# v1.1.4

## 修复

- 布局级导航 loading 在同链/未进入 pending 时整页卡死（同链短路 + 路径到达 settle + 会话代次）
- Header 首页/同分类/同搜索重复点击触发无意义导航
- 前台 Pagination 大号 token 污染后台 `/manage` 分页
- `buildBackendApiUrl` 在带子路径 `API_URL` 时丢失 pathname，与 rewrite 不一致
- 分类筛选 pending 用 query 字符串全等判断，参数键序/空值不一致时卡到超时

## 优化

- 前台导航改为布局级 content loading，避免页面卸载后 transition 无法 settle
- 筛选页列表区 loading 与语义化 query 到达判定
- FilmList / Hero / 热榜统一走布局级播放跳转 loading
- SSR API 日志仅在 development 输出详情

## 新增

- `PublicContentLoading` 布局级内容加载与 `useContentNavigate`
- `SiteLogo`：未配置用本地 `/logo.png`，已配置原样加载（失败不兜底）
- `api-base`：`API_URL` 规范化（带/不带 `/api` 均可）

## 修改

- 后台站点配置初始 `logo` 置空（由前端未配置时走本地默认）
- 文档补充 Docker `API_URL` 与直连后端 `/api` 两种访问模型说明

## 部署

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
cd ~/ecohub && docker compose up -d
```

默认账号：`admin / admin`（管理员）、`guest / guest`（只读）。正式部署请修改密码与 `JWT_SECRET`。
