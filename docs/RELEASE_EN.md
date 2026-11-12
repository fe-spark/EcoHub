Release **v3.0.5**, image `ghcr.io/fe-spark/ecohub:v3.0.5` (`:latest` also points to this release). For image tags, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/README-Deploy_EN.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- **v3.0.3 no longer reads `HTTPS_PROXY`, `HTTP_PROXY`, or `ALL_PROXY`**: the three variables have been removed from the compose files and `.env.example`, and values left in an existing `.env` no longer take effect. Deployments that relied on these variables to route danmaku or Telegram traffic through a proxy must, after upgrading, enable the global proxy in "网络代理" (Network proxy) in the admin panel and enable the "弹幕抓取" (Danmaku fetching) or "Telegram 机器人" (Telegram bot) module; otherwise these requests connect directly.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3_EN.md).

### v3.0.5 changes

- **Play page**: when no play source is specified, playback starts on the main source. Speed-based source selection can be turned on to pick a line for the current episode by sustained transfer speed. The sidebar shows the results, a retest is available, and a manual choice is not overwritten. Selection is off by default. A playback failure only reports the error and no longer switches lines automatically (#87, #91).
- **Collect joins an open round**: while a seal is open and a collect queue is still running, a new scheduled or manual collect joins that round and sealing waits until every queue has finished. A new collect is refused when no queue is running. Sources in the same batch that have not started are no longer marked failed just because an earlier source has reached indexing. When a scheduled run overlaps the next tick, one extra round collects the sources that were idle, after the current queue finishes. The refusal states whether the search index is updating or this round is about to be stored, and says that collects already running will finish. A rejected enqueue releases sources it had already reserved (#92).
- **Search index seal**: sealing runs once, only after every collect queue has finished. A full collect, an unavailable in-memory index, an index with 0 entries, or a changed-ID set larger than half of the current index loads the film table and play lines sequentially before any ID lookup. An hourly incremental collect that changes at most half of the index patches by ID, and a failed lookup falls back to the same sequential scan. A collect round ends only after this round's rebuild succeeds (#92).
- **Transient disconnects**: a source that already has successful pages, and then hits consecutive EOF, connection reset, or per-page timeout errors, pauses for about 15 seconds and retries those pages instead of recording a partial failure immediately. Each page is retried at most twice more, and the source takes at most 3 short pauses. Pages that still fail follow the existing consecutive-failure and 5-minute pause rules (#92).
- **Failed pages on the dashboard**: "采集失败页数" (failed collect pages) is the total of pages that failed during the current day in Beijing time, and each source can be expanded. An error in the batch detail can be opened in a dialog that separates the reason, the request URL, and the failed pages. Newly stored errors keep up to 2000 characters of the reason (#95).

### Architecture

- Primary/secondary binding has been removed. Each collect source maintains its own records, and a failure in one source does not affect the others.
- The first enabled collect source in sort order is the main source for the site. Switching the main source immediately shows that source's existing data, without re-collecting or deleting films; banners are stored per source, and the home page shows the banners of the new main source.
- The play page appends play lines from other sources by match key, in this order: Douban ID + title, title#category, title only. Play lines remain grouped by source, and records are not merged.
- Schema migrations retain only an empty baseline; new changes are appended at the end.

### Collect

- Request intervals and the write token bucket have been removed. Limits: 128 in-flight requests globally, 16 goroutines per source, and a 30-second timeout per page write transaction.
- The circuit breaker trips only when there are zero successful pages and page fetch failures reach the threshold. A source that already has successful pages, and then hits a run of connection drops or per-page timeouts, pauses for about 15 seconds and retries those pages. Pages that still fail count toward the existing rule: pause 5 minutes after 20 consecutive failed pages, and end the round after 3 such pauses that still fail. On stop or import, pages not yet being written are discarded and open transactions are completed.
- JSON probes use `detail` and XML probes use `videolist`, consistent with regular page fetches.
- Scheduled collects yield to maintenance tasks and afterwards fill the uncollected time window, up to 7 days. While a seal is open and a collect queue is still running, a new scheduled or manual collect joins that round; a new collect is refused when no queue is running. The gap-fill anchor advances only when an entire scheduled run succeeds. For the rules, see [Scheduled task conflicts](https://github.com/fe-spark/EcoHub/blob/main/docs/cron-task-conflict_EN.md).
- Batches are recorded in `collect_records`, and stored items in `collect_record_films`. The status reflects the actual storage result: success, partial success, failed, stopped, or aborted. Pages that were not fetched are marked as not fetched.
- Failed pages are retried within 30 minutes; after that, the entire source is re-collected. Successful batches are archived, and history is cleaned up on a schedule.
- Unmapped films are placed in the source's top-level "其他" (Other) category (the source's "其它" is treated as the same category). Hidden or disabled categories (including "其他") are still collected and stored but are not shown on the site. A run with successful pages still proceeds to indexing; a run with zero successes fails immediately.
- While a source is sealing, it cannot be deleted and its URL or protocol cannot be changed. Deleting a source waits for only one index finalization.
- When available memory is 2 GiB or less, only page concurrency, in-flight fetches, and the write queue are reduced.

### Search and site

- In-memory inverted index (word segmentation, full pinyin, pinyin initials) and category rankings, updated incrementally during collect. A full rebuild runs once at startup. Sealing runs once after every collect queue has finished. A full collect, a missing index, an index with 0 entries, a changed-ID set larger than half of the current index, or a failed database read loads the film table and play lines sequentially before any ID lookup. An hourly incremental collect that changes at most half of the index patches by ID.
- The home page hides categories without films and shows at most 12 top-level category blocks. Navigation, filters, and TVBox still use all categories of the source.
- Category rules apply per source. The order and visibility of site categories follow the admin mapping. Hidden categories (including rule-merged groups) are removed from all site APIs, and old URLs indicate that the category is no longer valid; search, daily updates, and TVBox keywords do not return these films either. Collect still stores these films, and category management still lists them and can re-enable them. A subcategory is never promoted to top level automatically.
- Unmapped films appear in the source's "其他" (Other) category. When a source rename takes over the previous "其他" ID, a new ID is assigned as a fallback.
- The player supports multiple play lines, playback progress memory, and danmaku. When no play source is specified, playback starts on the main source. Speed-based source selection is off by default; when enabled, it picks a line for the current episode by sustained transfer speed, and a playback failure no longer switches lines automatically. After the danmaku source is changed, the old cache is no longer used.
- **Image acceleration**: with the media reverse proxy enabled, site and TVBox posters are loaded through the edge CDN automatically, are accessible directly without a proxy, and do not consume the self-hosted server's bandwidth.

### Admin

- Actions open in drawers. Configuration, collect sources, and rules can be backed up and imported; collect is stopped before an import.
- **Banners**: stored per source; title, category, remarks, and other fields are assembled from the library at display time. Films without remaining play lines on that source are not shown on the home page and are marked as invalid in the admin panel, where they can be deleted and reordered. Manual add, edit, delete, and reorder operations are validated by version number, and concurrent edits prompt a refresh.
- **One-click scheduling**: select a strategy (newest, most popular, highest rated) and a count per category, up to 5 per category with no total limit; preview and adjust the result in a drawer, then publish the entire set. The scheduling configuration is not stored. Each one-click run requires confirmation and defaults to most popular, 1 per category; running it again on the same page reuses the last selection for that source, and reloading the page restores the defaults.
- Category management and the site share one rule tree, which controls only order and visibility. When a parent category is hidden, the switches of its subcategories cannot be changed.
- Roles: super user, regular user, and visitor.
- The ID column in the film list shows the source's own ID. During collect, only the failure count of failed pages is highlighted in red.
- An invalid play line is skipped individually. After a configuration is stored in the database, a failed cache write removes the old cached value.
- **Media reverse proxy**: supports configuration and validation of edge reverse proxy templates; the server rewrites poster URLs on output without modifying the original data.
