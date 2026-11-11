# Scheduled task conflicts

[中文](./cron-task-conflict.md) | English

## Rule summary

**When a maintenance task is due in the same window, maintenance runs first and collect tasks are skipped. When several maintenance tasks are due at the same time, they are queued and run sequentially.**

## Objects

"计划任务" (Scheduled tasks) in the admin panel contains 4 tasks by default. All default times are in Beijing time (the image time zone is `Asia/Shanghai`):

| Task ID | Name | Type | Default time |
| --- | --- | --- | --- |
| `sys_cron_auto_collect` | 采集已启用站点的更新 (collect updates from enabled sources) | Collect | Every hour on the hour, last 24 hours |
| `sys_cron_orphan_clean` | 清理片库冗余和孤儿数据 (clean redundant and orphan library data) | Maintenance | Daily 02:00 |
| `sys_cron_log_clean` | 清理过期运行日志 (clean expired run logs) | Maintenance | Daily 03:00 |
| `sys_cron_collect_clean` | 定期清理超过 30 天的采集运行记录与快照 (clean collect records and snapshots older than 30 days) | Maintenance | Daily 03:30 |

Task type `0` is collect. All other types (library clean, log clean, collect record clean) are maintenance.

## Rules

1. **Maintenance first.** Tasks due within 1 second are treated as one batch. If the batch contains any maintenance task, all collect tasks in it are skipped. If a maintenance task is already running or queued, a collect task that becomes due is also skipped. Skipped collect tasks are neither queued nor run later.
2. **Maintenance queues.** All maintenance tasks enter a single first-in, first-out queue and are run one at a time by a single worker. The next task starts only after the previous one ends. No task is skipped or dropped. A maintenance task that is already queued or running is not queued again.
3. **Latest configuration is read before running.** When a scheduled maintenance task reaches the front of the queue and has been disabled, it is skipped and the next one runs.
4. **Maintenance stops collect first.** Each maintenance task acquires the "maintenance gate" when it starts: it stops the running collect, waits for in-flight write transactions, the collect run, and its final publish to exit, and then cleans with exclusive access. If another short exclusive operation (such as probing a collect source API) holds the gate, the task waits up to 10 minutes and then raises an alert.
5. **Failures are isolated.** If a maintenance task fails or panics, a "定时任务失败告警" (scheduled task failure alert) is sent for that task only. The remaining tasks in the queue run as usual.
6. **Skipped collects are only logged.** It is not recorded as a collect failure, does not count toward the circuit breaker, sends no collect failure notice, and does not change the last run time.
7. **No cleaning if collect does not stop.** After collect is stopped, if write transactions or the stopped run do not exit within the exit limit, the maintenance run fails, a notification is sent, and cleaning does not start.

The exit limit is the same as the wait used when deleting a collect source: 3 minutes. It applies only to write transactions and runs that have not exited after the stop, and does not include waiting for collect to finish the remaining pages.

The last run time is written only when maintenance actually starts cleaning, or when a scheduled collect actually starts. Queuing, waiting, and skipping do not update it.

A skipped scheduled collect is not run later. The next scheduled collect fills the gap for each collect source from its last successful time, up to 7 days back. Data already stored is not affected.

The maintenance queue is held in memory only. If the service restarts, queued maintenance tasks are lost and run at their next due time.

## Examples

**Collect and maintenance due in the same second.** At 02:00 the library clean and the hourly collect are both due. The collect is skipped and the library clean starts.

**Several maintenance tasks due in the same second.** They are queued in due order; the second runs after the first completes. The collect due in that second is skipped.

**Long-running maintenance overlaps later tasks.** The 02:00 library clean is still running at 03:00:

- The 03:00 hourly collect is skipped.
- The 03:00 log clean is queued and runs as soon as the library clean ends.
- The 03:30 collect record clean queues behind the log clean.
- If the queue is still not empty at 04:00, the 04:00 hourly collect is also skipped.

**Collect is already running when maintenance is due.** The 01:00 hourly collect is still running at 02:00 when the library clean is due (the 02:00 hourly collect in the same second is skipped):

1. The library clean acquires the gate and stops the 01:00 collect. Collect sources that have not started are marked stopped.
2. After the in-flight transactions finish writing, cleaning starts.
3. After cleaning, the 01:00 collect does not resume. The 03:00 hourly collect is also blocked by the log clean. After all maintenance is done, the next hourly collect fills the gap from the last successful time.

**Manual runs.**

- Running a collect manually while maintenance is running or queued returns "维护中，已跳过" (under maintenance, skipped).
- Running another maintenance task manually while one is running queues it and returns "已有维护任务在执行，本任务已排队，前一条结束后自动执行" (a maintenance task is running; this task is queued and runs when the previous one ends). If no maintenance task is running, it starts immediately.
- Manual maintenance runs ignore the task's enabled switch. A disabled maintenance task can still be run manually.
- Running a maintenance task manually while the same task is queued or running returns a message stating that it is already queued or running.
- In the task list, a queued task shows "排队中" (queued).

## Implementation

The related code is in `server/internal/spider/`:

| File | Responsibility |
| --- | --- |
| `spider_cron_dispatch.go` | Merges tasks due within 1 second into a batch, then decides: if the batch has maintenance, or maintenance is already running or queued, all collects are skipped; maintenance is queued in due order |
| `maintenance_queue.go` | Serial maintenance queue: one worker, de-duplicated by task, reads the latest config before running, recovers from panic and moves on |
| `spider_cron.go` | `executeMaintenance`, the start flow shared by the three maintenance tasks; `RunTaskOnceWithStatus`, the manual run entry |
| `collect_lifecycle.go` | Maintenance gate: maintenance stops collect, waits for in-flight writes and runs to exit, then cleans with exclusive access |
| `collect_control.go` | Stopping collect with `StopAllCollect` and `StopCollectSource`, and the 3-minute exit limit `deleteStopTimeout` |

Collect entry: during maintenance (including while queued) no new batch starts. A collect source that has already reached its start point is skipped as "已停止" (stopped) and not recorded as a failure.

Collect is stopped with `StopAllCollect(reason)` (all) or `StopCollectSource(id, reason)` (one source). The log states the reason, for example: yielding to maintenance, user stopped all, user stopped this source, collect source disabled, importing collect source config, clearing the library, process exit. Later dispatches and per-source skips reuse the same reason instead of a generic "stopped all".

## Related documents

[FAQ](./README-FAQ_EN.md) · [Server](../server/README_EN.md) · [Release notes](./RELEASE_EN.md)
