Test release **v3.0.6-beta.3**, image `ghcr.io/fe-spark/ecohub:v3.0.6-beta.3`. A hyphenated prerelease tag does not move `:latest`. The stable release remains v3.0.5 below. The v3.0.6-beta.2 image did not build; use this tag.

### Upgrade notes

- Test install: set `ECOHUB_IMAGE` to `ghcr.io/fe-spark/ecohub:v3.0.6-beta.3` in the install directory, then run `docker compose pull && docker compose up -d`. `ecohub update` pulls the image already set in `.env` and does not switch to this test tag on its own.
- **This release clears collect records on startup**: `collect_records` and `collect_record_films` are dropped and recreated. Previous collect records are not kept. The film catalog is not stored in these tables and is left unchanged.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3_EN.md).

### v3.0.6-beta.3 changes

- **Collect record page**: source status now includes rate-limited, stopping, and waiting for search, so the frontend build completes.

### v3.0.6-beta.2 changes

- **Pause after consecutive failures**: when one source fails to fetch 20 pages in a row, that source pauses for about 2 minutes and then continues the pages that are still unfinished. It does not take new pages while paused. If the queue is already empty, it does not wait out the full 2 minutes. The collect center shows rate-limited during that pause.
- **Collect records do not block writes**: page fetches and search-index sealing no longer write the collect record. A failure to open the record does not cancel the collect.
- **Store every updated film id after the round ends**: after the round finishes writing films, every film id updated in that round is stored on the collect record, including a full collect. Rows are written 2000 at a time. If one batch fails, the ids already written for that round are deleted and the record stays running. If the process restarts mid-write, the stored row count is used.
- **Collect record page**: columns are collect time, updated films, result, and actions. Auto-refresh can be 5, 10, 15, or 30 seconds, default 10, and a manual refresh is available. Refresh pauses while the page is hidden and runs immediately when it becomes visible again. While a record is running, each source status matches the collect center. After it finishes, the saved record is shown.
- **Updated films**: search matches the start of the title inside this batch only. Previous and next pages use a cursor, and the total is the number of matches in this batch.

---

Release **v3.0.5**, image `ghcr.io/fe-spark/ecohub:v3.0.5` (`:latest` also points to this release). For image tags, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/README-Deploy_EN.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- **v3.0.3 no longer reads `HTTPS_PROXY`, `HTTP_PROXY`, or `ALL_PROXY`**: the three variables have been removed from the compose files and `.env.example`, and values left in an existing `.env` no longer take effect. Deployments that relied on these variables to route danmaku or Telegram traffic through a proxy must, after upgrading, enable the global proxy in "网络代理" (Network proxy) in the admin panel and enable the "弹幕抓取" (Danmaku fetching) or "Telegram 机器人" (Telegram bot) module; otherwise these requests connect directly.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3_EN.md).

### v3.0.5 changes

- **Play page**: when no play source is specified, playback starts on the main source. Speed-based source selection can be turned on to pick a line for the current episode by sustained transfer speed. The sidebar shows the results, a retest is available, and a manual choice is not overwritten. Selection is off by default. A playback failure only reports the error and no longer switches lines automatically (#87, #91).
- **Collect joins an open round**: while a seal is open and a collect queue is still running, a new scheduled or manual collect joins that round and sealing waits until every queue has finished. A new collect is refused when no queue is running. Sources in the same batch that have not started are no longer marked failed just because an earlier source has reached indexing. When a scheduled run overlaps the next tick, one extra round collects the sources that were idle, after the current queue finishes. The refusal states whether the search index is updating or this round is about to be stored, and says that collects already running will finish. A rejected enqueue releases sources it had already reserved (#92).
- **Search index seal**: sealing runs once, only after every collect queue has finished. A full sequential rebuild runs only when the in-memory index is unavailable, empty, or fails to read; all other cases patch incrementally by changed ID. Saving the collect record does not block sealing. A collect round ends only after this round's update succeeds (#92).
- **Write concurrency**: concurrent writes start at 16 and move with recent page latency, down to 2 and capped at 16. A doubled latency or a lock wait removes one writer; a faster window adds one back. One source still writes serially. Page workers and in-flight fetches follow the current writer count.
- **Transient disconnects**: a source that already has successful pages, and then hits consecutive EOF, connection reset, or per-page timeout errors, pauses for about 15 seconds and retries those pages instead of recording a partial failure immediately. Each page is retried at most twice more, and the source takes at most 3 short pauses. Pages that still fail follow the existing consecutive-failure and 5-minute pause rules (#92).
- **Failed pages on the dashboard**: "采集失败页数" (failed collect pages) is the total of pages that failed during the current day in Beijing time, and each source can be expanded. An error in the batch detail can be opened in a dialog that separates the reason, the request URL, and the failed pages. Newly stored errors keep up to 2000 characters of the reason (#95).
