# 采集期间访问超时优化说明

## 背景

本次对话围绕一个线上压力问题展开：

采集运行期间，用户访问 TVBox/影视仓接口以及后台 `web/src/app/manage/collect/` 采集管理页时，容易出现 SQL timeout，系统扛不住并发读写压力。

核心矛盾不是单个接口慢，而是采集写入期间仍有高频访问接口直接查询或聚合大表：

1. 后台采集管理页每 4 秒轮询 `/manage/collect/list`。
2. 旧逻辑会为每个采集站分别查询一次最后采集时间。
3. TVBox 列表访问在缓存未命中时会直接查 `film_index` 并执行分页统计。
4. 采集写入、缓存清理、用户访问同时发生时，MySQL 压力被放大。

本次优化目标是：采集期间后台和 TVBox 仍然可用，但尽量避免高频访问直接压 MySQL 大表。

## 已完成修改

### 1. 管理采集列表减少大表聚合次数

涉及文件：

- `server/internal/service/collect_service.go`

旧逻辑：

`/manage/collect/list` 每次请求都会遍历采集站列表，并按站点逐个查询最后采集时间：

```go
for _, source := range sources {
	var last sql.NullTime
	query := db.Mdb.Model(&model.FilmIndex{})
	if source.Grade == model.SlaveCollect {
		query = db.Mdb.Model(&model.MoviePlaylist{})
	}
	query.Where("source_id = ?", source.Id).Select("MAX(updated_at)").Scan(&last)
}
```

如果有 N 个采集站，每轮页面轮询就会触发 N 次 `MAX(updated_at)`。

新逻辑：

按主站和附属站拆成两个 ID 列表，然后分别进行一次 `GROUP BY source_id` 聚合。

```go
func getLastCollectTimeBySource(sources []model.FilmSource) map[string]*time.Time {
	result := make(map[string]*time.Time, len(sources))
	masterIDs := make([]string, 0, len(sources))
	slaveIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.Grade == model.SlaveCollect {
			slaveIDs = append(slaveIDs, source.Id)
			continue
		}
		masterIDs = append(masterIDs, source.Id)
	}
	fillLastCollectTime(result, db.Mdb.Model(&model.FilmIndex{}), masterIDs)
	fillLastCollectTime(result, db.Mdb.Model(&model.MoviePlaylist{}), slaveIDs)
	return result
}
```

```go
func fillLastCollectTime(result map[string]*time.Time, query *gorm.DB, sourceIDs []string) {
	if len(sourceIDs) == 0 {
		return
	}
	type lastCollectRow struct {
		SourceID string       `gorm:"column:source_id"`
		Last     sql.NullTime `gorm:"column:last_collect_time"`
	}
	var rows []lastCollectRow
	if err := query.
		Select("source_id, MAX(updated_at) AS last_collect_time").
		Where("source_id IN ?", sourceIDs).
		Group("source_id").
		Scan(&rows).Error; err != nil {
		log.Printf("GetLastCollectTimeBySource Error: err=%v", err)
		return
	}
	for _, row := range rows {
		if !row.Last.Valid || row.Last.Time.IsZero() {
			continue
		}
		value := row.Last.Time
		result[row.SourceID] = &value
	}
}
```

效果：

1. 查询次数从 `N 个站点 = N 次大表聚合` 降为最多 `2 次聚合查询`。
2. 后台采集页仍保留最后采集时间。
3. 页面行为不变，但采集期间更不容易因为轮询压垮 MySQL。

### 2. 批量采集选项接口改为轻量查询

涉及文件：

- `server/internal/handler/collect_handler.go`
- `server/internal/service/collect_service.go`
- `server/internal/repository/spider_repo.go`

旧逻辑：

`/manage/collect/options` 会复用 `GetFilmSourceList()`，因此会顺带查询最后采集时间。

新逻辑：

新增轻量查询，只读取已启用采集站。

```go
func GetEnabledCollectSourceList() []model.FilmSource {
	var list []model.FilmSource
	if err := db.Mdb.Where("state = ?", true).Order("grade ASC").Find(&list).Error; err != nil {
		log.Println("GetEnabledCollectSourceList Error:", err)
		return nil
	}
	return list
}
```

```go
func (s *CollectService) GetEnabledFilmSources() []model.FilmSource {
	return repository.GetEnabledCollectSourceList()
}
```

```go
func (h *CollectHandler) GetNormalFilmSource(c *gin.Context) {
	var l []model.FilmTaskOptions
	for _, v := range service.CollectSvc.GetEnabledFilmSources() {
		l = append(l, model.FilmTaskOptions{Id: v.Id, Name: v.Name})
	}
	dto.Success(l, "影视源信息获取成功", c)
}
```

效果：

1. 打开批量采集弹窗不再触发大表最后采集时间查询。
2. 弹窗只需要站点 ID 和名称，因此轻量查询更符合接口职责。
3. 用户体验不变，弹窗更快更稳。

### 3. TVBox 列表缓存覆盖常规分页

涉及文件：

- `server/internal/service/provide_service.go`

旧逻辑：

TVBox 列表缓存只覆盖第一页、无子分类、无筛选的少量请求。

```go
if pg <= 1 && wd == "" && h == 0 && year == "" && area == "" && lang == "" && plot == "" && cid == 0 {
	cacheKey = fmt.Sprintf("%s:%d:C%d:S%s:L%d", config.TVBoxList, t, cid, sort, limit)
}
```

新逻辑：

普通列表分页也纳入 Redis 缓存，缓存 key 带上分类、子分类、页码、排序和分页大小。

```go
if wd == "" && h == 0 && year == "" && area == "" && lang == "" && plot == "" {
	cacheKey = fmt.Sprintf("%s:T%d:C%d:P%d:S%s:L%d", config.TVBoxList, t, cid, pg, sort, limit)
}
```

效果：

1. TVBox 首页、分类页、普通翻页更容易命中 Redis。
2. 采集期间用户翻页不再每次都直接回源 MySQL。
3. 搜索和复杂筛选仍然实时查库，没有盲目扩大缓存范围。
4. 现有 `TVBoxList:*` 清理逻辑可以覆盖新 key 形态。

### 4. 已保留的采集写入调度改动

涉及文件：

- `server/internal/spider/collect_write_scheduler.go`
- `server/internal/spider/collect_write_scheduler_test.go`
- `server/internal/spider/Spider.go`

此前对话中已经完成采集写入调度优化，核心目标是避免多个站点采集时每页数据立即分散写库，而是尽量在批量采集中收集一波后汇总写入。

关键语义：

1. 写入调度器使用全局单 lane。
2. 数据库批次写入并发为 1。
3. 批量采集会注册 active source 集合。
4. 当批量 active 站点都已有 pending 数据或已完成时，再汇总写入。
5. 手动单站点采集不受批量 active 集合限制，即使只有 `pending=1` 也必须能写入。
6. 每个站点 pending 队列上限为 `collectWriteMaxPendingPages = 200`，超过后阻塞等待，不丢数据。

该部分改动用于降低采集写入期间的数据库写入冲击，是本次读接口减压的前置基础。

## 当前用户体验

### 后台采集管理页

访问 `web/src/app/manage/collect/` 时，页面行为保持不变：

1. 正常展示采集站列表。
2. 正常展示启用状态、主站/附属站、采集间隔。
3. 正常展示正在采集的进度。
4. 仍然每 4 秒轮询刷新。
5. 仍然保留最后采集时间。

变化是后端查询更轻，采集期间更不容易转圈、卡死或 SQL timeout。

### 批量采集弹窗

点击批量采集时，接口只加载启用站点选项。

用户看到的功能不变：

1. 仍然选择要采集的启用站点。
2. 仍然设置采集时间范围。
3. 仍然提交批量采集任务。

变化是弹窗打开更快，不再顺手查询大表最后采集时间。

### TVBox / 影视仓

TVBox 访问 `/api/provide/vod` 时：

1. 普通分类列表和翻页更容易命中 Redis。
2. 采集期间浏览旧缓存数据，体验更稳定。
3. 采集完成后清理 TVBox 列表缓存，下一次访问重新生成最新数据。
4. 搜索和复杂筛选仍实时查库。

整体体验是：采集期间 TVBox 和后台仍可用，不拦截访问，不提示“采集中请稍后”，只是后端把高频查询尽量变轻。

## 验证结果

已执行采集写入调度定向测试：

```bash
PORT=8080 JWT_SECRET=test MYSQL_HOST=127.0.0.1 MYSQL_PORT=3306 MYSQL_USER=test MYSQL_DBNAME=test REDIS_HOST=127.0.0.1 REDIS_PORT=6379 go test ./internal/spider -run TestCollectWriteLane -count=1 -timeout=20s -v
```

结果：通过。

已执行后端全量测试：

```bash
PORT=8080 JWT_SECRET=test MYSQL_HOST=127.0.0.1 MYSQL_PORT=3306 MYSQL_USER=test MYSQL_DBNAME=test REDIS_HOST=127.0.0.1 REDIS_PORT=6379 go test ./...
```

结果：通过。

另外执行过代码审查工具，结论是未发现确定的 bug。审查确认现有 `TVBoxList:*` 缓存清理逻辑可以覆盖新的 TVBox 列表缓存 key。

## 当前残余风险

### 1. TVBox 扩大缓存后需要服务级验证

单元测试和代码审查已通过，但 TVBox 缓存属于接口级行为，建议在真实服务环境验证：

1. 普通分类第一页是否正常返回。
2. 普通分类第二页是否正常返回。
3. Redis 是否生成 `EcoHub:TVBox:List:T...` key。
4. 第二次请求是否命中缓存。
5. 采集完成后缓存是否被 `TVBoxList:*` 清理。
6. 清理后下一次访问是否重新生成最新数据。

### 2. 管理采集页仍会碰大表

当前已经从 N 次大表聚合降到最多 2 次聚合，但 `/manage/collect/list` 仍然需要从 `film_index` 和 `movie_playlist` 计算最后采集时间。

如果要彻底隔离，需要进一步引入轻量状态表或缓存。

### 3. TVBox 冷缓存仍可能查库

当前方案是扩大 Redis 缓存，不是物化 TVBox 列表。

因此冷门分类、首次翻页、搜索、复杂筛选仍可能直接访问 MySQL。

## 更合理的后续方向

### 1. 新增采集站统计表

建议新增轻量状态表，例如：

```go
type CollectSourceStat struct {
	SourceID        string    `gorm:"primaryKey"`
	LastCollectTime time.Time `gorm:"index"`
	UpdatedAt       time.Time
}
```

采集写入完成后更新统计表，后台采集列表只读：

```text
film_sources + collect_source_stats + 内存 progress
```

这样 `/manage/collect/list` 就完全不需要访问 `film_index` 或 `movie_playlist`。

### 2. TVBox 缓存版本化

建议把 TVBox 缓存从“清理旧 key 后回源重建”改成“版本化切换”：

```text
EcoHub:TVBox:List:Version = v123
EcoHub:TVBox:List:v123:T1:C0:P1:S:L20
EcoHub:TVBox:List:v124:T1:C0:P1:S:L20
```

理想流程：

1. 采集开始时旧缓存继续服务用户。
2. 采集写入数据库。
3. 采集完成后生成新版本缓存。
4. 原子切换当前版本号。
5. 延迟清理旧版本。

这样可以避免采集完成瞬间清空缓存导致大量 TVBox 请求同时回源 MySQL。

### 3. TVBox 列表物化表

如果 TVBox 访问量较大，建议新增专用轻表，例如：

```go
type TVBoxVodListItem struct {
	ID              uint   `gorm:"primaryKey"`
	Mid             int64  `gorm:"uniqueIndex"`
	TypeID          int64  `gorm:"index"`
	RootTypeID      int64  `gorm:"index"`
	Name            string `gorm:"index"`
	Picture         string
	Remarks         string
	PlayFromSummary string
	UpdateStamp      int64 `gorm:"index"`
}
```

TVBox 列表只查询该轻表，避免冷缓存时直接扫 `film_index`。

### 4. 产品体验策略

建议明确采集期间的访问策略：

1. 后台管理页展示实时采集进度。
2. TVBox 展示上一轮稳定数据。
3. 采集成功后再切换到新数据。
4. 采集失败时保留旧缓存，不影响用户观看。

这种策略比“采集中所有访问都实时读最新库数据”更稳定。

## 本次涉及文件汇总

后端修改：

- `server/internal/service/collect_service.go`
- `server/internal/handler/collect_handler.go`
- `server/internal/repository/spider_repo.go`
- `server/internal/service/provide_service.go`

此前采集写入调度相关修改：

- `server/internal/spider/collect_write_scheduler.go`
- `server/internal/spider/collect_write_scheduler_test.go`
- `server/internal/spider/Spider.go`

前端仅用于定位访问链路，本次未修改前端文件。

## 总结

本次优化的核心是把采集期间的访问压力从“高频直接打大表”改成“少量聚合查询 + 更高 Redis 命中 + 轻量接口职责”。

当前方案属于低风险增量优化：

1. 不改变后台页面交互。
2. 不改变 TVBox 接口协议。
3. 不禁止采集期间访问。
4. 不引入新依赖。
5. 后端测试已通过。

后续如果要进一步提高稳定性，优先建议实现 `collect_source_stats`，彻底让后台采集页不再读取影片大表；再做 TVBox 缓存版本化和 TVBox 列表物化表。
