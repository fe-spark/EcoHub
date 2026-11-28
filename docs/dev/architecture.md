# 核心机制与数据链路

中文 | [English](../en/dev/architecture.md)

本文面向开发者与排障人员，说明 EcoHub 服务端与前端的核心运行机制及数据流向。使用层面常见问题见 [常见问题](../guide/faq.md)。

## 采集流式发布与索引封口

为兼顾采集吞吐与前台查询稳定性，系统在写库与内存搜索索引之间采用「流式发布 + 队列末统一封口」两段式设计（位于 `server/internal/collect/` 与 `server/internal/film/snapshot/`）：

1. **流式发布（`streamBuf`）**：批次写入 `film_index` 后，变更影片 ID 进入有界缓冲通道（主站每 `300` 部或 `3` 秒、非主站每 `1000` 部或 `10` 秒刷新一次；积压达到 `5000` 部触发背压等待），批量更新筛选标签表 `search_tag_item`、立即删除受影响影片的 `PlayInfo` Redis 缓存，并触发约 `5` 秒防抖的 Redis 列表缓存清理（连续写入时在最后一批完成约 `5` 秒后统一清理）。
2. **搜索索引封口（`reseal`）**：全量搜索与筛选内存索引不在每批次加锁重建，而是在当前全部活跃采集队列结束后统一执行：累计变更低于 `50000` 部时执行增量修补；达到或超过 `50000` 部或内存读模型不可用时执行顺序全量重建。

## 去重、跨站匹配与刮削存储

- **同站去重**：`movie_source_mapping` 以「采集站 + 采集站 `vod_id`」定位唯一记录；同站同页若出现相同 `vod_id` 仅保留该页最后一条，不同 `vod_id` 不会在采集阶段合并。分类键固定为 `source:{采集站 ID}:{采集站分类 ID}`。
- **跨站匹配**：读取播放页或封面匹配时按优先级依次检索匹配键：`豆瓣 ID + 片名` → `片名 + 大类` → `片名`。未命中豆瓣 ID 时还会校验归一化后的一级分类兼容性（`peerCategoryCompatible`），并过滤非主站隐藏分类下的影片；多键命中同一影片只计一次，播放线路始终按采集站分组显示。
- **TMDB 刮削与首页数据**：影片刮削直接更新 `film_index` 并锁定对应字段列，同时在 `film_tmdb_scrapes` 表持久化 `mid` 与 TMDB 编号及媒体类型映射。首页轮播（`banner_slot`）与首页推荐（`home_recommend_group`、`home_recommend_item`）按采集站隔离保存；绑定豆瓣或 Bangumi 榜单的推荐分组缓存原始榜单 `6` 小时，并在采集索引封口钩子（`RegisterCollectDoneHook`）触发后自动匹配同步。

## 采集记录与重试模型

- 批次开跑时在 `collect_records` 写入 `running` 主记录，批次结束时聚合各站状态与页级结果落终态；「单片更新」将目标影片 ID 保存在 `collect_records.target_mids`。
- 主站影片入库时同步回写 `film_index.last_record_id`（配合复合索引 `idx_film_index_source_record_mid`）。Telegram 采集通知查看主站更新列表时，直接按主站与最近一批 `record_id` 游标分页读取 `film_index`，提供分类与影片详情两级交互菜单。
- 失败重试在批次结束或开始后 `30` 分钟内优先重跑记录中的失败页，超过 `30` 分钟自动降级为重跑对应失败采集站的小时窗口；启动初始化与每日 `03:30` 定时任务分批清理 `30` 天前已结束历史记录，服务重启时由 `MarkInterruptedCollectRecords` 标记遗留 `running` 记录为异常中断。

## 读模型、排序与 Redis 缓存策略

列表排序默认按 `film_index.update_stamp` 降序、再按 `mid` 排序。新入库影片使用上游更新时间；已入库影片仅当播放线路变化或在后台手动保存时才刷新 `update_stamp`（仅改下载线路、备注、演员或封面不变动排序）。

在查询当前主站且内存读模型可用时，前台分类页与 TVBox 订阅共用 `tagPageFromIndex` 内存索引（仅指定非主站或内存索引不可用时回退查询 MySQL `film_index`）：

| 查询场景 | 缓存介质与 TTL | 失效或穿透条件 |
| --- | --- | --- |
| 前台筛选页 | Redis 缓存 `2` 小时（空结果 `1` 分钟），覆盖前 `20` 页 | 流式发布 `5` 秒防抖清理；超过 `20` 页不缓存 |
| TVBox 分类元数据 | Redis 缓存 `5` 分钟（带最长 `60` 秒随机抖动） | 采集封口清理分类与搜索缓存 |
| TVBox 常规列表 | Redis 缓存 `12` 小时（带最长 `15` 分钟随机抖动，空结果 `1` 分钟），覆盖前 `20` 页 | 带搜索词、带筛选条件或超过 `20` 页直接查读模型；流式发布 `5` 秒防抖清理 |

## 弹幕自适应排版与嗅探缓存

- **字号缩放**：在 `web/src/components/public/VideoPlayer/danmakuLayout.ts` 中以 `BASE_WIDTH = 960`（`16:9` 等效宽度 `min(playerWidth, playerHeight × 16 ÷ 9)`）为 `1x` 基准。大于 `960` 像素按 `1 + (ratio - 1) × 0.2` 放大（上限 `1.2` 倍）；小于 `960` 像素按比例缩小（下限 `0.5` 倍），且实际宽度小于 `960` 像素时额外受限于 `playerWidth ÷ 22`，最终限制在 `12–120` 像素。用户输入或调整字号时通过反算保存回 `1x` 基准值到浏览器 `localStorage`。
- **显示区域**：沿用插件原生 `[topMargin, bottomMargin]`（默认 `[10, "50%"]`），从播放器容器顶部起算，窄屏或异形屏下可利用上下黑边空间降低对正片画面的遮挡。宽度低于 `768` 像素时顶部/底部固定弹幕转为滚动弹幕；低于 `560` 像素时收起输入框与网页全屏按钮（仅保留圆点提示）。
- **全网弹幕缓存**：嗅探命中的全网弹幕按接口地址、片名与集数缓存 `7` 天；未匹配与失败结果不缓存。

## 登录态双端校验流程

1. Next.js 中间件 `web/src/proxy.ts` 在边缘层检查 `ecohub_auth_token` Cookie 是否存在（无 Cookie 直接跳转 `/login`）。
2. 进入 `/manage` 时服务端布局 `web/src/app/manage/layout.tsx` 请求 `/api/manage/user/info`，由 Go 后端同时验证 JWT 签名（有效期 `10` 天）与 Redis 中保存的当前会话 token（实现同账号异地登录顶替）。
3. 后台写操作与高危管理接口在后端路由中分别由 `WriteAccess`（拦截访客角色写入，返回 `403`）与 `AdminAccess`（限制超级用户）兜底控制。

## 相关文档

[计划任务运行冲突](./cron-task-conflict.md) · [开发指南](./setup.md) · [服务端说明](../../server/README.md) · [前端说明](../../web/README.md)
