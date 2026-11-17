Test release **v3.0.7-beta.3**, image `ghcr.io/fe-spark/ecohub:v3.0.7-beta.3`. A hyphenated prerelease tag does not move `:latest`. The stable release remains v3.0.6 below.

### Upgrade notes

- Test install: set `ECOHUB_IMAGE` to `ghcr.io/fe-spark/ecohub:v3.0.7-beta.3` in the install directory, then run `docker compose pull && docker compose up -d`. `ecohub update` pulls the image already set in `.env` and does not switch to this test tag on its own.
- **The first startup builds the film-list index** `idx_film_index_source_updated` and recounts each source's film total. A large catalog can make these steps slow. If the recount fails, the previous totals stay. Film rows and collect records are left unchanged.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3_EN.md).

### v3.0.7-beta.3 changes

- **Film list**: a non-primary source is paged by owner and update time, instead of looking up every play-line id. On local catalogs of about 70k to 170k films, the first page returns in tens of milliseconds.
- **Title search**: a Chinese query on a non-primary source filters by each character, then scores, and keeps the 200 best hits. A pinyin query still reads that source's titles.
- **Primary switch**: if warmup sees that the primary already changed, or the rebuild fails, the current primary index is built before the switch is marked finished.

---

### v3.0.7-beta.2

Test release **v3.0.7-beta.2**, image `ghcr.io/fe-spark/ecohub:v3.0.7-beta.2`.

### Upgrade notes

- Test install: set `ECOHUB_IMAGE` to `ghcr.io/fe-spark/ecohub:v3.0.7-beta.2` in the install directory, then run `docker compose pull && docker compose up -d`. `ecohub update` pulls the image already set in `.env` and does not switch to this test tag on its own.
- **The first startup recounts each source's film total.** A large catalog can make this step slow. Film rows and collect records are left unchanged.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3_EN.md).

### v3.0.7-beta.2 changes

- **Startup**: `film_index` no longer gains a `(first_source_id, deleted_at)` index during migration. On a catalog of about a million rows, beta.1 spent longer than MySQL's 30-second read timeout building that index and the process exited. The next start then failed because the index name already existed. Film totals still live on the collect source.

---

### v3.0.7-beta.1

Test release **v3.0.7-beta.1**, image `ghcr.io/fe-spark/ecohub:v3.0.7-beta.1`.

### v3.0.7-beta.1 changes

### Upgrade notes

- Test install: set `ECOHUB_IMAGE` to `ghcr.io/fe-spark/ecohub:v3.0.7-beta.1` in the install directory, then run `docker compose pull && docker compose up -d`. `ecohub update` pulls the image already set in `.env` and does not switch to this test tag on its own.
- **The first startup recounts each source's film total.** A large catalog can make this step slow. Film rows and collect records are left unchanged.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3_EN.md).

### v3.0.7-beta.1 changes

- **Search index**: memory keeps only the current primary source. Switching the primary drops the previous index immediately, and another switch or source edit is rejected until the rebuild finishes. If the ownership query fails, that reseal is skipped: unfiltered ids are not written into the index, and the failure does not trigger a full rebuild (#98).
- **Workbench**: the top card shows the current primary source's films, categories, and today's failed pages. Expand it to see the same three numbers for every source. Film totals live on the source row, so the expand request does not scan the film table. After a primary switch, the card waits for the sort request before refreshing.
- **Provide API and TVBox**: categories, lists, and the subscription expose only the current primary source. The subscription name is the site name. Playback detail still includes lines from the other sources.

---

Release **v3.0.6**, image `ghcr.io/fe-spark/ecohub:v3.0.6` (`:latest` also points to this release). For image tags, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/README-Deploy_EN.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- **Upgrading from v3.0.5 clears collect records on startup**: `collect_records` and `collect_record_films` are dropped and recreated. A database that already ran v3.0.6-beta.2 or later is not cleared again. The film catalog is not stored in these tables and is left unchanged.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3_EN.md).

### v3.0.6 changes

- **Rate limit**: after 20 page failures in a row, in-flight requests are cancelled and the source pauses for about 2 minutes, then continues the unfinished pages. Pages that already failed are not retried. The count starts over after the pause, and another 20 failures interrupt the round.
- **Collect records**: columns are time, trigger, window, duration, updated films, pages, sources, and status, with auto-refresh. A running round matches the collect center. Film ids updated in the round are saved when it ends.
- **Last collect pages**: the count resets to 0 when a collect starts, then follows this round's successful pages, and stays at the finished count.
- **Page-count probe**: a failure is retried twice. The source is marked failed only after all three attempts fail.

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
