# 采集记录重构

采集记录中间照常写库。页面不再实时查库，也不再实时更新。只在打开页面时读一次，在批次结束时收一次最终结果。

## 现在的问题

记录页开着一条 SSE，`/api/manage/collect/history/stream`。服务端每收到一次采集进度变化，就调 `GetActiveBatchesLiveProgress` 把内存里的实时快照推给页面，事件是 `record_progress`。页面随这条流不断改写表格和明细。

库里的数字来自另一条路。`maybeFlushRecord` 每 2 秒、或每攒满 200 部影片刷一次检查点，写 `collect_record` 和影片清单。两条路节奏不同，所以记录页和采集中心对不上。

采集中心看的是内存里的实时进度。一个站抓完页进入封口，状态是 `sealing`，卡片显示「待检索」。

记录页看的是 2 秒前刷进库的快照。`settleSourceStatus` 把 `sealing` 折成 `done`，这站早先又记过错误，于是有成功页就标成「部分失败」。一个已经抓完、正在封口的站，记录里显示的是它刚开始抓、连续超时那一刻的页数。

## 改成什么样

记录页只在两个时刻更新。

1. 打开页面或翻页时，`/history/list` 读一次库。运行中的批次显示上次刷进库的数字，状态就是「采集中」。不展开实时页数，不展开实时更新的影片。
2. 批次结束时，`persistCollectRecord` 汇总一次写入库，同时发一条 `record_finished`。页面收到后把这一行换成最终结果：成功、部分失败、失败、已停止，带上总页数、失败页和影片数。

运行过程中不再有 `record_progress`。页面不再跟着每一页的变化重绘。

## 改动

### 后端

`server/internal/handler/collect_record_handler.go` 的 `HistoryStream`：

- 去掉 `progress.Subscribe`，以及循环里每次进度变化都调用 `GetActiveBatchesLiveProgress` 再发 `record_progress` 的那段。
- 流只保留 `completionCh` 的 `record_finished`，和 15 秒一次的心跳。
- 连接建立时发一次 `record_snapshot`，只告诉页面哪些批次还在跑，不附带进度数字。

`GetActiveBatchesLiveProgress` 没有别的调用方后删除。

### 前端

`web/src/app/manage/collect/record/view/use-record-stream.ts`：

- 删除 `onProgress`。
- `record_snapshot` 只用来给运行中的行标「采集中」。
- `record_finished` 保持不变，收到后替换该行。

记录页的明细：

- 运行中只显示「采集中」，不显示半截的页数、失败页和更新影片。
- 页数、失败页、更新影片只在批次结束后展示。

### 写库不动

`maybeFlushRecord`、`writeCheckpoint`、影片清单照常写。它们的用途是崩溃恢复和结束后的汇总依据，不再被页面实时消费。

### 状态折算只在结束时用

`settleSourceStatus` 只在 `persistCollectRecord` 里用。中间的检查点不再把 `sealing` 写成 `done`。结束后的「部分失败」是最终结果，不会在采集过程中提前出现。

## 不变的部分

采集中心继续显示实时进度、待检索、限流中。那是它的职责，不在这次改动里。

服务重启的兜底不变。`MarkInterruptedCollectRecords` 仍把残留的 `running` 标成 `interrupted`。中途写库还在，中断前已经入库的页数和影片清单都还在。

## 效果

采集记录变成一份账单。打开时看到上次落库的状态，结束时定格成最终结果。采集中心继续看实时进度。两边不再互相矛盾，记录页也不再每采集一页就刷新一次。
