Release **v3.0.4**, image `ghcr.io/fe-spark/ecohub:v3.0.4` (`:latest` also points to this release). For image tags, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/README-Deploy_EN.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- **v3.0.3 no longer reads `HTTPS_PROXY`, `HTTP_PROXY`, or `ALL_PROXY`**: the three variables have been removed from the compose files and `.env.example`, and values left in an existing `.env` no longer take effect. Deployments that relied on these variables to route danmaku or Telegram traffic through a proxy must, after upgrading, enable the global proxy in "网络代理" (Network proxy) in the admin panel and enable the "弹幕抓取" (Danmaku fetching) or "Telegram 机器人" (Telegram bot) module; otherwise these requests connect directly.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3_EN.md).

### v3.0.4 changes

- **Collect replay**: films already in the library are queried by page for identity, update time, and match keys. Unchanged films no longer update the timestamp one by one, and unchanged match keys are not rewritten (#88).
- **Search index seal**: sealing starts only after every collect queue has finished. If any queue in the round includes a full collect, or the in-memory index is missing or has 0 entries, the film table and play lines are loaded by a full sequential scan. When every queue is an hourly incremental collect and the index already has entries, the seal updates only this round's IDs. A collect round ends only after this round's rebuild succeeds; a failed rebuild or an unrelated search-index progress event does not mark the collect finished. New collects stay blocked while the seal queue is waiting or a rebuild is queued (#88, #90).
- **Clients**: the Android client included in this repository is updated to `v1.1.5`, and the OHOS client to `v1.4.7`.

### Architecture

- Primary/secondary binding has been removed. Each collect source maintains its own records, and a failure in one source does not affect the others.
- The first enabled collect source in sort order is the main source for the site. Switching the main source immediately shows that source's existing data, without re-collecting or deleting films; banners are stored per source, and the home page shows the banners of the new main source.
- The play page appends play lines from other sources by match key, in this order: Douban ID + title, title#category, title only. Play lines remain grouped by source, and records are not merged.
- Schema migrations retain only an empty baseline; new changes are appended at the end.

### Collect

- Request intervals and the write token bucket have been removed. Limits: 128 in-flight requests globally, 16 goroutines per source, and a 30-second timeout per page write transaction.
- The circuit breaker trips only when there are zero successful pages and page fetch failures reach the threshold. A source with successful pages pauses for 5 minutes after 20 consecutive failed pages and then resumes; after 3 consecutive pauses that still fail, the current round ends. On stop or import, pages not yet being written are discarded and open transactions are completed.
- JSON probes use `detail` and XML probes use `videolist`, consistent with regular page fetches.
- Scheduled collects yield to maintenance tasks and afterwards fill the uncollected time window, up to 7 days. The gap-fill anchor advances only when an entire scheduled run succeeds. For the rules, see [Scheduled task conflicts](https://github.com/fe-spark/EcoHub/blob/main/docs/cron-task-conflict_EN.md).
- Batches are recorded in `collect_records`, and stored items in `collect_record_films`. The status reflects the actual storage result: success, partial success, failed, stopped, or aborted. Pages that were not fetched are marked as not fetched.
- Failed pages are retried within 30 minutes; after that, the entire source is re-collected. Successful batches are archived, and history is cleaned up on a schedule.
- Unmapped films are placed in the source's top-level "其他" (Other) category (the source's "其它" is treated as the same category). Hidden or disabled categories (including "其他") are still collected and stored but are not shown on the site. A run with successful pages still proceeds to indexing; a run with zero successes fails immediately.
- While a source is sealing, it cannot be deleted and its URL or protocol cannot be changed. Deleting a source waits for only one index finalization.
- When available memory is 2 GiB or less, only page concurrency, in-flight fetches, and the write queue are reduced.

### Search and site

- In-memory inverted index (word segmentation, full pinyin, pinyin initials) and category rankings, updated incrementally during collect. A full rebuild runs once at startup. Sealing runs after every collect queue has finished. A full collect in any queue, a missing in-memory index, an index with 0 entries, or a failed database read loads the film table and play lines sequentially. When every queue is an hourly incremental collect and the index already has entries, the seal updates only the changed IDs.
- The home page hides categories without films and shows at most 12 top-level category blocks. Navigation, filters, and TVBox still use all categories of the source.
- Category rules apply per source. The order and visibility of site categories follow the admin mapping. Hidden categories (including rule-merged groups) are removed from all site APIs, and old URLs indicate that the category is no longer valid; search, daily updates, and TVBox keywords do not return these films either. Collect still stores these films, and category management still lists them and can re-enable them. A subcategory is never promoted to top level automatically.
- Unmapped films appear in the source's "其他" (Other) category. When a source rename takes over the previous "其他" ID, a new ID is assigned as a fallback.
- The player supports multiple play lines, playback progress memory, and danmaku. After the danmaku source is changed, the old cache is no longer used.
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
