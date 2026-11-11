# FAQ

[中文](./README-FAQ.md) | English

For installation, ports, and passwords, see the [Deploy guide](./README-Deploy_EN.md).

## Collect and data

### Why does the site show no films after deployment?

No data has been collected yet. The release image contains no media data. The first startup writes only the 12 preset collect sources and does not run a full collect automatically.

Run a full collect from "采集中心" (Collect center) in the admin panel. More collect sources take longer, and the first collect can take several hours. Waiting for completion is not necessary: each collected batch is published to the snapshot that the site reads, so films from the main source appear as the collect progresses.

The site lists, the admin film list, and the TVBox / MacCMS subscriptions all read the snapshot rather than the tables being collected. An empty site before the first batch is published is therefore expected behavior.

### What is the main source?

The main source is the first enabled collect source in sort order. The site's home page, categories, search, and daily updates show only the films and categories collected by the main source.

Example: with collect sources A, B, and C in that order and all enabled, the main source is A. Moving B to the top switches the site to the data already collected by B immediately; nothing is re-collected, and the data of A and C is not deleted. When all collect sources are disabled, there is no main source.

The play page starts from the main source's film, uses match keys to find the same film in other enabled collect sources, and appends their play lines. Play lines are grouped by collect source.

The poster source is a separate flag and does not determine the main source. Site covers use the matching film's cover from the poster source first and fall back to the main source's own image when no match is found. When the poster source is the main source, its own image is used.

### What data is cleared when the main source is switched?

None. Switching the main source does not delete films or trigger a re-collect, and it can be done while a collect is running.

Homepage banners are stored per collect source. After the switch, the home page shows the banner set of the new main source. The previous set is retained and is shown again after switching back.

The following operations differ from switching the main source:

- Changing a collect source's URL: clears that source's films, play lines, failure records, and categories (including films without play lines), then fetches the categories again. Other collect sources are not affected.
- Deleting a collect source: likewise clears only that source's data.

### How are films deduplicated and matched across sources?

Each collect source keeps its own copy of the data. Collect does not merge them.

- Within one collect source: `movie_source_mapping` uses "collect source + the source's `vod_id`" to locate the film's existing record, and a subsequent collect updates only that record. Entries on the same page of the same source with the same match key are merged into one film.
- Across collect sources: match keys are not used to merge records. The play page uses them at read time to find the same film in other sources, in this order: Douban ID + title, title + top-level category, title only. A film matched by several keys is counted once. Play lines remain grouped by source; episodes from different sources are never combined.

The category key is `source:{collect source ID}:{source category ID}` and maps one-to-one to that source's own category tree.

### Where are scrape results and homepage banners stored?

- Film scraping: writes the selected cover, synopsis, score, and similar fields directly to the film. The title and category are not changed. Changes cannot be reverted.
- Homepage banners: stored per collect source. Each record stores only the film, the order, and any image, synopsis, score, or year edited in the admin panel; all other information is read from the library at display time. Scraping within the banner editor updates only that banner record, not the film itself, and no separate scrape table exists.

### What is the state of the data after a manual stop?

Stopping interrupts pages that are still being fetched. Pages already stored remain in the library. A page that is being written completes; pages waiting to be written are discarded. Failed pages are recorded in "采集记录" (Collect records) and can be retried.

## Snapshots and cache

### Why are snapshots published during a collect?

The site does not read the tables being collected, because those tables change continuously during a collect. After each batch, the batch is written to the snapshot and the in-memory search index is updated incrementally per film. Site lists, filters, the admin film list, and TVBox / MacCMS all read the snapshot.

After publishing, list cache cleanup is debounced by 5 seconds: when many batches are processed in succession, Redis is not cleared after every batch but once, approximately 5 seconds after the last batch.

The search index is normally updated incrementally. It is fully loaded once at startup. If an incremental update finds no usable index in memory, a full rebuild is performed as a fallback.

### Why do configuration changes not appear on the site immediately?

Saving the site configuration updates Redis and clears the home page cache. If the page still shows old content, common causes include:

- Caching in the browser, a CDN, or a reverse proxy.
- The running code or image is not the latest version.

Film lists, categories, and filters show only the current main source. After the main source is switched, the site shows the films already collected by the new main source; if it has not collected any data yet, the lists are empty.

The regular TVBox lists have an additional Redis cache layer with a lifetime of up to approximately 12 hours, which is cleared with a debounce after new data is stored. If results still differ, verify that the requests reach the same instance with the same parameters.

### How is "最近更新" (Recently updated) sorted? Are category pages and TVBox consistent?

Films are sorted by `film_index.update_stamp` in descending order, then by `mid`.

- New films: use the update time provided by the collect source.
- Films already in the library: the time is updated to the current collect time only when the play lines change. Changes to download lines, remarks, cast, or cover do not affect the order.
- Saving a film manually in the admin panel also sets its time to the save time.

The same rules apply to all collect sources.

The site category page and TVBox derive their filter options from the same filter data and apply the same matching rules. The site queries the in-memory index, while TVBox queries MySQL:

| Filter | Rule | Consistent |
| --- | --- | --- |
| Category | Expanded by the source's category rules into the same set of source categories (including merged groups); only films with a play line from that source are listed | Yes. The site uses the main source; TVBox uses each subscribed site's own source |
| Genre (剧情) | The film's genre tags contain the selected value | Yes |
| Region, language | Exactly equal to the selected value | Yes |
| Year | Equal to the selected year; no filtering when the value is not a number | Yes |
| "其他" (Other) | Value not among the filter options, including empty values | Yes |
| Sort | Recently updated, popularity, rating, year. When sorting by year, films from the same year are then ordered by update time | Yes |

Both use a Redis cache with different lifetimes: site filter results are cached for 2 hours; TVBox list pages without genre, region, language, or year filters are cached for approximately 12 hours, and those with filters for 3 minutes. For short-lived inconsistencies, check the cache first.

## Network

### Which requests use the admin network proxy?

A module's requests use the proxy only when the global proxy is enabled in "网络代理" (Network proxy, `/manage/system/proxy`) in the admin panel and the module's own switch is also enabled. All other requests connect directly. Container environment variables do not affect outbound requests.

| Module (admin name) | Covered requests | Default on new installations |
| --- | --- | --- |
| 影视采集爬虫 (Collect spider) | Collect source APIs. Each collect source also requires "采集接口代理" (Collect API proxy) to be enabled on its edit page | On |
| TMDB 刮削 (TMDB scraping) | Requests to the TMDB API (including the admin connectivity test) | On |
| Telegram 机器人 (Telegram bot) | Notifications and polling for the pagination buttons | On |
| 系统版本更新 (Version update) | GitHub version check | On |
| 弹幕抓取 (Danmaku fetching) | Danmaku search and download from the official dandanplay API or a self-hosted danmu-api (including the "测试" (Test) button in the admin danmaku settings) | Off |

"弹幕抓取" is disabled by default because a self-hosted danmu-api is usually deployed on the local network and becomes unreachable when routed through the proxy. After an upgrade from an earlier version, it is also disabled and danmaku requests continue to connect directly; the other modules keep their saved settings.

Example: a proxy runs on port `7890` on the host, and the official dandanplay API should be accessed through it. Set the proxy address to `http://host.docker.internal:7890` (inside the container, `127.0.0.1` refers to the container itself), enable the global switch, and then enable "弹幕抓取".

## Danmaku

### How is missing network danmaku diagnosed?

First use the "测试" (Test) button in "网站配置" (Website) → "基本信息" (Basic) → "弹幕系统配置" (Danmaku settings) in the admin panel, then check the load status in the player's danmaku input box.

Network danmaku requires all of the following: "启用弹幕系统" (Enable danmaku) and "自动嗅探全网弹幕" (Auto-fetch network danmaku) are enabled, and either a self-hosted danmu-api address or the dandanplay AppId and AppSecret is configured.

**Test button**: shown when "自动嗅探全网弹幕" is enabled. Enter a title and click "测试". The test uses the settings currently in the form (saving is not required) to fetch network danmaku for episode 1 of that title and displays the result below the button, for example "匹配到「葬送的芙莉莲 第1话」，第 1 集共 N 条全网弹幕" (matched; N network comments) or "失败：弹幕接口请求超时" (failed: request timed out). The test neither reads nor writes the danmaku cache. Whether it uses the proxy is determined by the "弹幕抓取" switch in "网络代理", as on the play page.

**Load status in the input box**: the danmaku load status is displayed in the danmaku input box of the player control bar and does not cover the video. The gray placeholder text shows the result, a dot on the left indicates the state, and hovering over the input box or the dot shows the full message (including the failure reason). When the player is narrower than 560 px, the input box is collapsed and the dot moves to the top-right corner of the danmaku switch; hovering over the switch shows the full message. On failure, a short notice is also shown over the video once (see the next section).

| Dot | Placeholder | Meaning |
| --- | --- | --- |
| Gray, blinking | 弹幕加载中… | Loading danmaku |
| Green | 共 N 条弹幕，说点什么吧 | Site and network danmaku are loaded; N is the total. Hovering shows "已加载 N 条弹幕（全网 M 条）" (N loaded, M from the network) |
| Yellow | 共 N 条弹幕，未找到全网弹幕 | No matching title was found, or the episode has no network danmaku. With no comments at all, the text is "未找到全网弹幕，说点什么吧" |
| Red | 全网弹幕加载失败：reason | Timeout, connection failure, authentication failure, unparseable response, etc. Long text is truncated with an ellipsis; hovering shows the full reason and the site comment count |
| Red | 弹幕加载失败：reason | The danmaku request itself failed (the API returned an error, or the network was interrupted) |
| Hidden | 共 N 条弹幕，说点什么吧 | Network danmaku is disabled; only comments posted on this site are shown. With no comments at all, the plugin default text "发个友善的弹幕见证当下" is shown |

Switching episodes rebuilds the player, and the status restarts from "弹幕加载中…".

The failure reason is a user-facing message and never contains the API address or keys. Successfully loaded network danmaku is cached for 7 days per API address, title, and episode. Unmatched and failed results are not cached and are retried on the next playback.

### Are danmaku settings saved? What happens when the player gets narrow?

Yes. The switch, opacity, font size, speed, display area, danmaku types, and anti-overlap settings changed in the player's danmaku settings are stored in the current browser and reused when switching episodes or reopening the page. Only changed settings are stored. Using a different browser or clearing site data restores the defaults.

The player is laid out according to its own displayed width. It does not distinguish between phones and computers and does not read the browser's user agent: a player of the same width behaves identically on a phone and in a narrowed desktop browser window (including the responsive mode of developer tools). The layout switches immediately when the window is resized, the screen is rotated, or fullscreen is entered or exited, without a page reload.

| Player width | Control bar | Load status | Top/bottom fixed danmaku |
| --- | --- | --- | --- |
| Below 560 px (phone portrait, very narrow window) | Danmaku input box and web fullscreen button collapsed (the button remains while in web fullscreen so that it can be exited); reduced button spacing | Dot at the top-right corner of the danmaku switch; hover over the switch for the message; a short notice over the video once on failure | Displayed as scrolling danmaku |
| 560–767 px (phone landscape, tablet, narrow window) | Input box shown and shrinks with the control bar; web fullscreen button collapsed | Placeholder and dot in the input box | Displayed as scrolling danmaku |
| 768 px and above | Full control bar | Placeholder and dot in the input box | Remain fixed at the top/bottom |

560 px is the narrowest width at which the input box still retains approximately 110 px with reduced button spacing; at narrower widths it shrinks to a few dozen pixels and the send button extends beyond the player. 768 px matches the width at which the play page switches to its mobile layout. Below 768 px, the half-screen area of the picture holds only five to eight rows, and fixed danmaku would cover scrolling danmaku; fixed danmaku is therefore displayed as scrolling danmaku and takes part in the same overlap avoidance.

**Font size**: the font size in the settings (default 25 px) applies to a large player. It is used unchanged when the player is at least 575 px wide; on narrower players it is scaled down proportionally with the width and capped at "player width ÷ 22" (at most 16 px for a player approximately 360 px wide), with a minimum of 12 px. A large font size set on a computer therefore does not fill a narrow player. When the font size is changed on a narrow player, the saved value is converted back to the large-player size.

**Display area**: "1/4", "半屏" (Half), "3/4", and "满屏" (Full) are measured against the picture itself. When a narrow player is taller than the picture and shows black bars above and below it, danmaku is displayed only within the picture and not on the bars.

When the player size changes, danmaku currently on screen is cleared and subsequent danmaku is laid out for the new size, preventing overlap between the old and new row positions.

## Login and permissions

### Why do API calls report "not logged in" when the admin page opens normally?

The web app performs only a preliminary check; the authoritative check is performed by the backend:

1. `web/src/proxy.ts` checks only whether the `ecohub_auth_token` cookie exists.
2. When `/manage` opens, the server-side layout calls `/api/manage/user/info`. The backend validates the JWT and the current token stored in Redis, and redirects to `/login` on failure.
3. If the token becomes invalid after the page has opened (expired, replaced by a sign-in on another device, or cleared from Redis), subsequent API calls return "not logged in" and the web app returns to the login page.

The backend response is authoritative. A sign-in is valid for 10 days; after that, the user must sign in again.

### How are the default account and visitor accounts used?

The first startup creates only the super user `admin`. Its password is the value of `ECOHUB_ADMIN_PASSWORD`, or `admin` if it is not set. If `admin` already exists, its password is not changed. Change the password in the admin panel immediately after the first sign-in.

Other accounts are created in "账号管理" (Accounts). Three roles are available:

| Role | Permissions |
| --- | --- |
| 超级用户 (Super user) | All operations, including site configuration, account management, and upgrades |
| 普通用户 (Regular user) | Day-to-day management, excluding operations restricted to the super user |
| 访客 (Visitor) | Read-only. All GET requests are allowed; POST / PUT / PATCH / DELETE requests are rejected |

### How do subscriptions work with private access on?

Enabling "私有化访问" (Private access) in "网站配置" (Site settings) requires a subscription key. Once enabled:

- The site and its APIs require a sign-in or the correct subscription key.
- Subscription URLs (`/api/provide/app`, `/api/provide/tvbox`, `/api/provide/vod`) always require the subscription key.

The key can be passed as a URL parameter or in the `X-Provide-Key` request header. Example with the key `mysecret888`:

```text
http://<server-ip>:3000/api/provide/tvbox?key=mysecret888
```

## Related documents

[Overview](./README_EN.md) · [Deploy guide](./README-Deploy_EN.md) · [Scheduled task conflicts](./cron-task-conflict_EN.md)
