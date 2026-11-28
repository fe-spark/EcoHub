Release **v3.2.3**, image `ghcr.io/fe-spark/ecohub:v3.2.3` (`:latest` also points to this release). For deployment steps, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/deploy.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x / v3.1.x / v3.2.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- Analytics has been refactored with new Redis keys and API routes; legacy analytics cache keys are not compatible, and new columns are applied on startup.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/v2-to-v3.md).

### v3.2.3 changes

- **Access control**: restrict analytics menu, page, and admin endpoints strictly to super admins, blocking visitors and normal users.
- **Visitor multi-login**: allow concurrent logins on visitor accounts without session kickout; single-device logout preserves other sessions while password, role, disable, or delete changes revoke all.
- **Analytics module**: extract standalone `analytics` package and `/api/manage/analytics/*` routes with daily 96-slot Redis hashes and memory-only write enrichment.
- **Dashboard UI**: rebuild analytics views with ECharts, sticky filter bar, persistent KPI row, and Traffic / Content / Logs workspace tabs.
- **Concurrency & reliability**: wait for rollup shutdown cleanly, guard rollup enablement under lock, chunk batched `IN` queries, and drop stale log responses.

---

Release **v3.2.2**, image `ghcr.io/fe-spark/ecohub:v3.2.2` (`:latest` also points to this release). For deployment steps, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/deploy.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x / v3.1.x / v3.2.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- First startup runs an online migration to add `last_record_id` with its index and drop `collect_record_films`.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/v2-to-v3.md).

### v3.2.2 changes

- **Collect records**: drop `collect_record_films` and the batch-films modal; store single-update IDs in `target_mids`.
- **Telegram notify**: query primary updates directly via `film_index.last_record_id` with a two-level flow.
- **Playlists & memory**: append suffixes to duplicate manual lines and free per-source dedup sets once finished.
- **Admin UI**: streamline collect records to 7 columns, add non-primary alerts, and unify config load locks.

---

Release **v3.2.1**, image `ghcr.io/fe-spark/ecohub:v3.2.1` (`:latest` also points to this release). For deployment steps, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/deploy.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x / v3.1.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/v2-to-v3.md).

### v3.2.1 changes

- **Standalone collect engine**: extract the `engine` core package, remove the fixed `200ms` interval, legacy write scheduler, and related config fields, and reuse probed page-1 items.
- **Adaptive 429 throttling**: honor `Retry-After` headers, deduplicate per-burst window halving by epoch, and apply two-stage dynamic request pacing with gradual recovery.
- **Concurrent write path**: shard page write locks by `(source_id, source_mid)` for same-source parallel writes, skip unchanged pages via lock-free preflight reads, and batch `CASE WHEN` / tuple updates.
- **Orchestration & concurrency fixes**: offload post-write affected-MID fan-out, prevent pre-canceled seed goroutine leaks, and retain `FOR UPDATE` row locks against concurrent film deletion.

---

Release **v3.2.0**, image `ghcr.io/fe-spark/ecohub:v3.2.0` (`:latest` also points to this release). For deployment steps, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/deploy.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x / v3.1.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/v2-to-v3.md).

### v3.2.0 changes

- **Home management & hot-chart sync**: upgrade the banner admin menu into a unified "Home Management" view ("Banners" and "Recommendations") to reorder and toggle built-in sections and custom groups; add Douban & Bangumi ranking charts with random/Douban quick-banner strategies, 6-hour and post-collect chart syncing, first-setup category-group import, fallback `cid` index alignment, and strictly read-only public category reads (protecting rule-root categories and related-movie source filtering).
- **Collect scheduling, ledger & write stability**: overhaul collect concurrency boundaries, epoch validation on displaced exits, and streaming clustered ledger cleanup; restore 16 concurrent write slots without cross-source `matchKey` lock contention; enforce ordered row locking during film writes and orphan cleanup, fix out-of-order empty-page truncation, reseal retry convergence, progress/lock token races, and config-import sync locking, and add the `partial_stopped` batch status.
- **Security hardening, SSRF & private mode**: upgrade passwords to `bcrypt` with transparent MD5 migration and reject whitespace password-change bypasses, replace global username login lockouts with IP / IP+account limiters, tighten SSRF/cross-scheme CORS checks, upload magic-byte/`nosniff` guards, and credential redaction, and enforce private-mode and `/provide` visibility/bounds checks plus subscription QR code support.
- **HTTP policy, MySQL migration lock & server refactor**: reorganize internal server packages by domain with explicit hook wiring, isolate per-source Telegram ledger stats and process callbacks concurrently, isolate MySQL advisory migration lock connections cleanly, and enforce SSE/upload timeouts, maintenance shutdown gates, web UX fixes, and full concurrency/security regression coverage.

---

Release **v3.1.6**, image `ghcr.io/fe-spark/ecohub:v3.1.6` (`:latest` also points to this release). For deployment steps, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/deploy.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x / v3.1.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/v2-to-v3.md).

### v3.1.6 changes

- **Collect notification**: align public-visible summary counts and category buckets with the current primary source, and add multi-source interactive title lists and paginated ledger queries.
- **Play page speed probe**: switch to 2-way concurrent probing of the highest-bandwidth variant, ignore `AVERAGE-BANDWIDTH` interference, widen timeouts, and compute speed from read bytes when partial segments are fetched before timeout.

---

Release **v3.1.5**, image `ghcr.io/fe-spark/ecohub:v3.1.5` (`:latest` also points to this release). For deployment steps, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/deploy.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x / v3.1.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/v2-to-v3.md).

### v3.1.5 changes

- **Category film counts**: category management shows film counts per source. A failed count returns an error instead of a fallback number.
- **Mapping rule preview**: preview now runs in the browser. When several exact-name rules match, only the last one applies.
- **Collect notification**: Telegram is sent immediately when a collect run stores no pages, including when pages were fetched but every save failed and the run was then stopped.
- **Play page**: episode list and source switching are split apart. With more than 100 episodes, reverse order stays on the last group while playback continues. Speed tests run one source at a time.

---

Release **v3.1.4**, image `ghcr.io/fe-spark/ecohub:v3.1.4` (`:latest` also points to this release). For deployment steps, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/deploy.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x / v3.1.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/v2-to-v3.md).

### v3.1.4 changes

- **Private access auth (#111)**: clear stale auth cookies and redirect to the login page with the original return path when an expired or invalid cookie remains; separate auth failures, network errors, and genuine empty libraries in public pages.

---

Release **v3.1.3**, image `ghcr.io/fe-spark/ecohub:v3.1.3` (`:latest` also points to this release). For deployment steps, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/deploy.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x / v3.1.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/v2-to-v3.md).

### v3.1.3 changes

- **Empty library & category guide (#112)**: show the collect-center guide on the homepage and valid category/filter pages when the search index is not ready yet; only report invalid category when the category does not exist or is hidden.

---

Release **v3.1.2**, image `ghcr.io/fe-spark/ecohub:v3.1.2` (`:latest` also points to this release). For deployment steps, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/deploy.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x / v3.1.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/v2-to-v3.md).

### v3.1.2 changes

- **Config import (#109)**: replacing collect sources keeps the live film count for sources that already exist with the same ID; new sources start at 0. Import rejects duplicate source addresses, ignoring host case and a trailing slash. Modules absent from a backup cannot be selected. Search and homepage caches are cleared even when the primary source ID stays the same.
- **Library size (#108, #109)**: workbench totals for films, categories, and today's failed pages now sum every collect source. Incremental count caches are gone; stats recompute asynchronously after collection, with ingesting and computing states in the UI. Refreshes skipped during collection are marked dirty, and single-film collection also refreshes stats.
- **Cross-source matching (#110)**: peer playlists and posters compare normalized root categories instead of each site's local Pid. Missing category names do not match. Names ending in 剧 or 片 are no longer folded into TV or movie automatically.
- **Collection stop**: in-flight probes cancel immediately, fetched pages dropped before queueing are discarded, and stopped sources skip the synchronous publish lock. Tasks are marked stopped before probes are canceled.
- **Admin UI**: the header primary-source selector labels the current primary and fixes alignment; the admin password confirm dialog submits on Enter (#108).

---

Release **v3.1.1**, image `ghcr.io/fe-spark/ecohub:v3.1.1` (`:latest` also points to this release). For deployment steps, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/deploy.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x / v3.1.0: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/v2-to-v3.md).

### v3.1.1 changes

- **Per-source category and banner management (#107)**: category and banner management default to the current primary source and support switching to manage any collect source's category order, visibility, and banners.
- **Hidden categories excluded from peer playback supplement**: films under hidden categories on non-primary sources are no longer attached as peer playback sources in primary film or TVBox details; dynamic playback caches are invalidated on category or rule changes.
- **Search reseal optimization**: large batch reseals reuse verified primary ownership and fall back to sequential full-site rebuild above the threshold to reduce peak memory usage.
- **TMDB, gallery & management refinements**: streamlined TMDB matching by removing background auto-match jobs, unified gallery file deletion checks (delete disk files before DB records), and refactored proxy, notification polling, auth helpers, and admin page shells.

---

Release **v3.1.0**, image `ghcr.io/fe-spark/ecohub:v3.1.0` (`:latest` also points to this release). For deployment steps, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/deploy.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- **The first startup builds the banner score index** `idx_film_index_cat_score`. If the film-list index `idx_film_index_source_updated` is missing, the same startup builds it and recounts each source's film total. A large catalog can make these steps slow. If the recount fails, the previous totals stay. Film rows and collect records are left unchanged. Instances already running `v3.1.0-beta.x` upgrade smoothly.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/v2-to-v3.md).

### v3.1.0 changes

- **Global primary source & search index**: in-memory search index keeps only the current primary source and drops the previous index on switch; completed global primary switching, disable/delete handover validation, admin access enforcement, batch-delete queue cancellation, primary deleting indicator in header selector, and fixed full-page remount flicker on batch delete (#98).
- **Category rules & hidden-category filtering (#106)**: synthetic root groups created by `CategoryRoot` rules use dedicated `film_category` rows and non-colliding public IDs; added "All Groups" rule filter, cascading `CategorySub` merging on demoted root categories, and visible representative selection for partially hidden merged groups; filtered hidden subcategories out of homepage recommendations, category rank pools, and banner candidates.
- **Provide API, TVBox & workbench**: public APIs, lists, and TVBox subscriptions expose only the current primary source with cleaner line sorting; workbench shows primary and per-source film, category, and today's failed page stats.
- **Search, filters & banners**: added simplified/traditional and fullwidth/halfwidth matching plus literal `%`/`_` escaping on non-primary queries; optimized banner candidate queries and skipped films without play lines.
- **Reset & server hardening**: improved large-catalog reset/seed rebuild; added danmaku validation and IP rate limits, SSRF and bounded decompression guards, config redaction, and concurrency/cache safety fixes.

---

Release **v3.0.6**, image `ghcr.io/fe-spark/ecohub:v3.0.6` (`:latest` also points to this release). For deployment steps, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/deploy.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- **Upgrading from v3.0.5 clears collect records on startup**: `collect_records` and `collect_record_films` are dropped and recreated. A database that already ran v3.0.6-beta.2 or later is not cleared again. The film catalog is not stored in these tables and is left unchanged.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/v2-to-v3.md).

### v3.0.6 changes

- **Rate limit**: after 20 page failures in a row, in-flight requests are cancelled and the source pauses for about 2 minutes, then continues the unfinished pages. Pages that already failed are not retried. The count starts over after the pause, and another 20 failures interrupt the round.
- **Collect records**: columns are time, trigger, window, duration, updated films, pages, sources, and status, with auto-refresh. A running round matches the collect center. Film ids updated in the round are saved when it ends.
- **Last collect pages**: the count resets to 0 when a collect starts, then follows this round's successful pages, and stays at the finished count.
- **Page-count probe**: a failure is retried twice. The source is marked failed only after all three attempts fail.

---

Release **v3.0.5**, image `ghcr.io/fe-spark/ecohub:v3.0.5` (`:latest` also points to this release). For deployment steps, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/deploy.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- **v3.0.3 no longer reads `HTTPS_PROXY`, `HTTP_PROXY`, or `ALL_PROXY`**: the three variables have been removed from the compose files and `.env.example`, and values left in an existing `.env` no longer take effect. Deployments that relied on these variables to route danmaku or Telegram traffic through a proxy must, after upgrading, enable the global proxy in "网络代理" (Network proxy) in the admin panel and enable the "弹幕抓取" (Danmaku fetching) or "Telegram 机器人" (Telegram bot) module; otherwise these requests connect directly.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/en/guide/v2-to-v3.md).

### v3.0.5 changes

- **Play page**: when no play source is specified, playback starts on the main source. Speed-based source selection can be turned on to pick a line for the current episode by sustained transfer speed. The sidebar shows the results, a retest is available, and a manual choice is not overwritten. Selection is off by default. A playback failure only reports the error and no longer switches lines automatically (#87, #91).
- **Collect joins an open round**: while a seal is open and a collect queue is still running, a new scheduled or manual collect joins that round and sealing waits until every queue has finished. A new collect is refused when no queue is running. Sources in the same batch that have not started are no longer marked failed just because an earlier source has reached indexing. When a scheduled run overlaps the next tick, one extra round collects the sources that were idle, after the current queue finishes. The refusal states whether the search index is updating or this round is about to be stored, and says that collects already running will finish. A rejected enqueue releases sources it had already reserved (#92).
- **Search index seal**: sealing runs once, only after every collect queue has finished. A full sequential rebuild runs only when the in-memory index is unavailable, empty, or fails to read; all other cases patch incrementally by changed ID. Saving the collect record does not block sealing. A collect round ends only after this round's update succeeds (#92).
- **Write concurrency**: concurrent writes start at 16 and move with recent page latency, down to 2 and capped at 16. A doubled latency or a lock wait removes one writer; a faster window adds one back. One source still writes serially. Page workers and in-flight fetches follow the current writer count.
- **Transient disconnects**: a source that already has successful pages, and then hits consecutive EOF, connection reset, or per-page timeout errors, pauses for about 15 seconds and retries those pages instead of recording a partial failure immediately. Each page is retried at most twice more, and the source takes at most 3 short pauses. Pages that still fail follow the existing consecutive-failure and 5-minute pause rules (#92).
- **Failed pages on the dashboard**: "采集失败页数" (failed collect pages) is the total of pages that failed during the current day in Beijing time, and each source can be expanded. An error in the batch detail can be opened in a dialog that separates the reason, the request URL, and the failed pages. Newly stored errors keep up to 2000 characters of the reason (#95).
