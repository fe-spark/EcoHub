Release **v3.0.2**, image `ghcr.io/fe-spark/ecohub:v3.0.2` (`:latest` points to it too). For image tags see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/README-Deploy_EN.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade

- v3.0.x: if installed with `ecohub`, run `ecohub update`. With 1Panel or your own compose file, run `docker compose pull && docker compose up -d` in the install directory.
- v2.x: not compatible, no migration. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3_EN.md).

### v3.0.2 fixes

- **Orphan data cleanup**: now deletes in batches by mid and reports errors per batch, so long transactions no longer trigger invalid connection alerts.
- **Scheduled task hardening**: maintenance tasks take priority and queue serially, and "run now" is disabled while queued; expired collect records are cleaned in batches; panics in scheduled tasks and collect goroutines are recovered and raise an alert.
- **Database connections**: health checks no longer replace the global MySQL/Redis connections, removing a data race; MySQL connect and Ping have a 10-second timeout.
- **Retry after stop**: when you retry right after "stop all" or stopping one source, the old batch no longer overwrites the new retry's progress, lock, or status.
- **Rate-limit resume**: after a source has successful pages, 20 failed pages in a row pause it for 5 minutes, then it resumes from the breakpoint; after 3 pauses in a row that still fail, this round ends. Other sources are not affected.
- **Failed pages**: no longer limited to the first 500 pages, so retrying failed pages can recover all of them. `collect_records.detail_json` is now MEDIUMTEXT and is widened automatically on start.
- **Collect progress tooltip**: failed pages in the hover tooltip are sorted by page number, consecutive pages are merged into ranges, and long content scrolls.
- **Variety show segments**: the play list only removes entries with exactly the same URL, so segments such as "第1期上 / 第1期下 / 纯享 / 加更" (part 1 / part 2 / extended / bonus) are no longer merged into one episode (#80). For shows that have already ended, run one full collect to recover missing episodes.
- **Category nav highlight**: entering a category section or reloading a category page now highlights the matching category in the top nav (#82).

### Architecture

- Primary/secondary binding is removed. Each collect source keeps its own records, and a problem with one source does not affect others.
- The first enabled source in sort order is the main source for the site. Switching shows that source's existing data right away, with no re-collect and no deleted films; carousels are stored per source, and the home page shows the new main source's own set.
- The play page adds other sources' lines by match key: Douban ID + title, title#category, title only. Lines stay separate per source, and records are not merged.
- Schema migrations keep only an empty baseline; new changes are only appended at the end.

### Collect

- Request intervals and the write token bucket are removed. 128 in flight globally, 16 goroutines per source, 30-second timeout per page write transaction.
- The circuit breaker trips only with zero successful pages and page-fetch failures reaching the threshold. A source with successful pages pauses 5 minutes after 20 failed pages in a row and resumes; after 3 pauses in a row that still fail, this round ends. On stop or import, pages not yet writing are dropped and transactions already open are finished.
- JSON probes use `detail` and XML probes use `videolist`, the same as real page fetches.
- Scheduled collect yields to maintenance, then fills the uncollected window afterward, up to 7 days. The gap-fill anchor moves only when a whole scheduled run succeeds. Rules: [Scheduled task conflicts](https://github.com/fe-spark/EcoHub/blob/main/docs/cron-task-conflict_EN.md).
- Batches are recorded in `collect_records` and stored-item details in `collect_record_films`. Status follows what was actually stored: success, partial success, failed, stopped, aborted. Pages not fetched are marked as not fetched.
- Failed pages are retried within 30 minutes; after that the whole source is re-collected. Successful batches are archived, and history is cleaned on a schedule.
- Unmapped films go into the source's top-level "其他" (Other) category (the source's "其它" counts as the same). Hidden or disabled categories (including "其他") are still collected and stored but not shown on the site. A run with successful pages still goes to indexing; a run with zero successes fails right away.
- While a source is sealing, it cannot be deleted and its URL or protocol cannot be changed. Deleting a source waits for only one index finish.
- When available memory is 2 GiB or less, only page concurrency, in-flight fetches, and the write queue are tightened.

### Search and site

- In-memory inverted index (word segmentation, full pinyin, pinyin initials) and category rankings. Collect updates it incrementally. A full rebuild runs once on start; an incremental update also triggers one full rebuild when there is no usable index in memory or reading the database fails.
- The home page hides categories with no films, and the category blocks show at most 12 top-level categories. Navigation, filters, and TVBox still use all of the source's categories.
- Category rules apply per source. Site category order and visibility follow the admin mapping. Hidden categories (including rule-merged groups) disappear from every site API, and old URLs show that the category is no longer valid; search, daily updates, and TVBox keywords do not return those films either. Collect still stores those films. Category management still shows them and can turn them back on. A subcategory never becomes top-level on its own.
- Unmapped films appear in the source's "其他" (Other). When a source rename takes the old "其他" ID, a new ID is assigned as a fallback.
- The player supports multiple lines, progress memory, and danmaku. After changing the danmaku source, the old cache is not hit.
- **Image acceleration**: with media reverse proxy on, site and TVBox posters go through the edge CDN automatically, open directly without a proxy, and do not use your own bandwidth.

### Admin

- Actions now open in drawers. Config, sources, and rules can be backed up and imported; collect is stopped before import.
- **Carousel**: stored per source; title, category, notes, and so on are assembled live from the library. Films with no play line left on that source are not shown on the home page and are marked invalid in admin, where they can be deleted and reordered. Manual add, edit, delete, and reorder are checked by version number, and concurrent edits prompt a refresh.
- **One-click scheduling**: pick a strategy (newest, hottest, top rated) and a count per category, at most 5 per category, no total limit; preview and adjust the result in a drawer, then publish the whole set. The scheduling config is not stored. Every one-click run asks for confirmation, defaulting to hottest, 1 per category; running it again on the same page reuses that source's last choice, and a reload returns to the defaults.
- Category management and the site share one rule tree; it only adjusts order and visibility. When a parent is hidden, its children's switches cannot be changed.
- Roles are super user, regular user, and visitor.
- The ID column in the film list is the source's own ID. During collect, only the failure count of failed pages is shown in red.
- A single bad line is skipped on its own. After config is stored in the database, a failed cache write deletes the old value.
- **Media reverse proxy**: supports configuring and validating edge reverse proxy templates; the server rewrites poster URLs on output without changing the original data.
