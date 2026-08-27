# EcoHub 访问日志与请求画像方案

状态：方案文档，尚未实施  
范围：Go API 访问采集、Redis 热聚合、管理后台「访问分析」  
约束：不新增中间件栈 / 数据库；面向 2 核 2G 自托管

---

## 1. 背景与现状

EcoHub 是自托管影视聚合系统。流量来自 Web 前台、鸿蒙 / 安卓客户端、TVBox / 影视仓（`/api/provide/*`）以及管理后台。站点管理员目前只能在「系统设置 → 系统日志」里看到混在一起的文本行，无法回答：

- 谁在访问（Web / App / TVBox）
- 在干什么（浏览 / 搜索 / 播放 / Provide 拉取）
- 接口是否健康（QPS、延迟、4xx/5xx、慢请求）

### 1.1 现有访问日志

| 能力 | 实现 | 问题 |
| --- | --- | --- |
| HTTP 访问 | `server/internal/middleware/access_log.go` 打一行 `[HTTP] status \| cost \| IP \| method path` | 非结构化，无法聚合 |
| 落盘 | `server/internal/infra/syslog`：10MB 滚动、保留 7 天、内存缓冲 1 万行 | 与采集、通知、慢查询日志混写 |
| 后台展示 | `/api/manage/system/logs/delta` + 系统日志页，最多 1000 行、关键词过滤 | 不能按接口 / 客户端 / 延迟 / 错误筛选 |
| 噪声过滤 | 仅跳过 `/api/manage/collect/list`、`/api/manage/system/logs/delta` 的 2xx | `/api/health`、海报静态、TVBox 列表轮询仍进日志 |
| 客户端识别 | OHOS 已设 `User-Agent: EcoHub-OHOS`；Android 无专用 UA | Web / TVBox / App 分不清 |
| 真实 IP | `c.ClientIP()`，未配置 `TrustedProxies` | 反代后经常是容器或网关地址 |
| 业务埋点 | 播放、搜索只服务业务，不记访问 | 无热搜、热播、入口分布 |

### 1.2 结论

访问日志与系统日志职责不同：前者是流量与画像，后者是排障。继续往 `ecohub.log` 堆 `[HTTP]` 行无法演进。  
也不应把每条请求写成 MySQL 行：TVBox 轮询 + 采集轮询会打满 2 核 2G 的磁盘与 InnoDB。

---

## 2. 目标

站点管理员打开后台即可回答三类问题：

1. **谁在访问**：Web / OHOS / Android / TVBox，日 PV / UV，Top IP（打码）
2. **在干什么**：首页、分类、搜索、播放、Provide 拉取
3. **是否健康**：QPS、近似 P95、4xx/5xx、慢请求（≥500ms）

可验收标准（P0）：

- 2xx 正常访问不再写入系统日志
- 后台「访问分析」能看到今日 PV / UV、24h QPS、错误数、最近请求明细
- 采集轮询、日志轮询、健康检查、海报静态、分析接口自身不进入画像
- 请求路径不因分析功能变慢（采集失败或 channel 满只丢事件，不阻塞）

---

## 3. 非目标

- 不引入 Elasticsearch / ClickHouse / Loki / Prometheus / Grafana
- 不把访问明细做成登录用户画像（前台多数无登录）
- 不在 Next.js 或 App 内再打一套前端点（会漏 TVBox，且可被关闭）
- 不长期存储完整 IP、Cookie、JWT、完整 Query
- 不把 Provide 的完整 query 当作 Path 基数（`ac=list|detail` 即可）
- 第一期不落 MySQL 日报（P2 可选）

---

## 4. 方案对比

| 方案 | 做法 | 优点 | 缺点 | 结论 |
| --- | --- | --- | --- | --- |
| **A. 热聚合 + 短窗明细** | 中间件产结构化事件 → 有界 channel → Redis 计数 / HLL / ZSET + 最近 N 条 ring | 无新依赖，查询快，写放大可控 | 明细只保留几十小时 | **采用** |
| B. 全量 MySQL 明细 | 每请求 INSERT | 实现直接、可任意翻历史 | TVBox / 采集高 QPS 拖垮库 | 不做主路径 |
| C. 外部可观测栈 | Loki + Grafana 等 | 能力最强 | 违背「2 核 2G 开箱」 | 不做默认 |

P2 可将日聚合滚到 MySQL，保留 30～90 天趋势；明细仍不进 MySQL。

---

## 5. 架构

```
HTTP 请求
  │
  ├─ AccessLog 中间件（同步只做采集，目标 <1ms）
  │     ├─ 跳过 health / 日志轮询 / 采集轮询 2xx / 海报静态 / 分析接口自身
  │     ├─ 结构化 AccessEvent（路径脱敏、IP 打码、Action / ClientType）
  │     └─ 非阻塞投递有界 channel（满则丢，dropped++）
  │
  ├─ 业务 handler（不变）
  │
  └─ 后台 1 个 worker
        ├─ Redis：分钟桶、日 UV、客户端 / 行为 Hash、Top ZSET、延迟桶、recent / slow / error List
        └─ 仅 4xx/5xx 与慢请求（≥500ms）写入 syslog（运维通道）

管理接口  GET /api/manage/access/overview|logs|tops
后台页面  /manage/access
```

原则：

- **中间件不写 Redis。** Redis 故障或短暂阻塞不能拖接口。
- **2xx 正常访问不写系统日志。** 系统日志只留错误、慢请求、业务告警。
- **分析失败静默降级。** worker 挂了，站点仍可用，后台显示「采集暂停 / 无数据」。

---

## 6. 数据模型

### 6.1 单次事件（不落 MySQL，只进 Redis）

```go
type AccessEvent struct {
    Ts         time.Time `json:"ts"`
    Method     string    `json:"method"`
    Path       string    `json:"path"`       // 去 query、去换行、去 token
    Route      string    `json:"route"`      // public / provide / manage / health / static
    Action     string    `json:"action"`     // browse / search / play / classify / provide / manage / other
    Status     int       `json:"status"`
    LatencyMs  int64     `json:"latencyMs"`
    Bytes      int       `json:"bytes"`
    ClientType string    `json:"clientType"` // web / ohos / android / tvbox / manage / crawler / unknown
    IPHash     string    `json:"-"`          // HMAC(IP)，只用于 UV，不进明细 JSON
    IPPreview  string    `json:"ipPreview"`  // IPv4 末段打码 1.2.3.x；IPv6 留前 48bit
    UAFamily   string    `json:"uaFamily"`   // chrome / safari / ecohub-ohos / tvbox / bot / ...
    Resource   string    `json:"resource"`   // 影片 ID 或搜索词（截断 32 字）
}
```

`IPHash` 不得下发前端。明细接口只返回 `IPPreview`。

### 6.2 路径与资源提取

| 接口 | Action | Resource |
| --- | --- | --- |
| `GET /api/index`、`/api/index/dailyUpdates`、`/api/dailyUpdates`、`/api/navCategory` | `browse` | 空 |
| `GET /api/searchFilm` | `search` | `keyword` 截断 32 字 |
| `GET /api/filmPlayInfo` | `play` | `id` |
| `GET /api/filmClassify`、`/api/filmClassifySearch` | `classify` | 分类 / 标签摘要（可选） |
| `GET /api/provide/vod` | `provide` | `ac`（list / detail / 空） |
| `GET /api/provide/config` | `provide` | `config` |
| `/api/manage/*` | `manage` | 空 |
| 其他 | `other` | 空 |

Provide 的 Path 归一为 `/api/provide/vod` 或 `/api/provide/config`，禁止把 `?t=&pg=&wd=` 拼进 Path，避免 ZSET 基数爆炸。

### 6.3 客户端判定

按顺序命中即停：

| ClientType | 规则 |
| --- | --- |
| `ohos` | `User-Agent` 含 `EcoHub-OHOS`（鸿蒙客户端已设置） |
| `android` | `User-Agent` 含 `EcoHub-Android`（**当前未设置，P1 补客户端 Header**） |
| `tvbox` | 路径前缀 `/api/provide/` |
| `manage` | 路径前缀 `/api/manage/` |
| `crawler` | UA 含 `bot` / `spider` / `crawler` / `curl` / `wget`（大小写不敏感） |
| `web` | 其余前台 API |
| `unknown` | 无法归类的静态或异常路径 |

P0 不依赖 Android 改动：Android 流量会暂时记入 `web`，P1 补 UA 后自动分开。

### 6.4 噪声过滤（不采集）

在现有 skip 上扩展：

- `GET|HEAD /api/health`
- `GET /api/manage/system/logs/delta`
- `GET /api/manage/collect/list` 且状态 &lt; 400
- `GET /api/manage/access/*`（避免自采样）
- 前缀 `/api/upload/pic/poster/`
- 方法 `OPTIONS`（CORS 预检）

`/api/provide/vod` **要记**，这是 TVBox 流量主体。

---

## 7. Redis 设计

统一前缀 `EcoHub:Access:`，与现有 `EcoHub:*` 一致。启动时的 Redis 整批清理若扫 `EcoHub:*`，访问数据会随重启丢失——**这是可接受的**：热数据本身就有 TTL，重启后从零累计。若后续启动清理要保留访问数据，再把扫描模式收窄（P2 再议，第一期不改清理逻辑）。

| Key | 结构 | TTL | 用途 |
| --- | --- | --- | --- |
| `EcoHub:Access:min:{yyyyMMddHHmm}` | Hash：`pv` `err4` `err5` `cost_sum` | 48h | QPS / 错误曲线 |
| `EcoHub:Access:uv:{yyyyMMdd}` | HyperLogLog(`IPHash`) | 14d | 日 UV |
| `EcoHub:Access:client:{yyyyMMdd}` | Hash：`web` `ohos` `android` `tvbox` `manage` `crawler` `unknown` | 14d | 客户端占比 |
| `EcoHub:Access:action:{yyyyMMdd}` | Hash：`browse` `search` `play` `classify` `provide` `manage` `other` | 14d | 行为画像 |
| `EcoHub:Access:top:path:{yyyyMMdd}` | ZSET（member=`METHOD path`） | 14d | 热接口 |
| `EcoHub:Access:top:search:{yyyyMMdd}` | ZSET（member=关键词） | 14d | 热搜 |
| `EcoHub:Access:top:play:{yyyyMMdd}` | ZSET（member=影片 ID） | 14d | 热播 |
| `EcoHub:Access:hist:{yyyyMMdd}` | Hash：`b50` `b100` `b200` `b500` `b1000` `bInf` | 14d | 延迟分布 / 近似 P95 |
| `EcoHub:Access:recent` | List，右侧 LPUSH，LTRIM 2000 | 48h | 访问日志 |
| `EcoHub:Access:slow` | List，最近 200 条（`latencyMs >= 500`） | 7d | 慢请求 |
| `EcoHub:Access:error` | List，最近 200 条（status ≥ 400） | 7d | 异常 |
| `EcoHub:Access:meta:dropped` | 整数，进程内也可另计 | 14d | channel 丢弃计数 |

写入策略：

- worker 对单条事件用 pipeline 一次提交（INCR / PFADD / ZINCRBY / LPUSH / LTRIM / EXPIRE）
- EXPIRE 只在 key 新建时设，避免每条请求刷新 TTL 放大写入
- 延迟用固定桶，不用精确百分位结构；P95 用桶上界估算即可

近似 P95：从高桶往低桶累加，命中的桶上界作为 P95。2 核 2G 够用，误差可接受。

---

## 8. 隐私与安全

| 数据 | 处理 |
| --- | --- |
| 完整 IP | 不落盘、不下发。UV 用 `HMAC-SHA256(salt, ip)` 的 hex 前 16 字节 |
| 明细 IP | IPv4 `a.b.c.x`；IPv6 保留前 48 bit，其余置零后再压缩显示 |
| Query | Path 去掉全部 query。`token` / `password` / `pwd` 即使误入也剥离 |
| 搜索词 | 只进 `Resource` 与热搜 ZSET，截断 32 字，不做全文长期存储 |
| Cookie / JWT | 不采集 |
| HMAC salt | 独立配置 `ACCESS_IP_SALT`，缺省时用 `JWT_SECRET` 派生（`sha256("ecohub-access-ip:" + JWT_SECRET)`），文档中说明正式环境建议单独设置 |

权限：查询接口挂在现有 `manageRoute` 上，走 `AuthToken` + `WriteAccess`（与系统日志一致）。guest 只读。

---

## 9. 真实 IP（TrustedProxies）

Gin 默认不信任任意 `X-Forwarded-For`。反代（1Panel / Nginx / Caddy）后 `ClientIP()` 经常是 Docker 网关。

新增环境变量：

```
TRUSTED_PROXIES=127.0.0.1,::1
```

- 默认 `127.0.0.1,::1`（同容器 Next 反代、本机）
- 1Panel / 独立 Nginx 填反代容器或宿主机地址，例如 `172.17.0.1` 或内网网段
- 启动时 `engine.SetTrustedProxies(list)`；解析失败打 WARN 并回退默认值
- 同时识别 `X-Forwarded-For` / `X-Real-IP`（Gin 已支持，关键是信任列表）

不配对的后果：UV 塌缩成 1、Top IP 无意义。画像的客户端与行为统计仍可用。

---

## 10. 后台产品

独立侧栏菜单 **访问分析**（`/manage/access`），不塞进「系统设置 → 系统日志」。

遵循 `.ai/ui-rules.md`：antd 6、CSS Module、8pt 间距、单层 Card、禁止 `:global`。

页面三个区块（同一页滚动，不拆子路由）：

### 10.1 概览

- 数字：今日 PV、今日 UV、错误数（4xx+5xx）、近似 P95
- 24 小时 QPS 折线（按分钟桶汇总到 5 分钟点，288 点太多则 15 分钟）
- 客户端占比（饼图或横向条）

### 10.2 请求画像

- 行为分布：browse / search / play / classify / provide / manage
- 热接口 Top 10
- 热搜 Top 10（P0 可先占位，有数据即显示；无搜索则为空态）
- 热播 Top 10（影片 ID；P1 可反查片名）

### 10.3 访问日志

单层 Card 内：筛选 + 表格。

筛选项：状态（全部 / 2xx / 4xx / 5xx）、客户端、路径关键词、仅慢请求。

列：时间、方法、路径、状态、耗时、客户端、打码 IP、UA 族。

快捷视图：全部 / 慢请求 / 错误。数据分别读 `recent` / `slow` / `error`。

空态文案：「暂无访问记录。若站点刚启动或分析已关闭，属预期。」

---

## 11. 管理接口

均需登录。只读，无 POST。

### `GET /api/manage/access/overview`

Query：`day` 可选，默认当天（站点本地时区）。

```json
{
  "day": "2026-08-27",
  "pv": 12345,
  "uv": 678,
  "err4": 12,
  "err5": 1,
  "p95Ms": 200,
  "dropped": 0,
  "client": { "web": 8000, "tvbox": 3000, "ohos": 1000, "android": 0, "manage": 300, "crawler": 45 },
  "action": { "browse": 5000, "search": 400, "play": 2000, "provide": 4000, "classify": 800, "manage": 300 },
  "series": [{ "t": "2026-08-27T10:00:00+08:00", "pv": 80, "err4": 0, "err5": 0 }]
}
```

### `GET /api/manage/access/tops`

Query：`day`、`kind=path|search|play`、`limit` 默认 10 最大 50。

```json
{
  "kind": "search",
  "items": [{ "key": "庆余年", "count": 42 }]
}
```

### `GET /api/manage/access/logs`

Query：`source=recent|slow|error`（默认 recent）、`limit` 默认 100 最大 200、`status`、`client`、`q`（路径包含）。

过滤在服务端对 List 快照做，List 本身已有上限，不必上搜索引擎。

```json
{
  "list": [
    {
      "ts": "2026-08-27T10:01:02+08:00",
      "method": "GET",
      "path": "/api/filmPlayInfo",
      "status": 200,
      "latencyMs": 36,
      "clientType": "web",
      "ipPreview": "203.0.113.x",
      "uaFamily": "chrome",
      "resource": "1024"
    }
  ]
}
```

---

## 12. 配置项

写入 `.env.example` / `config.go`，默认开启、可关。

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `ACCESS_LOG_ENABLED` | `true` | 关闭后中间件不采集、不写 Redis |
| `ACCESS_SLOW_MS` | `500` | 慢请求阈值，同时决定是否写 syslog |
| `ACCESS_RECENT_LIMIT` | `2000` | recent List 长度 |
| `TRUSTED_PROXIES` | `127.0.0.1,::1` | 逗号分隔 |
| `ACCESS_IP_SALT` | 空（由 JWT_SECRET 派生） | IP HMAC 盐 |

不新增 Docker 服务，不改 compose 拓扑。

---

## 13. 拟改文件

```
server/internal/middleware/access_log.go      # 改造：产事件；2xx 不再 log.Printf
server/internal/access/                       # 新增
  event.go                                    # 模型、脱敏、FromContext
  classify.go                                 # Route / Action / ClientType / UAFamily
  collector.go                                # channel、worker、丢弃计数、启停
  redis_store.go                              # pipeline 写入
  query.go                                    # overview / tops / logs 读取
  access_test.go                              # 判定、脱敏、跳过路径
server/internal/handler/access_handler.go     # 三个 GET
server/internal/router/router.go              # 注册路由；SetTrustedProxies
server/internal/config/config.go              # 上述环境变量
server/cmd/server/main.go                     # 启动 collector

web/src/app/manage/access/page.tsx
web/src/app/manage/access/view/index.tsx
web/src/app/manage/access/view/index.module.less
web/src/app/manage/layout-view/index.tsx      # 侧栏增加「访问分析」

.env.example                                  # 追加变量注释
```

Android 客户端（独立仓库 `app-for-android/`）P1：请求头增加 `User-Agent: EcoHub-Android/{version}`。  
OHOS 已具备，无需改。

单文件超过 500 行则按 `event` / `collector` / `redis_store` / `query` 拆分，禁止把 Redis 与 HTTP 塞进一个文件。

---

## 14. 中间件示意

```go
func AccessLog() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        c.Next()
        if !config.AccessLogEnabled {
            return
        }
        evt := access.FromContext(c, time.Since(start))
        if evt == nil {
            return
        }
        access.Collect(evt) // select { case ch <- evt: default: dropped++ }
        if evt.Status >= 400 || evt.LatencyMs >= config.AccessSlowMs {
            syslog.Warnf("[HTTP] %d | %dms | %s | %s %s",
                evt.Status, evt.LatencyMs, evt.IPPreview, evt.Method, evt.Path)
        }
    }
}
```

channel 容量建议 1024。worker 单协程即可，避免 Redis 命令乱序与 pipeline 竞争。进程退出时 `close(ch)` 并 drain 剩余事件，最多等 1 秒。

现有 `sanitizeAccessLogPath` 的换行剥离保留，并扩展为去掉 query、限制 Path 长度 256。

---

## 15. 与系统日志的关系

| | 系统日志 | 访问分析 |
| --- | --- | --- |
| 用途 | 排障、采集、通知、慢 SQL | 流量与请求画像 |
| 存储 | 文件滚动 + 内存 seq | Redis |
| 后台入口 | 系统设置 → 系统日志 | 侧栏「访问分析」 |
| HTTP 行 | 仅 4xx/5xx 与慢请求 | 全量（已过滤噪声） |

实施后：打开系统日志不应再被正常 2xx 刷屏。

---

## 16. 分期与验收

### P0（本方案第一期，可独立上线）

- 结构化采集 + Redis 聚合 + 后台概览 / 明细
- 2xx 退出系统日志
- `TRUSTED_PROXIES`
- 热搜 / 热播 ZSET 一并写入（有数据就展示，不依赖客户端改动）

验收：

1. 本机连续访问首页、搜索、播放，概览 PV 增加，明细可见对应 Path
2. 打开系统日志，不再出现大量 2xx `[HTTP]`
3. 连续刷新系统日志页，访问分析中不应出现 `/api/manage/system/logs/delta`
4. `ACCESS_LOG_ENABLED=false` 后概览不再增长，接口正常
5. Redis 临时断开：前台接口仍 200，后台概览为空或提示不可用，进程不 panic

### P1

- Android `User-Agent: EcoHub-Android/{version}`
- 热播 ID 反查片名
- 慢请求单独卡片强化

### P2（按需）

- MySQL 日聚合表 `access_daily_stats` / `access_daily_top`，保留 90 天
- CSV 导出
- 错误率超阈值走现有 Telegram 通知
- 启动时 Redis 清理是否排除 `EcoHub:Access:*`

---

## 17. 风险与取舍

| 风险 | 处理 |
| --- | --- |
| Redis 与站点同机，高 QPS 写放大 | pipeline + 跳过噪声 + 单 worker；2xx 不再写磁盘 |
| channel 满丢事件 | `dropped` 计数展示在概览；宁丢分析不丢请求 |
| 反代未配 TrustedProxies | UV / IP 失真，客户端与行为统计仍可用；文档与 `.env.example` 标明 |
| 进程重启丢失分钟桶 | TTL 数据，可接受；P2 再考虑日报 |
| Provide 被刷 | 只记归一 Path + `ac`，ZSET 不会被 query 打爆 |
| 启动扫描删除全部 `EcoHub:*` | 第一期接受访问数据随清理归零；不在 P0 改全局清理 |

更优但未采用的路径：ClickHouse / Loki 能存更长明细，但要额外容器与运维，不符合当前部署模型。

---

## 18. 默认决策（实施时按此执行，除非另行变更）

1. **范围**：P0 含流量 / 延迟 / 错误 / 最近明细，并写入热搜 / 热播（展示有则显示）。片名反查放到 P1。
2. **入口**：独立侧栏「访问分析」，不进系统设置 Tab。
3. **明细保留**：Redis 最近 2000 条、TTL 48 小时；更长历史要等 P2 MySQL 日报，不做全量明细表。

---

## 19. 现状代码锚点

- 访问中间件：`server/internal/middleware/access_log.go`
- 路由注册：`server/internal/router/router.go`（`r.Use(middleware.AccessLog())`）
- 系统日志：`server/internal/infra/syslog/logger.go`、`server/internal/handler/system_log_handler.go`
- 后台系统日志页：`web/src/app/manage/system/logs/view/index.tsx`
- 侧栏：`web/src/app/manage/layout-view/index.tsx`
- Provide：`server/internal/handler/provide_handler.go`
- 播放 / 搜索：`server/internal/handler/index_handler.go`（`FilmPlayInfo`、`SearchFilm`）
- OHOS UA：`app-for-ohos/entry/src/main/ets/common/utils/AppVersionUtil.ets`
