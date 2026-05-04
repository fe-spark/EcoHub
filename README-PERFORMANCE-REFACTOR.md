# 接口性能与读模型重构计划

## 背景

当前系统的慢接口不是单点问题，而是读路径设计问题。

多个高频接口仍在请求链路中直接查询 `film_index` 或 `film_list_snapshot`，并执行实时 `COUNT(*)`、`LIKE '%xxx%'`、排序分页和标签聚合。数据量增长后，这类接口会从毫秒级退化到几十秒。

本次重构目标不是继续给单个 SQL 打补丁，而是把影片读路径统一改成：

```text
采集阶段写事实，发布阶段生成读模型，请求阶段只读读模型。
```

## 强制执行立场

本次性能重构不做向后兼容。

执行过程中必须遵守：

1. 允许破坏旧接口内部实现
2. 允许直接新增、删除、重命名数据库表和字段
3. 不为了兼容旧查询路径保留双轨实现
4. 不保留旧 SQL 路径作为兜底
5. 不考虑旧数据平滑迁移
6. 新读模型落地后，调用方必须直接适配新结构

如果旧实现与读模型目标冲突，直接删除或重写旧实现。

本计划中的 `film_filter_options_snapshot`、`film_filter_index_snapshot`、`ActiveReadModel` 属于新主路径，不是旧链路的补丁层。

## 性能目标

高频接口必须满足：

1. 正常命中读模型时 P95 小于 1 秒
2. 任意筛选组合最差小于 5 秒
3. 首次请求不得承担全量读模型构建成本
4. 高频接口请求链路不得执行影片大表实时 `COUNT(*)`
5. 高频接口请求链路不得执行 `class_tag LIKE '%xxx%'` 扫表
6. 高频接口请求链路不得实时聚合筛选标签

## 接口分组

### 高频前台接口

以下接口必须走只读模型：

```text
GET /api/index
GET /api/searchFilm
GET /api/filmClassify
GET /api/filmClassifySearch
GET /api/filmPlayInfo
GET /api/navCategory
GET /api/provide/vod
GET /api/provide/config
```

### 高频后台影片接口

以下接口不能直接扫 `film_index`：

```text
GET /api/manage/film/search/list
GET /api/manage/film/class/tree
GET /api/manage/film/class/find
```

### 中低频后台管理接口

以下接口不套影片读模型，但需要索引、慢日志和必要归档：

```text
GET /api/manage/user/list
GET /api/manage/banner/list
GET /api/manage/mapping/rule/list
GET /api/manage/collect/list
GET /api/manage/collect/record/list
GET /api/manage/cron/list
GET /api/manage/file/list
```

### 写入与任务接口

以下接口不追求亚秒，但必须异步化、可观测、可恢复：

```text
POST /api/manage/spider/start
POST /api/manage/spider/stopAll
POST /api/manage/spider/update/single
POST /api/manage/spider/clear
POST /api/manage/film/add
POST /api/manage/film/search/del
POST /api/manage/film/class/tree/save
POST /api/manage/film/class/update
```

## 当前慢点归因

### 后台影片列表

接口：

```text
GET /api/manage/film/search/list
```

旧路径问题：

```go
options := service.FilmSvc.GetSearchOptions()
sl := service.FilmSvc.GetFilmPage(s)
```

风险点：

1. 默认列表请求会直接查询 `film_index`
2. `GetFilmPage` 旧逻辑会实时 `COUNT(*)`
3. `GetSearchOptions` 每次请求动态构建所有分类筛选项
4. 筛选项构建会间接触发多次标签聚合或快照扫描

目标路径：

```text
后台列表读取后台读模型
后台 options 读取已发布筛选项快照
```

### 前台分类筛选

接口：

```text
GET /api/filmClassifySearch
```

旧路径问题：

```text
列表 COUNT(*)
列表 LIKE / 条件过滤
Area GROUP BY
Language GROUP BY
Year GROUP BY
Plot Pluck + Go split
hasOthers COUNT
```

目标路径：

```text
筛选候选集来自倒排读模型
标签选项来自 filter options 快照
分页 total 来自读模型结果长度或预计算计数
```

### 公开搜索

接口：

```text
GET /api/searchFilm
```

旧路径问题：

```sql
name LIKE '%keyword%' OR sub_title LIKE '%keyword%'
COUNT(*)
ORDER BY year DESC, update_stamp DESC, mid DESC
```

目标路径：

```text
短期：进程内读模型字符串匹配
长期：轻量搜索索引或外部搜索引擎
```

### TVBox / Provide 列表

接口：

```text
GET /api/provide/vod
```

旧路径问题：

1. 无筛选时部分缓存可命中
2. 带筛选、关键词、最近更新时间时仍走 SQL 查询
3. 仍可能触发实时 `COUNT(*)`

目标路径：

```text
全部列表、筛选、关键词、最近更新都走统一读模型
```

## 目标架构

### 事实层

事实层保存采集和播放源事实，不直接服务高频列表查询。

```text
movie_detail_info
movie_playlist
source_categories
film_index
```

### 规则层

规则层表达当前分类和映射规则。

```text
mapping_rules
categories
category_mappings
```

### 发布读模型层

读模型层表达当前已发布版本的展示视图。

```text
film_list_snapshot
film_filter_options_snapshot
film_filter_index_snapshot
film_admin_list_snapshot
```

### 缓存层

缓存层只加速读模型，不作为业务真相源。

```text
Redis
Process memory ActiveReadModel
```

## 推荐数据结构

### film_list_snapshot

用途：前台列表和 TVBox 列表的基础展示快照。

核心字段：

```text
snapshot_version
mid
pid
cid
root_category_key
category_key
name
sub_title
class_tag
area
language
year
update_stamp
hits
score
picture
remarks
play_from_summary
```

### film_filter_options_snapshot

用途：保存每个一级分类下的筛选项，避免请求时聚合标签。

建议字段：

```text
snapshot_version
pid
name
value
score
sort
```

写入时机：

```text
采集收尾或快照发布阶段
```

读取接口：

```text
/api/filmClassifySearch
/api/manage/film/search/list
/api/provide/config
```

### film_filter_index_snapshot

用途：保存筛选倒排索引，替代 `class_tag LIKE '%xxx%'`。

建议字段：

```text
snapshot_version
pid
cid
tag_value
mid
update_stamp
hits
score
year
```

示例：

```text
mid=100
pid=1
cid=11
class_tag=剧情,家庭
area=大陆
language=国语
year=2026
```

生成倒排行：

```text
Category=11 -> mid=100
Plot=剧情 -> mid=100
Plot=家庭 -> mid=100
Area=大陆 -> mid=100
Language=国语 -> mid=100
Year=2026 -> mid=100
```

查询时不再使用：

```sql
class_tag LIKE '%家庭%'
```

改为：

```sql
tag_type = 'Plot' AND tag_value = '家庭'
```

### ActiveReadModel

用途：服务进程内的当前生效读模型。

建议结构：

```go
type FilmReadModel struct {
	Version string
	ByMid   map[int64]FilmDoc
	ByPid   map[int64][]int64
	ByCid   map[int64][]int64
	ByTag   map[TagKey][]int64
	Options map[int64]FilterOptions
}
```

切换方式：

```text
新读模型构建完成后，原子替换 active 指针。
旧读模型自然释放。
```

## 请求链路禁止行为

高频接口中禁止出现：

```go
dto.GetPage(query, page)
```

除非该 query 明确不是影片大表。

高频接口中禁止出现：

```sql
COUNT(*) FROM film_index
COUNT(*) FROM film_list_snapshot
class_tag LIKE '%xxx%'
```

高频接口中禁止在请求时执行：

```text
Area GROUP BY
Language GROUP BY
Year GROUP BY
class_tag Pluck + split
全量标签重建
读模型全量重建
```

## 采集阶段如何铺垫优化

采集阶段只写事实，但必须为发布读模型准备足够信息。

主站写入 `film_index` 时必须包含：

```text
mid
source_id
root_category_key
category_key
pid
cid
name
sub_title
class_tag
area
language
year
update_stamp
hits
score
picture
remarks
play_from_summary
```

同时维护最小影响面的标签事实：

```text
Category
Plot
Area
Language
Year
Sort
```

采集收尾阶段执行：

```text
1. 刷新播放源摘要
2. 重建 film_list_snapshot
3. 重建 film_filter_options_snapshot
4. 重建 film_filter_index_snapshot
5. 加载 ActiveReadModel
6. 预热 Redis / 进程内缓存
7. 原子切换 active snapshot version
```

用户请求不得触发第 2 到第 6 步。

## 分阶段实施计划

### 第一阶段：慢日志和边界确认

目标：所有超过 1 秒的接口都能定位阶段耗时。

任务：

1. 给核心 handler 增加阶段耗时日志
2. 记录列表、options、详情、缓存加载、序列化耗时
3. 增加统一慢接口日志
4. 梳理所有 `dto.GetPage` 调用点

验收：

```text
任意接口超过 1 秒，日志能显示慢在哪个阶段。
```

### 第二阶段：筛选 options 快照化

目标：后台和前台不再请求时构建筛选项。

任务：

1. 新建 `film_filter_options_snapshot`
2. 快照发布阶段生成所有一级分类 options
3. `/api/filmClassifySearch` 读取 options 快照
4. `/api/manage/film/search/list` 读取 options 快照
5. `/api/provide/config` 读取 options 快照
6. 删除请求时动态 `GetSearchTag` 主路径

验收：

```text
后台列表 options 获取 < 100ms
前台筛选 tags 获取 < 100ms
```

### 第三阶段：筛选倒排索引

目标：删除高频路径中的 `class_tag LIKE '%xxx%'`。

任务：

1. 新建 `film_filter_index_snapshot`
2. 发布阶段按影片拆分标签并写入倒排索引
3. 多条件筛选用候选集合求交
4. 排序从 `film_list_snapshot` 或 ActiveReadModel 文档读取
5. 删除前台筛选 SQL 查询主路径

验收：

```text
任意 Plot / Area / Language / Year / Category 组合筛选 < 1s
最差 < 5s
```

### 第四阶段：进程内 ActiveReadModel

目标：请求阶段不再 Redis JSON 反序列化大数组。

任务：

1. 新增 `server/internal/readmodel`
2. 服务启动时加载 active snapshot
3. 快照发布后构建新读模型
4. 构建完成后原子切换
5. `/api/searchFilm` 改用内存搜索
6. `/api/filmClassifySearch` 改用内存筛选
7. `/api/provide/vod` 改用内存筛选
8. 相关推荐改用内存候选集

验收：

```text
无 Redis miss 抖动
无首次用户请求重建读模型
高频前台接口 P95 < 1s
```

### 第五阶段：后台影片管理读模型

目标：后台默认列表不再阻塞。

任务：

1. 后台列表复用 ActiveReadModel 或独立 AdminReadModel
2. 后台 options 读取 `film_filter_options_snapshot`
3. 后台默认列表不查 `film_index COUNT(*)`
4. 删除后台动态构建所有分类 options 的逻辑

验收：

```text
/api/manage/film/search/list 默认页 < 1s
任意后台筛选 < 1s
```

### 第六阶段：删除旧查询路径

目标：不保留双轨和误用入口。

需要删除或迁移：

```go
ListFilmSnapshotsByTags
SearchSnapshotsByKeyword
SearchFilmKeyword
GetSearchPage
ListFilmIndexesByTags
GetSnapshotMovieListByCategoryPage
```

保留前必须满足：

```text
仅低频管理工具使用
明确标注禁止高频调用
有慢日志保护
```

### 第七阶段：非影片接口治理

目标：避免其他运维表未来变慢。

处理接口：

```text
/api/manage/user/list
/api/manage/mapping/rule/list
/api/manage/file/list
/api/manage/collect/record/list
/api/manage/cron/list
/api/manage/banner/list
```

处理方式：

```text
小表：普通索引 + COUNT 可接受
增长型运维表：组合索引 + 归档
日志型表：cursor pagination
```

## 推荐目录结构

```text
server/internal/repository/film/
  fact_repo.go
  snapshot_repo.go
  filter_option_repo.go
  filter_index_repo.go
  read_model_repo.go

server/internal/service/
  index_service.go
  film_service.go
  provide_service.go
  read_model_service.go

server/internal/readmodel/
  film_model.go
  film_loader.go
  film_filter.go
  film_search.go
  film_store.go
```

职责：

```text
repository/film：数据库读写
service：业务编排
readmodel：内存读模型、筛选、排序、分页
```

## 技术选型

### 推荐方案

```text
MySQL 物化读表 + 进程内 ActiveReadModel
```

优点：

1. 适合当前数据规模
2. 不引入额外中间件
3. 请求链路最快
4. 可通过 MySQL 恢复读模型
5. 结构清晰，可观测

### 不推荐继续只靠 Redis JSON 大数组

原因：

1. 每次请求可能反序列化大数组
2. 缓存 miss 抖动明显
3. 多接口重复构建
4. 集合求交和排序不自然
5. 无法作为长期稳定读模型

### 暂不推荐 Elasticsearch

原因：

1. 当前 7 万级数据没有必要
2. 运维复杂度增加
3. 当前核心问题是筛选和分页，不是复杂全文搜索

未来数据到百万级，或需要复杂中文分词搜索时，再评估 Meilisearch、Typesense 或 Elasticsearch。

## 最终验收标准

重构完成后必须满足：

```text
/api/index < 500ms
/api/searchFilm < 1s
/api/filmClassifySearch < 1s
/api/provide/vod < 1s
/api/manage/film/search/list < 1s
任意冷启动后首次请求 < 5s
采集发布后读模型预热完成再切换版本
高频接口无实时 COUNT(*)
高频接口无 class_tag LIKE '%xxx%'
高频接口无请求时标签聚合
```

## 最终原则

后续所有影片高频读取都必须遵守：

```text
不读事实表做列表。
不请求时聚合标签。
不请求时构建读模型。
不高频 COUNT 大表。
不高频 LIKE 扫标签。
```

允许破坏旧接口内部实现，不保留旧查询路径作为兼容分支。
