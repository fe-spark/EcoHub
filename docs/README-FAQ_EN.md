# FAQ

[中文](./README-FAQ.md) | English

Install, ports, and passwords: [Deploy guide](./README-Deploy_EN.md).

## Collect and data

### Why is the site empty after setup?

Because nothing has been collected yet. The release image ships no media data. The first start only writes 12 preset collect sources. It does not run a full collect by itself.

In admin, open "采集中心" (Collect center) and run one full collect. More collect sources take longer, and the first run can take several hours. You do not have to wait for the end: after each batch is collected, it is published to the snapshot the site reads, so films from the main source appear while the collect runs.

The site lists, the admin film list, and the TVBox / MacCMS subscriptions all read the snapshot, not the tables being collected. Until the first batch is published, the site is empty. That is expected.

### What is the main source?

The main source is the first enabled collect source in the sort order. The site's home page, categories, search, and daily updates show only films and categories collected by the main source.

Example: the collect sources are ordered A, B, C, all enabled, so the main source is A. Drag B to the top and the site switches to the data B already collected. Nothing is re-collected, and A's and C's data is not deleted. When every collect source is disabled, there is no main source.

The play page starts from the main source's film, uses match keys to find the same film in other enabled collect sources, and appends their play lines. Play lines are shown per collect source.

The poster source is a separate flag and does not decide the main source. Site covers use the matching film's cover from the poster source first, and fall back to the main source's own image when nothing matches. When the poster source is the main source, its own image is used.

### What does switching the main source clear?

Nothing. No films are deleted, nothing is re-collected, and you can switch while a collect is running.

Homepage banners are stored per collect source. After switching, the home page shows the new main source's own banner set. The old set is still there and comes back if you switch back.

Do not confuse this with:

- Changing a collect source's URL: clears that source's films, play lines, failure records, and categories (films without play lines too), then fetches categories again. Other sources are not touched.
- Deleting a collect source: also clears only that source.

### How are films deduplicated and matched across sources?

Each collect source keeps its own copy. Collect does not merge them.

- Within one source: `movie_source_mapping` uses "collect source + the source's `vod_id`" to find the film's existing row. Collecting again updates only that row. Entries on the same page of the same source with the same match key become one film.
- Across sources: match keys do not merge rows. The play page uses them at read time to find the same film in other sources, in this order: Douban ID + title, title + top-level category, title only. A film hit by several keys counts once. Play lines stay per source; episodes from different sources are never stitched together.

The category key is `source:{collect source ID}:{source category ID}`, one-to-one with that source's own category tree.

### Where do scrape results and homepage banners live?

- Film scrape: writes the chosen cover, blurb, score, and similar fields directly onto the film. Title and category stay. There is no "restore".
- Homepage banners: stored per collect source. Each row keeps only the film, the order, and any image, blurb, score, or year edited in admin. Everything else is read from the library when shown. A scrape inside the banner editor writes only that banner row. It does not change the film, and there is no separate scrape table.

### What state is the data in after a manual stop?

Stop interrupts pages that are still being fetched. Pages already stored stay in the library. A page that is being written finishes; pages still waiting to be written are dropped. Failed pages are recorded in "采集记录" (Collect records) and can be retried.

## Snapshots and cache

### Why publish snapshots during a collect?

Because the site does not read the tables being collected. Those tables change all the time during a collect. After each batch, that batch is written into the snapshot and the in-memory search index is updated film by film. Site lists, filters, the admin film list, and TVBox / MacCMS all read the snapshot.

After publishing, list-cache cleanup is debounced by 5 seconds: during many batches in a row Redis is not cleared after every batch, but once, about 5 seconds after the last batch.

The search index is normally updated incrementally. It is fully loaded once at start. If an incremental update finds no usable index in memory, a full rebuild runs as a fallback.

### I changed a setting. Why did the site not change right away?

Saving site settings updates Redis and clears the home page cache. If the page still looks old, common causes are:

- Browser, CDN, or reverse proxy caching.
- The running code or image is not the latest.

Film lists, categories, and filters show only the current main source. After switching the main source, the site shows the films the new main source already has. If it has not collected anything yet, the site is empty.

The regular TVBox lists have another Redis cache layer, up to about 12 hours, cleared with a debounce after new data is stored. If results still differ, check that you are hitting the same instance with the same request parameters.

### How is "最近更新" (recently updated) sorted? Do category pages and TVBox agree?

By `film_index.update_stamp`, newest first, then by `mid`.

- New films: use the update time given by the collect source.
- Films already in the library: during collect, the time changes to the current collect time only when play lines changed. Changes to download lines, remarks, cast, or cover do not change the order.
- Saving a film manually in admin also sets its time to the save time.

All collect sources follow the same rules.

The site category page and TVBox take their filter options from the same filter data and use the same matching rules. The site queries the in-memory index; TVBox queries MySQL:

| Filter | Rule | Same on both? |
| --- | --- | --- |
| Category | Expanded by the source's category rules into the same set of source categories (including merged groups); only films with a play line from that source | Yes. The site uses the main source; TVBox uses each subscribed site's own source |
| Genre (剧情) | The film's genre tags contain the selected value | Yes |
| Region, language | Exactly equal to the selected value | Yes |
| Year | Equal to the selected year; no filter when the value is not a number | Yes |
| "其他" (Other) | Value not in the filter options; empty values count too | Yes |
| Sort | Recently updated, popularity, rating, year. When sorting by year, films in the same year are then ordered by update time | Yes |

Both sides have a Redis cache with different lifetimes: site filter results 2 hours; TVBox list pages without genre, region, language, or year filters about 12 hours, and with filters 3 minutes. When they briefly differ, check the cache first.

## Network

### Which requests use the admin network proxy?

A module's requests go through the proxy only when the global proxy is on in admin → "网络代理" (Network proxy, `/manage/system/proxy`) and that module's switch is on too. Everything else connects directly. Container environment variables do not affect outbound requests.

| Module (admin name) | Requests it covers | Default on new installs |
| --- | --- | --- |
| 影视采集爬虫 (collect spider) | Collect source APIs. Each collect source also needs "采集接口代理" (collect API proxy) turned on in its edit page | On |
| TMDB 刮削 (TMDB scraping) | Requests to the TMDB API (including the admin connection test) | On |
| Telegram 机器人 (Telegram bot) | Notifications and polling for the paging buttons | On |
| 系统版本更新 (version update) | GitHub version check | On |
| 弹幕抓取 (danmaku fetching) | Danmaku search and download from the official dandanplay API or a self-hosted danmu-api (including the "测试" (Test) button in the admin danmaku settings) | Off |

"弹幕抓取" is off by default because a self-hosted danmu-api is often on the local network, and sending it through the proxy would break it. After upgrading from an older version it is also off, so danmaku keeps connecting directly; the other modules keep their saved switches.

Example: a proxy runs on port `7890` on the host, and you want the official dandanplay API to go through it. Enter `http://host.docker.internal:7890` (inside the container, `127.0.0.1` is the container itself), turn on the global switch, then turn on "弹幕抓取".

## Danmaku

### Network danmaku does not show up. How do I check?

First try the "测试" (Test) button in admin → "网站配置" (Website) → "基本信息" (Basic) → "弹幕系统配置" (Danmaku settings), then look at the load status on the player's danmaku input box.

Network danmaku needs all of these: "启用弹幕系统" (enable danmaku) and "自动嗅探全网弹幕" (auto-fetch network danmaku) are on, and either a self-hosted danmu-api address or the dandanplay AppId and AppSecret is filled in.

**Test button**: appears once "自动嗅探全网弹幕" is on. Enter a title and click "测试". It uses the settings currently in the form (they do not need to be saved yet) to fetch network danmaku for episode 1 of that title, and shows the result under the button, for example "匹配到「葬送的芙莉莲 第1话」，第 1 集共 N 条全网弹幕" (matched, N network comments) or "失败：弹幕接口请求超时" (failed: request timed out). The test neither reads nor writes the danmaku cache. Whether it uses the proxy follows the "弹幕抓取" switch in "网络代理", the same as the player.

**Load status on the input box**: danmaku load status is shown on the danmaku input box in the player control bar, not over the video. The grey placeholder text gives the result, a small dot on the left shows the state, and hovering over the input or the dot shows the full message (including the failure reason).

| Dot | Placeholder | Meaning |
| --- | --- | --- |
| Grey, blinking | 弹幕加载中… | Loading danmaku |
| Green | 共 N 条弹幕，说点什么吧 | Site and network danmaku both loaded; N is the total. Hover shows "已加载 N 条弹幕（全网 M 条）" (N loaded, M from the network) |
| Yellow | 共 N 条弹幕，未找到全网弹幕 | No matching title, or this episode has no network danmaku; with no comments at all it shows "未找到全网弹幕，说点什么吧" |
| Red | 全网弹幕加载失败：reason | Timeout, unreachable, auth failure, unreadable response, and so on; cut off with an ellipsis when too long, hover for the full reason and the site comment count |
| Red | 弹幕加载失败：reason | The danmaku request itself failed (the API returned an error, or the network dropped) |
| Hidden | 共 N 条弹幕，说点什么吧 | Network danmaku is off; only comments sent on this site. With none at all it is the plugin default "发个友善的弹幕见证当下" |

Switching episodes rebuilds the player, so the status starts again from "弹幕加载中…".

The reason is a human-readable message and never contains the API address or keys. Network danmaku that loads successfully is cached for 7 days per API address, title, and episode. No-match and failed results are not cached, so the next play retries.

### Are danmaku settings saved? Why is there no input box on phones?

Yes. The switch, opacity, font size, speed, display area, danmaku types, and anti-overlap set in the player's danmaku settings are stored in the current browser and reused when you switch episodes or come back. A different browser, or clearing site data, goes back to the defaults.

On phones (the browser reports iPhone, iPad, iPod, or Android, or the window is 768 px wide or less) only the danmaku switch and settings are shown, with no input box, so you cannot send danmaku from a phone. Phones cannot show the status on the input box either: when danmaku fails to load, a short notice with the reason appears over the video once; other states show nothing.

## Login and permissions

### The admin page opens, but API calls say I am not logged in. Why?

The web app only does a rough check. The real check is on the server:

1. `web/src/proxy.ts` only checks that the `ecohub_auth_token` cookie exists.
2. When `/manage` opens, the server-side layout calls `/api/manage/user/info`. The API checks the JWT and the current token stored in Redis. On failure it redirects to `/login`.
3. If the token becomes invalid after the page has opened (expired, replaced by a login on another device, or cleared in Redis), later API calls return "not logged in" and the web app goes back to the login page.

The API response is always the source of truth. A login is valid for 10 days. After that, log in again.

### How do the default account and visitor accounts work?

The first start creates only the super user `admin`. Its password is `ECOHUB_ADMIN_PASSWORD`, or `admin` when unset. If `admin` already exists, its password is not changed. Change the password in admin right after you log in.

Create other accounts in "账号管理" (Accounts). There are three roles:

| Role | What it can do |
| --- | --- |
| 超级用户 (super user) | Everything, including site settings, accounts, and upgrades |
| 普通用户 (regular user) | Day-to-day management, but not the super-user-only actions |
| 访客 (visitor) | Read only. All GET requests pass; POST / PUT / PATCH / DELETE are rejected |

### How do subscriptions work with private access on?

When you turn on "私有化访问" (private access) in "网站配置" (Site settings), you must fill in a subscription key. After that:

- The site and its APIs need a login or the correct subscription key.
- Subscription URLs (`/api/provide/app`, `/api/provide/tvbox`, `/api/provide/vod`) always need the subscription key.

Pass the key as a URL parameter or in the `X-Provide-Key` request header. For example, with the key `mysecret888`:

```text
http://<server-ip>:3000/api/provide/tvbox?key=mysecret888
```

## Related

[Overview](./README_EN.md) · [Deploy guide](./README-Deploy_EN.md) · [Scheduled task conflicts](./cron-task-conflict_EN.md)
