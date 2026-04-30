# 采集派生刷新重构方案

## 目标

本文用于收敛当前 `DerivedRefresh` / `SlaveSummaryRefresh` 相关设计，使其符合 `README-COLLECT-REFACTOR.md` 的新模型约束。

目标只有三条：

1. 主站采集写入时直接产出来源分类身份和基础展示事实，不再依赖收尾派生修正资源数据
2. 附属站只负责播放源补充，不参与分类与展示结果修正
3. 派生刷新只保留真正必要的附加投影与缓存失效能力

一句话定义本次重构：

**删除主站“事后回写资源分类”的派生刷新，保留附属站“播放摘要补充”的最小刷新链路。**

## 现状判断

### 当前 `DerivedRefresh` 的职责

当前主站收尾阶段的 `DerivedRefresh` 实际负责：

1. 按 `sourceID` 聚合待刷新 `pid`
2. 清理分类页、标签、首页、provide 等缓存
3. 重建 `search_info.play_from_summary`
4. 重建 `search_tag_item`

这代表当前主站链路仍然存在：

1. 先写事实
2. 再通过收尾任务补齐派生数据
3. 依赖任务完成后系统才完全收敛

这与目标模型冲突，因为新模型要求主站采集时直接写出来源分类身份和基础展示事实，不允许依赖事后回写资源分类字段。

### 当前 `SlaveSummaryRefresh` 的职责

当前附属站收尾阶段的 `SlaveSummaryRefresh` 只负责：

1. 按 `sourceID` 聚合待刷新 `mid`
2. 重建对应影片的 `play_from_summary`

这个职责基本符合新模型，因为附属站允许补充播放列表并刷新播放源摘要。

## 重构原则

本次重构必须严格遵守以下原则：

1. 不保留旧主站派生修正链路作为兜底
2. 不保留“主站先写半成品，再收尾回写资源分类”的模型
3. 不让 `search_tag_item` 反向驱动主展示链路
4. 不让附属站进入分类、规则、展示结果修正流程
5. 不为过渡期引入双写或兼容分支

## 分类规则与展示筛选边界

分类规则变更后，不回写 `search_info.pid/cid`、`movie_detail_info` 或影片主体字段。

但分类规则必须允许刷新 `category_mappings` / `cacheSourceMap`，因为它们表示“来源分类身份 -> 当前展示分类”的映射关系。

展示筛选不得只依赖固化的 `pid/cid`。对于采集数据，应通过 `search_info.category_key` / `root_category_key` 与最新 `category_mappings` 关联查询，使所有资源不分新旧都按当前分类规则展示。

因此，`RefreshFutureCategoryMappingsFromSourceCategories()` 不属于主站资源数据回写，也不属于 `DerivedRefresh` 的事后补正确性链路，应作为分类规则保存后的同步映射刷新能力保留。

对应约束：

1. 不得把分类查询退化为只查 `search_info.pid/cid`
2. 不得删除 `category_key` / `root_category_key` 与 `category_mappings` 的查询链路
3. 不得把 `RefreshFutureCategoryMappingsFromSourceCategories()` 归类为需要删除的主站派生刷新

## 新职责划分

### 主站

主站写路径必须一次完成：

1. 写入 `movie_detail_info`
2. 解析来源分类身份
3. 写入来源根分类与子分类稳定身份
4. 直接生成基础展示字段并写入 `search_info`
5. 写入必要的版本信息：`category_version`、`rule_version`
6. 执行最小必要的缓存失效

主站禁止再做的事情：

1. 通过 `DerivedRefresh` 回写资源分类字段
2. 通过异步批量任务重建资源分类归属
3. 通过标签重建修正主站分类语义

### 附属站

附属站只允许负责：

1. 写入 `movie_playlist`
2. 维护 `movie_source_mapping`
3. 刷新对应影片的播放来源摘要

附属站禁止负责：

1. 分类匹配
2. 规则计算
3. `search_info` 分类字段修正
4. 标签重建
5. 资源展示事实修正

### 标签与缓存

`search_tag_item` 必须降级为：

1. 缓存层或附加投影
2. 失败不会影响主站展示正确性
3. 不再作为主展示链路的必要前置条件

缓存层只能承担：

1. 加速查询
2. 附加统计
3. 非主链路辅助展示

不能承担：

1. 主站资源分类字段修正
2. 资源数据纠偏

## 对 `DerivedRefresh` 的处理

### 结论

`DerivedRefresh` 不应继续保留为“主站派生总刷新器”。

它当前承担的职责必须拆分并收敛。

### 必须删除的旧语义

以下语义必须删除：

1. 主站写入后等待收尾任务回写资源分类字段
2. 主站分类或标签正确性依赖收尾刷新
3. 把 `DerivedRefresh` 视为主站展示链路的一部分

### 保留能力的处理方式

`DerivedRefresh` 当前包含的能力应拆为两类：

1. 主链路必需能力：前移到主站写路径
2. 附加能力：保留为最小缓存失效或可选投影刷新

具体建议如下。

#### 前移到主站写路径

以下能力不得再留在收尾刷新里：

1. 来源分类身份写入
2. 基础展示字段生成
3. `search_info` 资源事实落库

这些必须在主站采集写库事务内或紧邻事务后的同步流程中直接完成。

#### 可保留为附加能力

以下能力可以保留，但必须降级：

1. 局部缓存失效
2. `search_tag_item` 投影刷新

并且必须满足：

1. 即使刷新失败，主站资源事实仍然正确
2. 即使不执行刷新，资源分类字段也不会被回写或纠偏
3. 标签投影和缓存刷新不得占用采集站点并发槽，不得阻塞后续站点派发

### 命名建议

删除 `DerivedRefresh` 这个总称，改成语义明确的能力函数，例如：

1. `InvalidateSearchInfoCachesByPids`
2. `RefreshSearchTagProjectionByPids`

如果主站仍然需要刷新 `play_from_summary`，也不应通过一个笼统的“派生刷新器”承担，而应使用单独的播放摘要刷新能力。

## 对 `SlaveSummaryRefresh` 的处理

### 结论

`SlaveSummaryRefresh` 可以保留，但必须严格收缩为附属站播放摘要刷新器。

### 允许保留的原因

它符合新模型对附属站的定义：

1. 附属站写播放列表
2. 附属站补来源映射
3. 附属站刷新播放源相关摘要

### 收缩要求

`SlaveSummaryRefresh` 未来只允许做一件事：

1. 根据受影响 `mid` 刷新 `play_from_summary`

不得继续扩张为：

1. 分类刷新器
2. 标签刷新器
3. 搜索展示结果修正器

### 命名建议

建议改名以突出真实职责，例如：

1. `SlavePlaySummaryRefresh`
2. `ScheduleSlavePlaySummaryRefresh`
3. `FlushPendingSlavePlaySummaryRefresh`

这样可以避免未来继续把附属站刷新器当作泛用派生任务入口。

## 推荐的新流程

### 主站写入流程

主站采集写路径应收敛为：

1. 保存 `movie_detail_info`
2. 计算内容身份与来源分类身份
3. 写入来源根分类与子分类稳定身份
4. 直接构造基础 `search_info`
5. 直接写入 `search_info`
6. 执行最小缓存失效
7. 按需刷新附加标签投影

最终要求：

1. 主站数据写完即可展示
2. 不依赖收尾修正任务回写资源分类字段
3. 当前展示分类由 `category_mappings` 在查询时解释，所有资源不分新旧都按当前规则展示

### 附属站写入流程

附属站采集写路径应收敛为：

1. 保存 `movie_playlist`
2. 保存 `movie_source_mapping`
3. 聚合受影响 `mid`
4. 刷新 `play_from_summary`

最终要求：

1. 附属站只影响播放源摘要
2. 附属站不触碰展示分类结果
3. 播放摘要刷新失败只影响播放源摘要新鲜度，不得触发分类、标签或 `search_info` 分类字段修正

## 重构步骤

### 第一阶段：切断主站对 `DerivedRefresh` 的依赖

目标：主站写入完成后，`search_info` 已经包含稳定来源分类身份和基础展示事实。

执行项：

1. 梳理主站写路径中所有依赖收尾修正才能写入的资源事实字段
2. 将这些字段计算前移到主站写入流程
3. 删除主站收尾阶段对资源分类回写的依赖

验收标准：

1. 禁用 `DerivedRefresh` 后，主站资源仍可通过当前分类映射正常展示

### 第二阶段：拆解 `DerivedRefresh`

目标：只保留最小附加能力。

执行项：

1. 删除 `DerivedRefresh` 统一调度器
2. 拆成局部缓存失效与可选标签投影刷新
3. 移除“主站派生总刷新”语义

验收标准：

1. 代码中不再存在主站资源分类字段依赖 `DerivedRefresh` 回写的路径

### 第三阶段：收紧附属站刷新器

目标：保留附属站播放摘要刷新，但禁止职责扩张。

执行项：

1. 将 `SlaveSummaryRefresh` 改名为更精确的播放摘要刷新器
2. 校验附属站写路径只更新 playlist、mapping、summary
3. 删除附属站触发分类或标签刷新的任何代码

验收标准：

1. 附属站采集不会改写主站展示分类结果
2. 附属站采集只影响播放源相关展示

### 第四阶段：清理残留旧模型

目标：彻底移除“事后回写资源数据”的旧思维。

执行项：

1. 删除与主站收尾修正绑定的旧函数
2. 删除与规则修改后回写资源分类字段有关的触发链路
3. 清理误导性命名与注释

验收标准：

1. 系统中不再保留“主站先写半成品，再异步回写资源分类字段”的链路

## 建议的代码层落点

### 应该保留的能力

1. 主站同步写入最终 `search_info`
2. 附属站按 `mid` 刷新 `play_from_summary`
3. 最小缓存失效
4. 标签投影的可选刷新

### 应该删除的能力

1. 主站 `ScheduleDerivedRefresh`
2. 主站 `FlushPendingDerivedRefresh`
3. 主站按 `sourceID` 聚合等待收尾回写资源分类字段的模型

### 应该改名的能力

1. `SlaveSummaryRefresh` -> `SlavePlaySummaryRefresh`
2. `ScheduleSlaveSummaryRefresh` -> `ScheduleSlavePlaySummaryRefresh`
3. `FlushPendingSlaveSummaryRefresh` -> `FlushPendingSlavePlaySummaryRefresh`

## 风险提示

本次重构的主要风险不在附属站，而在主站写路径。

需要重点关注：

1. 当前哪些前台字段仍隐式依赖 `search_tag_item`
2. 当前哪些接口仍直接依赖 `play_from_summary`
3. 主站写入事务里是否已经具备生成基础 `search_info` 和来源分类身份的全部输入
4. 缓存是否能从粗粒度全清收敛为最小影响面失效

如果上述边界不先梳理清楚，容易出现：

1. 删除 `DerivedRefresh` 后前台局部能力退化
2. 主站来源分类身份写入不完整，导致新的隐性不一致

## 最终结论

按 `README-COLLECT-REFACTOR.md` 的新模型，结论必须明确为：

1. `DerivedRefresh` 不再合理，必须拆除其“主站结果收尾修正器”角色
2. `SlaveSummaryRefresh` 基本合理，但必须收紧为“附属站播放摘要刷新器”
3. 主站采集必须同步写出来源分类身份和基础展示事实
4. 附属站只允许补充播放源，不允许进入分类与展示结果修正链路

本次重构完成后，系统应只保留以下固定约束：

1. 主站直接产出最终展示数据
2. 所有资源不分新旧都按当前规则展示，但资源数据不因规则修改被回写
3. 附属站只补播放源
4. 派生刷新只承担附加投影与缓存职责，不再承担主结果修正职责
