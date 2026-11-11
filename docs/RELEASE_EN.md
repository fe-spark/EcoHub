Release **v3.0.3**, image `ghcr.io/fe-spark/ecohub:v3.0.3` (`:latest` points to it too). For image tags see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/README-Deploy_EN.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade

- v3.0.x: if installed with `ecohub`, run `ecohub update`. With 1Panel or your own compose file, run `docker compose pull && docker compose up -d` in the install directory.
- **v3.0.3 no longer reads `HTTPS_PROXY`, `HTTP_PROXY`, or `ALL_PROXY`**: the three variables are removed from the compose files and `.env.example`, and leaving them in an old `.env` has no effect. If you relied on them to send danmaku or Telegram through a proxy, after upgrading turn on the global proxy in the admin 「网络代理」 page and turn on the 「弹幕抓取」 or 「Telegram 机器人」 module; otherwise those requests connect directly.
- v2.x: not compatible, no migration. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3_EN.md).

### v3.0.3 fixes

- **Network proxy**: danmaku fetching and Telegram notifications now follow only the admin 「网络代理」 (network proxy) page: they use the proxy when the global proxy is on and the matching module (「弹幕抓取」 danmaku fetching, 「Telegram 机器人」 Telegram bot) is on; otherwise they connect directly. 「弹幕抓取」 is a new module and is off by default.
- **Danmaku load status**: no longer shown as a notice over the video. The result is the gray placeholder text in the danmaku input, and a dot on its left shows loading, OK, no network danmaku, or failed; hover for the full message and the failure reason. When the player is narrower than `560px` the input is hidden and the dot moves to the danmaku toggle.
- **Danmaku test**: with 「自动嗅探全网弹幕」 (auto-sniff network danmaku) on, the danmaku card in 「网站配置」 (site config) has a 「测试」 (test) button. It fetches episode 1 of a title with the settings currently filled in and shows the result below the button. The card also shows whether danmaku requests go through the proxy or direct.
- **Player danmaku**: back to the danmaku plugin's own scheduling; the patch keeps only the faster loading (#63) and stops moving danmaku with time 0 later. On/off, opacity, font size, speed, display area, danmaku types, and anti-overlap are saved in the browser and kept across episodes. Fixes the danmaku toggle and settings not showing on phones (#83).
- **Player layout by width**: the layout follows the player's own width instead of the device type and switches as soon as the window narrows. Below `560px` the danmaku input and the web fullscreen button are hidden; at `560–767px` the web fullscreen button is hidden; from `768px` the full layout is used. Danmaku font size shrinks with the player width (minimum `12px`) and the display area is measured on the picture, so danmaku no longer floats over the black bars. Below `768px`, top and bottom danmaku scroll instead, so danmaku no longer overlaps on phones.
- **Player border**: the player has a 1px border and rounded corners at every width, so its edge is visible on dark backgrounds on phones. No border in fullscreen or web fullscreen.
- **Player height**: when the page is a single column (`1024px` wide or less), the player height follows the video's real aspect ratio at full width, with no extra black bars above and below. Portrait video is capped at 75% of the screen height, and ultra-wide video gets at least `180px`. The danmaku settings panel scrolls when it does not fit.
- **Episode panel**: in the single-column layout the episode panel grows with its content and the list is at most `max(320px, 60svh)` tall, instead of showing only one or two rows. Above 100 episodes the list is split into ranges of 50 (1-50, 51-100, …), opening on the range that is playing.
- **TVBox year sort**: films from the same year are then sorted by update time, the same as the site.
- **Install script**: `ecohub` supports your own `docker-compose.override.yml` in the install directory; install and update never overwrite it, and every compose command applies it. `ecohub help` now lists `SERVER_PORT` and `MYSQL_ROOT_PASSWORD`.
- **Docs**: all docs are rewritten to the new style guide and corrected against the code. Deploy, FAQ, server, web, v2 upgrade, scheduled task conflicts, and release notes all have English versions.

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
