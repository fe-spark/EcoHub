Release **v3.0.3**, image `ghcr.io/fe-spark/ecohub:v3.0.3` (`:latest` also points to this release). For image tags, see the [Deploy guide](https://github.com/fe-spark/EcoHub/blob/main/docs/README-Deploy_EN.md).

[中文](https://github.com/fe-spark/EcoHub/blob/main/docs/RELEASE.md) | English

### Upgrade notes

- v3.0.x: for installations managed by `ecohub`, run `ecohub update`. For 1Panel or self-managed compose files, run `docker compose pull && docker compose up -d` in the install directory.
- **v3.0.3 no longer reads `HTTPS_PROXY`, `HTTP_PROXY`, or `ALL_PROXY`**: the three variables have been removed from the compose files and `.env.example`, and values left in an existing `.env` no longer take effect. Deployments that relied on these variables to route danmaku or Telegram traffic through a proxy must, after upgrading, enable the global proxy in "网络代理" (Network proxy) in the admin panel and enable the "弹幕抓取" (Danmaku fetching) or "Telegram 机器人" (Telegram bot) module; otherwise these requests connect directly.
- v2.x: not compatible; no migration is provided. See [Upgrading from v2 to v3](https://github.com/fe-spark/EcoHub/blob/main/docs/v2-to-v3_EN.md).

### v3.0.3 changes

- **Network proxy**: danmaku fetching and Telegram notifications are now controlled only by "网络代理" (Network proxy) in the admin panel. They use the proxy when the global proxy is enabled and the corresponding module ("弹幕抓取" Danmaku fetching, "Telegram 机器人" Telegram bot) is enabled; otherwise they connect directly. "弹幕抓取" is a new module and is disabled by default.
- **Danmaku load status**: no longer displayed as a notice over the video. The result is shown as gray placeholder text in the danmaku input box, and a dot on its left indicates loading, success, no network danmaku, or failure; hovering shows the full message and the failure reason. When the player is narrower than `560px`, the input box is collapsed and the dot is shown on the danmaku switch.
- **Danmaku test**: when "自动嗅探全网弹幕" (Auto-fetch network danmaku) is enabled, the danmaku card in "网站配置" (Site settings) provides a "测试" (Test) button. It fetches network danmaku for episode 1 of a specified title using the settings currently in the form and displays the result below the button. The card also indicates whether danmaku requests use the proxy or connect directly.
- **Player danmaku**: reverted to the danmaku plugin's built-in scheduling. The patch retains only the loading performance improvement (#63) and the fix that prevents danmaku with time 0 from being delayed. The switch, opacity, font size, speed, display area, danmaku types, and anti-overlap settings are stored in the browser and retained across episodes. Fixed the danmaku switch and settings not appearing on phones (#83).
- **Responsive player layout**: the layout follows the player's own width instead of the device type and switches immediately when the window width changes. Below `560px`, the danmaku input box and the web fullscreen button are collapsed; at `560–767px`, the web fullscreen button is collapsed; at `768px` and above, the full layout is used. The danmaku font size scales down with the player width (minimum `12px`), and the display area is measured against the picture, so danmaku no longer appears on the black bars. Below `768px`, top and bottom danmaku are displayed as scrolling danmaku, which resolves overlapping danmaku on phones.
- **Player border**: the player shows a 1px border and rounded corners at all widths, so its boundary remains visible against dark backgrounds on phones. No border is shown in fullscreen or web fullscreen.
- **Player height**: in the single-column layout (width `1024px` or less), the player height is calculated from the video's actual aspect ratio at full width, removing excess black bars above and below the picture. Portrait video is limited to 75% of the screen height, and ultra-wide video has a minimum height of `180px`. The danmaku settings panel scrolls when it does not fit.
- **Episode panel**: in the single-column layout, the episode panel expands with its content and the list has a maximum height of `max(320px, 60svh)`, instead of showing only one or two rows. Above 100 episodes, the list is divided into ranges of 50 (1-50, 51-100, …), and the range being played is shown by default.
- **TVBox year sort**: films from the same year are further sorted by update time, consistent with the site.
- **Install script**: `ecohub` supports a user-maintained `docker-compose.override.yml` in the install directory. Install and update never overwrite this file, and all compose commands apply it. `ecohub help` now documents `SERVER_PORT` and `MYSQL_ROOT_PASSWORD`.
- **Documentation**: all documents have been rewritten according to the new style guide and corrected against the code. The deploy guide, FAQ, server and web READMEs, v2 upgrade guide, scheduled task conflicts, and release notes are available in English.

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

- In-memory inverted index (word segmentation, full pinyin, pinyin initials) and category rankings, updated incrementally during collect. A full rebuild runs once at startup; an incremental update also triggers a full rebuild when no usable index exists in memory or a database read fails.
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
