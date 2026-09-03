# Telegram 通知系统需求文档

> 本文档依据当前代码实现（`server/internal/notify/`、`server/internal/repository/notify_repo.go`、`server/internal/service/collect_service.go`、`web/src/app/manage/system/notify/`）编写，描述通知系统的完整需求与行为。

## 1. 功能概述

EcoHub 通过 Telegram Bot 向管理员推送系统运行事件，覆盖：

- 采集执行类：批次摘要、单源失败、收尾失败、进度超时
- 定时任务类：任务失败、任务完成
- 配置管理类：采集源配置变更（新增 / 删除 / 主站切换 / 启用停用 / URI 变更等）

通知附带交互能力：摘要消息内置「更新列表」按钮可分页浏览变更影片；Bot 支持 `/search` 指令搜索影片，片名可点击跳转站内播放页。

通知为**可选功能**：总开关默认关闭，需在管理后台配置 Bot Token 与 Chat ID 后启用。

## 2. 术语

| 术语 | 说明 |
|---|---|
| 事件 | 可被订阅推送的系统行为，每个事件有独立开关（`NotifyEventSwitches`） |
| 订阅规则 | 管理后台「触发事件与订阅规则」中的事件开关，决定哪些事件推送 |
| 更新列表 | 一次采集批次中判定为「有更新」的影片列表：**必须是主站已有影片（全局 mid）**；任一播放源「最后一集」标签有变化（含新增/回退）均计入；未匹配主站的附属片不进列表 |
| 变更批次 | 一次采集的更新列表容器（MySQL `notify_change_batch` + `notify_change_mid`），48 小时有效 |
| Chat ID | Telegram 接收目标，支持数字 ID、群组/频道 `@username`、`-100xxxxxxxxxx` |

## 3. 功能需求

### 3.1 配置管理

#### 3.1.1 配置项

| 配置项 | 类型 | 默认 | 约束 | 说明 |
|---|---|---|---|---|
| `enabled` | bool | false | — | 通知总开关；关闭时所有业务推送停止（联通测试不受影响） |
| `botToken` | string | 空 | 启用时必填 | Telegram Bot Token，保存后脱敏展示 |
| `chatIds` | []string | 空 | 启用时至少 1 个 | 接收目标；格式校验（正则 `^(-?\d+|@[A-Za-z0-9_]+)$`，**仅启用时校验**，禁用状态允许保存以便清理残留）；去空/trim/去重 |
| `events.*` | 7 个 bool | 见 3.2 | — | 各事件订阅开关 |
| `includeFilmDetails` | bool | true | — | 摘要是否展示更新列表（含按钮入口） |
| `maxFilmsInMessage` | int | 15 | 1–20 | 更新列表每页最多影片数 |
| `minIntervalSec` | int | 60 | 0–3600 | 同类事件最小推送间隔（防刷限流）；0 表示不限 |

#### 3.1.2 持久化与缓存

- 配置存储于 MySQL 单行表 `notify_config`（`NotifyConfigRecord`，Payload 为 JSON），更新时整行删除重建。
- Redis 缓存 `NotifyConfigKey`（TTL 24h），读取优先 Redis，未命中回源 MySQL；Redis 反序列化失败时删除旧 key 兜底。
- 保存成功后刷新缓存并重启 Telegram 轮询（`EnsureBotPoller`，Token 变更场景）。

#### 3.1.3 Token 脱敏

- 展示：`MaskBotToken` — 短 Token 全 `*`；长 Token 显示「前 6 位 + `***` + 后 4 位」。
- 更新：`IsMaskedToken` 识别脱敏占位值，提交脱敏或空 Token 时保留旧值；否则使用新值。
- 错误脱敏：`sanitizeTelegramErr` 替换错误信息中的完整 Token、`/bot<token>/` 片段（正则 `(?i)/bot[0-9]{5,}:[A-Za-z0-9_-]+/` 替换为 `/bot***/`），并给出代理连接提示。

### 3.2 触发事件与订阅规则

共 7 个事件，均有独立开关（`NotifyEventSwitches`），任一事件推送前提为总开关 `enabled=true` 且对应事件开关开启。

| # | 事件 key | 配置字段 | 默认 | 触发时机 | 限流维度 |
|---|---|---|---|---|---|
| 1 | `collect_batch_summary` | `collectBatchSummary` | true | 整批采集结束后推送各源统计；收尾失败写入摘要 `FinalizeError` 行 | 不限流（按批次自然频率） |
| 2 | `collect_source_failed` | `collectSourceFailed` | true | 单源连续失败达到阈值被终止时即时告警。**批量采集且批次摘要开启时并入概要不单独发送**（避免逐源轰炸）；单站采集或概要关闭时保持即时告警 | `事件:sourceID` |
| 3 | `collect_finalize_failed` | `collectFinalizeFailed` | true | 快照更新/摘要刷新等收尾发布失败。**仅在摘要事件关闭时单独发送**，避免与摘要中的收尾错误双发 | `事件` |
| 4 | `collect_progress_stale` | `collectProgressStale` | true | 采集任务卡住被强制标记失败（进度超时） | `事件:sourceID` |
| 5 | `cron_task_failed` | `cronTaskFailed` | true | 后台定时任务运行失败。覆盖全部任务模型（0 自动更新 / 1 指定源 / 2 失败恢复 / 3 孤儿清理）；采集类任务的源级失败仍由批次概要承载，此处覆盖任务级结构性失败（未配置源/类型废弃等） | `事件:taskID` |
| 6 | `cron_task_done` | `cronTaskDone` | false | 定时任务成功完成（默认关闭，避免频发打扰）。覆盖模型 0/1/2（模型 3 由清理任务内部发送） | `事件:taskID` |
| 7 | `source_config_changed` | `sourceConfigChanged` | true | 采集源配置变更：编辑（等级/URI/启用停用/图片同步/名称/间隔）、新增、删除、主站切换（含被自动降级的旧主站通知）。**批量启用/禁用聚合发送「批量 N 个」**（超长按页拆分 `N 个 · i/m`，不截断丢源）；中途失败仍推送已成功变更 | 单源 `事件:sourceID`；批量 `事件:batch:<源ID集合指纹>`（不同源集合互不限流） |

**事件 7 详细触发场景（`collect_service.go`）：**

| 操作 | 变更描述（消息内） |
|---|---|
| 编辑站点 | 逐字段对比生成：`启用状态: 已启用 → 已停用`、`站点类型: 主站 → 附属站`、`接口地址已变更`、`站点名称: X → Y`、`图片同步: 开 → 关`、`请求间隔: Nms → Mms` |
| 新增附属站 | `新增采集源（附属站）`（先基于 URI 生成稳定 ID，保证限流 key 与消息 ID 完整） |
| 新增主站 | `新增采集源（主站）`；若自动降级现有主站，另向被降级旧主站单独发送 `原主站已降级为附属站，主站数据已清空` |
| 附属→主站升级 | 升级源发 `站点类型: 附属站 → 主站`；被自动降级的旧主站单独发送降级通知 |
| 删除站点 | `删除采集源` |
| 批量启用/禁用 | 聚合为 `采集源配置变更（批量 N 个）`（多页时 `批量 N 个 · i/m`），逐源列出变更；部分源失败时仍通知已成功项 |

### 3.3 消息格式

统一规范：HTML 解析模式（`parse_mode=HTML`，禁用网页预览）；标题前缀 `[EcoHub · 站点名]`（未配置站点名时 `[EcoHub]`）；普通 HTML 不用 `<pre>`（避免 Telegram 显示「复制代码」）；超长按 4096 字符拆分（优先换行处，最多 3 段，超出截断并提示）；所有用户输入经 `html.EscapeString`。

| 消息 | 模板 |
|---|---|
| 采集概要 | `<b>[EcoHub·站名] 采集概要</b>` + `⚡ 触发方式` + `⏱ 时长` + `📊 采集源：成功 N 个[，失败 M 个]` + `🎬 采集数量：更新 X 部` + `⚠️ 收尾`（如有）+ 失败源列表（仅失败时 `❌ 源名(主站|附属): 原因`）+ `📋 更新列表 N 部 · M 页`（含按钮） |
| 单源失败 | `⚠️ 采集源失败告警` + `📌 采集源` + `🚨 状态: failed` + `❌ 原因` + `🕒 时间` |
| 进度超时 | `⚠️ 采集进度超时告警` + `🚨 状态: stale` + 原因 `进度超时 status=X age=Y` |
| 收尾失败 | `⚠️ 采集收尾失败` + `📊 涉及源数` + `❌ 原因` + `🕒 时间` |
| 定时任务失败 | `🚨 定时任务失败` + `📌 任务ID` + `📝 备注` + `❌ 原因` + `🕒 时间` |
| 定时任务完成 | `✅ 定时任务完成` + `📌 任务ID` + `📝 备注` + `📋 明细` |
| 采集源配置变更 | `🔧 采集源配置变更` + `📌 站点`（含 `(<code>id</code>)`）+ `🛠 变更:` 逐条 `· ...` + `🕒 时间` |
| 测试消息 | `✅ Telegram 通知服务联通成功！` + `🕒 发送时间` |

时间统一为东八区 `YYYY-MM-DD HH:mm:ss`。

### 3.4 更新列表交互

#### 3.4.1 变更判定（只比「最后一集」）

更新列表收录 **主站已有影片** 上、**任一播放源「最后一集」有变化** 的影片（附属站用于扩展主站播放源）：

- **主站身份键**（`BuildContentKey`）：优先 `vod_{源站 vod_id}`。豆瓣/片名哈希用于附属站跨站匹配。
- **主站两层过滤**：
  1. **是否写库**（`masterBusinessSignature`）：片名（标点归一）/副标/封面 path/PlayFrom+PlayList（链接去 query）/备注/状态/演职等；排除 Hits/UpdateTime 等噪声。
  2. **是否进列表**（`filterPlayStructureNotifyMIDs`）：**新片** 或 **任一线路「最后一项分集标签」与旧数据不同**（含新增集数/新增线路/集数回退；忽略链接与中间集）。
- **附属站**（扩展主站播放源）：
  1. 必须通过 match_key **匹配到主站全局 mid**，否则不进列表。
  2. 与库中同 key 旧内容对比：**任一线路「最后一项分集标签」变化**（含新增/回退）或 **首次写入** → 进列表；**仅链接刷新/中间集变化** 不进。
  3. 每个 match_key 只通知一个最优 mid。
  4. **同一影片多条目共享匹配键**（如「XXX英语」「XXX国语」同豆瓣 ID → 同 douban 匹配键）：
     同一 `(movie_key, group_index)` 槽位按落库「后写覆盖」去重，多条目并存**不算变化**；
     仅当某 key 本次**无内容**（源站改名/条目消失的残留）时也不进列表（陈旧 key 不通知）。
- **「最后一集」定义**（`lastEpisodeLabel`）：每条线路取**源站返回顺序的最后一个非空分集标签原文**（不解析数字；macCMS 源站按集数/日期顺序返回，剧「第01集…」、综艺「第20240107期…」、电影「HD/正片」同样适用）。
- **依赖源站有序**：判定不解析数字，若源站乱序返回，顺序变化会触发更新（目前接入源均按顺序返回）。
- **回退也计入**：集数从 16 回到 10 同样算「最后一集变化」进列表（「不一样算更新」）；代价是源站/CDN 在 16↔18 间抖动时同一 mid 会随批次反复上报，属预期取舍。
- **批间不去重**：每批独立判定；各线路最后一集相同时再采不反复刷同一 mid。

#### 3.4.2 批次与明细

- 一次采集 = 一个变更批次（`ChangeBatch`，MySQL 持久化，48h TTL，全局去重）。
- 变更 mid 写入 `notify_change_mid`（`OnConflict DoNothing` 去重，记录 `created_at` 写入时间）；批次创建时清理过期批次。
- **落库与推送解耦**：总开关 / `collect_batch_summary` 关闭时仍创建批次并写变更 mid（首页「每日更新」与 TG `/daily` 依赖该表）；仅摘要推送短路，此时摘要的「变更数」回退为分源合计。
- 批次去重条数作为摘要头部「变更 N 部」与列表总条数。

#### 3.4.3 按钮与翻页

- 摘要消息尾部（开启 `includeFilmDetails` 且有变更时）附「📋 更新列表」按钮，回调 `nfp:<batchID>:open`。
- 进入列表后可翻页：`◀ 上一页` / `N / M`（点击显示"共 M 页 · N 条"）/ `下一页 ▶` / `🔙 返回概要`（还原摘要尾部段）。
- 列表按影片 `update_stamp` 新→旧排序，每页 `maxFilmsInMessage` 条（1–20）；展示序号、片名（可点击跳站内播放页，需后台配置公网网站地址，否则提示不可跳转）、`#mid`。
- 列表/搜索会话过期（批次 48h / 搜索 48h）回调提示"已过期，请重新采集/搜索"。

#### 3.4.4 Bot 指令与键盘

- Bot 注册命令 `/start`、`/daily`、`/search`、`/help`。
- `/daily`：汇总近 24 小时各批次 `notify_change_mid`（按 mid **写入时间**切窗，旧行回落批次开批时间），先出分类入口再进列表（`🔙 返回分类`；回调前缀 `ndu:`）。
- 私聊下发常驻 ReplyKeyboard（📅 每日更新 / 🔍 搜索查询 / ❓ 帮助）；群/超级群不发键盘（默认隐私模式收不到非 `/` 消息），请用斜杠命令。
- 仅通知配置中的 Chat ID 白名单可用；非白名单会话回复提示并引导加入配置。
- `/search` 关键词最多 64 字；私聊点「搜索查询」后下一条纯文本视为关键词（Redis 待命失败则回退 `/search 关键词` 提示）。
- 搜索按快照检索（最多取 200 条），结果按片名链接展示、分页浏览（回调前缀 `nsr:`）。

### 3.5 限流与防刷

- **同类事件最小间隔**：配置 `minIntervalSec`（默认 60s），按「事件:对象ID」维度限流；`0` 表示不限。
- **摘要事件**：不限流（按批次自然频率）。
- **测试发送**：最小间隔 3s（`notify:test_send`），防止管理端被当作 Telegram 代发代理刷接口。
- 限流器为进程内内存表（超过 4096 key 整表重置）。

### 3.6 安全

- **Token**：任何错误信息（发送/回调/测试）不回传完整 Token；日志不输出 Token。
- **白名单**：回调与指令均校验 Chat（数字 ID 或 `@username` 匹配配置列表）；回调缺 `Message/Chat` 直接拒绝。
- **代理**：支持 `TG_PROXY`（优先）→ `HTTPS_PROXY/HTTP_PROXY/ALL_PROXY`；协议 `http/https/socks5`；代理配置错误回退直连并打日志，不拖垮进程。
- **超时重试**：API 调用超时 45s；5xx 重试一次（间隔 400ms），429 不重试直接报错。

### 3.7 异步与容错

- 所有发布函数 `go safePublish` 异步执行，内部 recover panic；通知发送失败不影响采集主流程。
- 多 Chat 并行发送（信号量限并发）；单 Chat 失败仅记日志，不影响其他接收者。
- 发送上下文超时 30s；测试发送 35s（含代理握手）。

## 4. 非功能需求

| 项 | 要求 |
|---|---|
| 性能 | 变更批次写入为批量插入（每批 200 条）；影片明细查询按 200 条分块，不持有全量内存列表 |
| 可用性 | 通知组件初始化失败（代理无效等）自动降级，服务可正常启动 |
| 数据生命周期 | 变更批次 48h 过期自动清理（`purgeExpiredChangeBatches`，单次最多 20 轮 × 100 条）；搜索会话 Redis 48h TTL |
| 可观测性 | 关键路径日志：发送失败、限流、轮询启停、批次异常、代理选择 |

## 5. 技术架构

```
┌─ 触发源 ─────────────────────────────────────────────────┐
│ spider/notify_hook.go        service/collect_service.go  │
│  emitBatchSummaryForSources   UpdateFilmSource           │
│  emitSourceFailedNotify       SaveFilmSource             │
│  emitProgressStaleNotify      DelFilmSource              │
│  (cron 任务)                                             │
└───────────────┬──────────────────────────────────────────┘
                ▼
┌─ notify 包（异步）───────────────────────────────────────┐
│ PublishBatchSummary / PublishSourceFailed / ...          │
│  ├─ eventEnabled(总开关+子开关)  → 未开启直接返回          │
│  ├─ allowOrLog(限流)             → 被限流记录日志          │
│  ├─ format* (HTML 消息组装)                               │
│  ├─ sendMessages（并发多 Chat + 4096 拆分）                │
│  └─ telegram_client（apiCall 重试/脱敏/代理）              │
│                                                          │
│ 更新列表：StartChangeBatch → AppendMids(MySQL 去重)       │
│          → 摘要按钮 → film_page 翻页回调                   │
│ 每日更新：LoadChangeMidsBetween → /daily + 首页卡片        │
│ 轮询：EnsureBotPoller → getUpdates long-poll             │
│      → dispatchCallback（nfp / ndu / nsr）                 │
│      → handleBotMessage（/daily /search /help）            │
└──────────────────────────────────────────────────────────┘
```

数据流：采集源（spider）→ 变更判定（film 包）→ `noteCollectedMIDs` 累计 → 批次落库 → 批次摘要异步发送 → 用户按钮翻页 / `/daily` / `/search`。首页「每日更新」读同一张 mid 表，经读模型可见性过滤后展示。

## 6. 接口定义

| 接口 | 方法 | 说明 |
|---|---|---|
| `/manage/config/notify` | GET | 获取通知配置（Token 脱敏） |
| `/manage/config/notify/update` | POST | 更新通知配置（Token 合并保留、Chat ID 校验、范围校验）；成功刷新缓存并重启轮询 |
| `/manage/config/notify/test` | POST | 联通测试；可携带草稿 `botToken`/`chatIds` 不落库；返回各 Chat 发送结果（`sent` + `failed[]`） |

## 7. 数据模型

| 表 | 字段要点 | 说明 |
|---|---|---|
| `notify_config` | `payload`(JSON) | 单行配置；`NotifyConfig` 序列化（含 `events` 七开关） |
| `notify_change_batch` | `id`(16位hex) / `site_name` / `page_size` / `total` / `overview` / `created_at` / `expire_at` | 变更批次元数据，48h 过期 |
| `notify_change_mid` | `batch_id` + `mid` 复合主键 / `created_at` | 批次内变更影片 mid（全局去重）；`created_at` 供 24h 窗筛选 |

配置 JSON 结构：

```json
{
  "enabled": false,
  "botToken": "",
  "chatIds": [],
  "events": {
    "collectBatchSummary": true,
    "collectSourceFailed": true,
    "collectFinalizeFailed": true,
    "collectProgressStale": true,
    "cronTaskFailed": true,
    "cronTaskDone": false,
    "sourceConfigChanged": true
  },
  "includeFilmDetails": true,
  "maxFilmsInMessage": 15,
  "minIntervalSec": 60
}
```

## 8. 部署与配置

- 服务启动时（`main.go`）调用 `EnsureBotPoller`：已启用通知且已保存 Token 则启动 Telegram 长轮询（删除 webhook、注册命令），未启用通知或无 Token 停止。
- 环境变量：`TG_PROXY`（可选，如 `http://127.0.0.1:7890`、`socks5://127.0.0.1:7891`）优先于通用代理变量。
- 升级注意：**已保存过通知配置的实例**，旧配置 JSON 不含 `sourceConfigChanged` 字段，反序列化后该事件默认为关闭；如需默认开启，请在管理后台手动打开或重新保存一次配置。

## 9. 待办与已知限制

- 概览消息超长分段发送时，「🔙 返回概要」只还原最后一段（前置分段留在会话中不合并）。
- 批次按源计数为内存累加，跨调用（同页重试）可能重复，靠「分源合计 X，去重影片 Y」行兜底说明。
- 「最后一集」取源站返回顺序的最后一个分集标签：源站/CDN 在 16↔18 间抖动（回退也算变化）时，同一 mid 会随批次反复上报更新，属「不一样算更新」的预期取舍；`update_stamp` 会被抖动覆盖。
- 附属站某条目被源站删除后，其旧 match_key 的 playlist 行不会立即清理（写路径按页内 key 删除，无法区分「条目消失」与「条目在其它页」）；这类残留行不参与更新列表判定（本次无内容不进列表），仅可能多占用详情页播放源展示。
- 被自动降级的旧主站通知已覆盖「编辑升级」与「新增主站」场景；数据清理/分类树同步失败等错误路径不补发通知（错误已返回管理端）。
