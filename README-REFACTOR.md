# EcoHub 重构计划

## 背景

当前后端在以下四类链路之间存在职责串联过深的问题：

- 分类骨架
- 分类映射关系
- 主站采集
- 附属站采集

现状的主要问题不是单条 SQL 写得不够好，而是多条业务链路被串成了长链路，导致任意一个环节变更时，都会触发不必要的重建、回灌、标签刷新与播放摘要刷新，最终把 MySQL IO 和长事务压力拉满。

目标不是做局部补丁，而是把职责边界重新切清楚，让系统运行更顺滑、启动更稳定、规则修改更轻、采集链路更可控。

## 重构目标

重构完成后，系统应收敛为下面这条单向依赖链：

- 主站分类骨架 -> 分类映射
- 分类映射 -> 主站主数据分类归属
- 主站主数据 -> 附属站播放补充

并严格满足以下约束：

- 附属站绝不能反向影响分类骨架
- 附属站绝不能触发整库分类重建
- 映射规则变更绝不能触发整库影片派生重建
- 分类变化不应顺带触发 `play_from_summary` 重建
- 启动阶段不应默认执行全量重建型任务

## 当前核心问题

### 1. 标签重建函数职责混杂

当前 `server/internal/repository/film/write_repo.go` 中的 `RebuildSearchTagsByPids` 实际上同时做了三件事：

- 重建 `search_info`
- 重建 `play_from_summary`
- 重建 `search_tag_item`

这导致分类变化会串联刷新播放摘要，职责严重越界。

### 2. 分类重建链路过长

当前 `server/internal/repository/category_repo.go` 中的 `RebuildCategoriesFromSourceCategories()` 在完成分类骨架与映射关系更新后，还会继续触发全根分类标签刷新，间接带起影片搜索数据与播放摘要相关逻辑。

### 3. 启动阶段承担了重建职责

启动流程中曾直接执行派生数据强制重建。即使已经临时改成后台执行，设计上仍然不合理。启动流程应该负责初始化，而不是默认承担全库修复。

### 4. 主站与附属站边界仍需继续收紧

主站负责事实数据写入，附属站负责播放资源补充，这一业务语义已经明确，但代码里仍存在一些链路上的连带影响，需要继续拆开。

## 目标职责分层

### 一层：主站分类骨架层

负责：

- `source_categories`
- `categories`

规则：

- 只允许主站写入 `source_categories`
- 只由主站分类同步或分类重置流程更新骨架
- 不参与播放源摘要逻辑
- 不因附属站采集变化而变化

### 二层：分类映射层

负责：

- `category_mappings`
- `search_info` 中分类归属字段修正

规则：

- 映射只服务于分类归属，不负责播放层数据
- 映射变更只影响 `pid`、`cid`、`c_name`、`root_category_key`、`category_key`
- 映射变更不影响 `movie_playlist`、`movie_detail_info`、`play_from_summary`

### 三层：主站主数据层

负责：

- `search_info`
- `movie_detail_info`
- `movie_match_key`
- 主站影片自身 `play_from_summary`
- 受影响分类的标签增量刷新

规则：

- 日常采集走增量写入
- 不依赖整批回灌式重建作为常规流程
- 全量 rebuild 只保留给修复工具或后台任务

### 四层：附属站播放补充层

负责：

- `movie_playlist`
- 附属站 `movie_source_mapping`
- 相关影片 `play_from_summary`

规则：

- 附属站不允许修改分类骨架
- 附属站不允许修改分类映射
- 附属站不允许触发整类标签重建
- 附属站仅补充播放资源，不改变主数据分类结果

## 分阶段重构计划

### 第一阶段：先减压

目标：先砍掉最明显的职责串联与高 IO 路径。

#### 1. 拆掉 `RebuildSearchTagsByPids` 的混合职责

当前函数应拆成职责明确的多个函数，例如：

- `RefreshSearchInfosByPids(...)`
- `RefreshPlayFromSummaryByMids(...)`
- `RebuildSearchTagsOnlyByPids(...)`

第一阶段的重点不是名字，而是边界：

- 分类链路不能再顺手刷新播放摘要
- 标签链路不能再顺手做整批搜索数据重建

#### 2. 把分类链路和 `play_from_summary` 链路彻底解耦

分类或映射变化后，只允许做：

- 分类树重建
- 映射关系重建
- `search_info` 分类字段修正
- 标签重建

不允许做：

- 播放摘要刷新
- 影片详情回灌
- 播放列表重算

#### 3. 取消启动默认全量派生重建

启动应只负责：

- 表初始化
- 索引检查
- 缓存初始化
- 主站分类同步可后台执行

启动不应默认执行：

- 全量 `search_info` 回灌
- 全量标签重建
- 全量播放摘要重建

### 第二阶段：收紧日常写路径

目标：让主站与附属站写路径各自稳定、各自轻量。

#### 4. 主站采集改成增量事实写入

主站日常采集流程应固定为：

1. 写 `movie_detail_info`
2. 直接生成并 upsert `search_info`
3. 更新 `movie_match_key`
4. 更新该影片自己的 `play_from_summary`
5. 只刷新受影响 `pid` 的标签

说明：

- 日常采集不依赖“先落事实，再整批回灌搜索表”
- 全量 rebuild 保留给后台修复任务

#### 5. 附属站采集收紧成纯播放补充

附属站采集只负责：

- 写 `movie_playlist`
- 维护附属站 `source_mid -> global_mid`
- 刷新对应影片 `play_from_summary`

附属站采集不负责：

- 分类骨架
- 分类映射
- `search_info.pid/cid`
- 标签重建

### 第三阶段：整理后台任务边界

目标：把“重建”拆成几个可控的后台能力，而不是一个全能入口。

#### 6. 分类重建收敛为结构层任务

`RebuildCategoriesFromSourceCategories()` 应只负责：

- 从当前启用主站读取 `source_categories`
- 重建 `categories`
- 重建 `category_mappings`
- 修正 `search_info` 的分类字段
- 重建相关分类标签

不再负责：

- `movie_detail_info` 回灌
- `play_from_summary` 刷新
- 整库派生修复

建议按内部步骤再拆分为：

- `rebuildCategoryTreeFromSource(...)`
- `rebuildCategoryMappingsFromSource(...)`
- `refreshSearchInfoCategoryBindings(...)`
- `rebuildCategoryTags(...)`

#### 7. 把后台重建入口拆分成独立任务

建议最终拆成三类后台任务：

- 分类结构重建
- 播放摘要重建
- 派生搜索数据重建

对应后端能力建议收敛为独立入口，例如：

- `TriggerCategoryStructureRebuildAsync()`
- `TriggerPlaySummaryRebuildAsync()`
- `TriggerSearchDerivedRebuildAsync()`

这样做的目标是：

- 小问题只修小模块
- 避免一个后台入口把整库都带起来
- 让管理端的手动操作更加可预测

## 预计涉及文件

### 核心重构文件

- `server/internal/repository/film/write_repo.go`
- `server/internal/repository/category_repo.go`
- `server/internal/repository/film/playfrom_summary.go`
- `server/internal/repository/film/playlist_repo.go`
- `server/internal/repository/film/derived_refresh.go`
- `server/internal/repository/film/slave_summary_refresh.go`
- `server/internal/service/init_service.go`
- `server/internal/service/collect_service.go`
- `server/internal/spider/Spider.go`

### 可能需要同步检查的文件

- `server/internal/repository/film/admin_repo.go`
- `server/internal/service/film_service.go`
- `server/internal/handler/manage_handler.go`
- `web/src/app/manage/system/mapping-rules/`
- `web/src/app/manage/collect/`
- `web/src/app/manage/film/class/`

## 实施顺序建议

### 第一优先级

1. 拆 `RebuildSearchTagsByPids`
2. 从分类链路中移除 `play_from_summary` 依赖
3. 取消启动默认全量派生重建

### 第二优先级

1. 主站采集完全增量化
2. 附属站采集彻底限定为播放补充层

### 第三优先级

1. 分类重建入口独立
2. 派生重建入口独立
3. 播放摘要重建入口独立

## 预期收益

- 启动时间更稳定
- `mapping-rules` 操作更轻，不再轻易触发长事务
- 分类重建不会再顺手带起播放摘要重建
- 主站与附属站采集职责边界更清楚
- SQL 与 IO 压力更集中、更可预测
- 后续针对单个模块优化 SQL 时，不再被整条链路绑架

## 风险与验证

### 风险

- 拆分后若调用链清理不完整，可能出现某些后台刷新不再触发
- 分类字段与标签逻辑拆开后，若漏掉增量路径，可能短时间出现数据最终一致性问题
- 管理端若仍沿用“一个按钮触发所有重建”的认知，接口与文案也需要同步调整

### 验证重点

- 启动服务时是否仍出现长时间阻塞
- 修改映射规则时是否还会引发播放摘要相关重建
- 主站采集单片/批量时是否只影响对应影片与对应分类
- 附属站采集时是否只更新播放相关数据
- 主站切换后分类骨架、映射关系、主数据分类归属是否仍正确

## 最终目标

最终系统需要稳定收敛为下面这条业务链：

- 主站分类负责骨架
- 映射规则负责分类归属
- 主站采集负责主数据
- 附属站采集负责播放补充

任何一个环节的修改，都不应再无差别打穿到其它层。

这次重构的核心不是“写更少的 SQL”，而是“让不该运行的 SQL 根本不要运行”。
