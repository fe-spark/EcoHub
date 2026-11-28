# Scheduled Task Conflicts

[中文](../../dev/cron-task-conflict.md) | English

## Rule summary

**When a maintenance task is due in the same window, maintenance runs first and collect tasks are skipped. When several maintenance tasks are due at the same time, they are queued and run sequentially.**

## Objects

"计划任务" (Scheduled tasks) in the admin panel contains 4 tasks by default. All default times are in Beijing time (image time zone `Asia/Shanghai`):

| Task ID | Name | Type | Default time |
| --- | --- | --- | --- |
| `sys_cron_auto_collect` | 采集已启用站点的更新 (collect updates from enabled sources) | Collect | Every hour on the hour, last 24 hours |
| `sys_cron_orphan_clean` | 清理片库冗余和孤儿数据 (clean redundant and orphan library data) | Maintenance | Daily 02:00 |
| `sys_cron_log_clean` | 清理过期运行日志 (clean expired run logs) | Maintenance | Daily 03:00 |
| `sys_cron_collect_clean` | 定期清理超过 30 天的采集运行记录与快照 (clean collect records older than 30 days) | Maintenance | Daily 03:30 |

Task type `0` is collect. All other types (library clean, log clean, collect record clean) are maintenance.

## Rules

1. **Maintenance first.** Tasks due within 1 second are treated as one batch. If the batch contains any maintenance task, all collect tasks in it are skipped. If a maintenance task is already running or queued, a collect task that becomes due is also skipped. Skipped collect tasks are neither queued nor run later.
2. **Maintenance queues.** All maintenance tasks enter a single FIFO queue and are run sequentially by one worker without skipping or dropping; duplicate queued/running tasks are ignored.
3. **Latest configuration is read before running.** If a scheduled maintenance task has been disabled by the time it reaches execution, it is skipped and the next task proceeds.
4. **Maintenance stops collect first.** Each maintenance task acquires the maintenance gate: it stops running collects, waits for in-flight write transactions, runs, and final publish to exit, and then cleans with exclusive access. If another short exclusive operation holds the gate, it waits up to 10 minutes before alerting.
5. **Failures are isolated.** If a maintenance task fails or panics, an alert is sent only for that task and later queued tasks proceed normally.
6. **Skipped collects are only logged.** They do not count as failures, circuit-breaker strikes, failure notices, or last-run updates.
7. **No cleaning if collect does not stop.** If stopped writes/runs fail to exit within the timeout, maintenance fails with an alert without starting cleanup.

The exit limit matches source-deletion wait time: `3` minutes for in-flight writes/runs to exit (without waiting for remaining pages).

The last-run time updates only when maintenance actually begins cleaning or a scheduled collect actually begins running. Skipped collects are made up on the next run based on each source's last success time, up to `7` days back.

The maintenance queue is in-memory only and resets on restart.

## Examples

**Collect and maintenance due in the same second.** At 02:00 both library clean and hourly collect trigger: collect is skipped and library clean runs.

**Several maintenance tasks due in the same second.** They enqueue in order and run serially; collect in that second is skipped.

**Long-running maintenance overlaps later tasks.** At 03:00 while 02:00 library clean is still running:

- 03:00 hourly collect is skipped.
- 03:00 log clean enqueues and runs right after library clean finishes.
- 03:30 collect record clean queues behind log clean.
- If the queue is still occupied at 04:00, 04:00 collect is also skipped.

**Collect already running when maintenance triggers.** At 02:00 while 01:00 collect is still running:

1. Library clean acquires the gate and stops the 01:00 collect; unstarted sources are marked stopped.
2. Once in-flight write transactions finish, cleaning starts.
3. After cleaning, the 01:00 collect does not resume; the next hourly collect catches up from the last success time.

**Manual runs.**

- Triggering collect manually while maintenance runs or queues returns "维护中，已跳过" (under maintenance, skipped).
- Triggering a second maintenance task manually queues it (or runs immediately when idle), ignoring the task's enabled switch; duplicate triggers for the same queued/running task return a notice, and queued tasks display "排队中" (queued).

## Implementation

Code lives under `server/internal/collect/`:

| File | Responsibility |
| --- | --- |
| `spider_cron_dispatch.go` | Batches tasks within 1 second; skips all collects when maintenance is present, running, or queued |
| `maintenance_queue.go` | Serial FIFO maintenance worker with deduplication, fresh config reads, and panic recovery |
| `spider_cron.go` | Shared `executeMaintenance` flow and manual entry `RunTaskOnceWithStatus` |
| `collect_lifecycle.go` | Maintenance gate: stops collect, waits for in-flight writes/runs, then executes cleanup exclusively |
| `collect_control.go` | `StopAllCollect`, `StopCollectSource`, and 3-minute exit timeout `deleteStopTimeout` |

Stops carry explicit reasons (`StopAllCollect(reason)` / `StopCollectSource(id, reason)`) logged per skip instead of generic messages.

## Related documents

[Core mechanics and data flow](./architecture.md) · [Server](../../../server/README_EN.md) · [Release notes](../RELEASE.md)
