# FAQ

[中文](../../guide/faq.md) | English

For installation, commands, and environment variables, see the [Deploy guide](./deploy.md) and [Operations and configuration](./operations.md). For internal schemas, caching rules, and scaling formulas, see [Core mechanics and data flow](../dev/architecture.md).

## Collect and usage

### Why does the site show no films after deployment?

The release image contains no media data. On first startup, EcoHub seeds only the 12 preset collect sources and does not start a full collect automatically.

Open "采集中心" (Collect center) in the admin panel and run a full collect. Waiting for completion is not necessary: as batches finish writing and publishing, list caches invalidate after a short debounce and films from the main source appear progressively, while the full search index updates after all active collect queues finish.

### What are the main source and poster source?

- **Main source**: the active source chosen via the admin "主站" (Main source) selector. The site's home page, categories, search, and daily updates display only films and categories from this source. Disabling or deleting the main source requires picking a new one; reordering sources does not change it.
- **Cross-source play lines**: starting from the main source's film, the play page finds matching films from other enabled sources and groups their lines below by source.
- **Poster source**: an independent flag for site covers. Covers prefer the matching film from the poster source and fall back to the main source's own image when unavailable.

### What data is cleared when the main source is switched?

None. Switching the main source does not delete films or trigger re-collect, and works even during an active collect.

Both "首页轮播" (Homepage banners) and "首页推荐" (Homepage recommendations) are stored per collect source and switch along with the main source without losing previous configurations. By contrast, **changing a source's URL** or **deleting a source** clears that source's films, play lines, failure logs, and categories without affecting other sources.

### What state is the library in after a manual stop?

Stopping interrupts pages currently being fetched. Already saved films remain in the library, active write transactions complete, and queued batches are stopped or discarded. Failed pages are logged in "采集记录" (Collect records) and can be retried in the admin panel (batches older than 30 minutes automatically fall back to source-level hour retries).

### Where are scrapes, banners, and recommendations saved?

- **Film scraping**: writes the chosen cover, synopsis, rating, and related fields directly to the film while preserving title and category; changes cannot be automatically reverted.
- **Homepage banners and recommendations**: managed per collect source under "首页管理" (Homepage). Edits or scrapes inside a banner override only that banner item and do not overwrite library records or get overwritten by routine collects. Recommendation groups bound to Douban or Bangumi charts sync automatically against library titles when charts update or collects finish.

### Why do edits or new collects not show up immediately?

Saving site settings clears the home page cache immediately. Remaining stale content usually comes from:

- Browser, CDN, or reverse proxy caching.
- Inspecting another source while the current main source has no data yet.
- Independent list, filter, and TVBox caches (which clear with an approximate 5-second debounce during streaming collects); wait a few seconds or verify request parameters and instances.

## Network proxy

### Which requests use the admin network proxy?

Outbound requests use a proxy only when the global proxy switch is on in "网络代理" (Network proxy, `/manage/system/proxy`) AND that module's switch is enabled; otherwise requests connect directly. Container environment variables do not control outbound requests. Supported schemes are `http://`, `https://`, `socks5://`, and `socks5h://`.

| Module (admin name) | Covered requests | Default on new installs |
| --- | --- | --- |
| 影视采集爬虫 (Collect spider) | Collect source APIs; each source must also enable "采集接口代理" on its edit page | On |
| TMDB 刮削 (TMDB scraping) | TMDB API requests (including admin test) and Douban / Bangumi chart fetches for recommendations | On |
| Telegram 机器人 (Telegram bot) | Notifications and interactive callback polling | On |
| 系统版本更新 (Version update) | GitHub version check | On |
| 弹幕抓取 (Danmaku fetching) | Official dandanplay API or self-hosted danmu-api requests (including the admin "测试" button) | Off |

"弹幕抓取" defaults to Off because self-hosted danmu-api servers usually run on a local network and break if routed through an external proxy.

Example: to proxy dandanplay through host port `7890`, enter `http://host.docker.internal:7890` (`127.0.0.1` inside a container points to the container itself), enable the global toggle, and enable "弹幕抓取".

## Danmaku

### How do I troubleshoot missing network danmaku?

First click "测试" (Test) under "网站配置" → "基本信息" → "弹幕系统配置" in the admin panel, then check the status indicator inside the player's danmaku input box.

Network danmaku requires enabling both "启用弹幕系统" and "自动嗅探全网弹幕", plus either a self-hosted danmu-api URL or dandanplay AppId and AppSecret. The "测试" button fetches episode 1 using current form inputs without reading or writing cache, respecting the "弹幕抓取" proxy switch.

Input dot and placeholder meanings:

| Dot | Placeholder | Meaning |
| --- | --- | --- |
| Blinking gray | 弹幕加载中… | Fetching danmaku |
| Green | 共 N 条弹幕，说点什么吧 | Site and network danmaku loaded; hover for network count |
| Yellow | 共 N 条弹幕，未找到全网弹幕 | Title unmatched or episode has no network danmaku |
| Red | 全网弹幕加载失败 / 弹幕加载失败 | Timeout, unreachable API, or auth error; hover for user-facing reason |
| Hidden | 共 N 条弹幕，说点什么吧 | Network danmaku off; only local site comments are shown |

### Are danmaku settings saved and how do narrow players behave?

Yes. Toggle, opacity, font size, speed, display area, type filters, and overlap protection persist in the local browser across episodes and visits until site data is cleared or another browser is used.

The player adapts dynamically to its own rendered width regardless of device user agent:

| Player width | Control bar | Load status | Fixed top/bottom danmaku |
| --- | --- | --- | --- |
| Below 560 px | Input box and web fullscreen toggle collapsed (exit button remains when in web fullscreen) | Dot at top-right of danmaku switch; brief on-screen error notice | Converted to scrolling danmaku |
| 560–767 px | Input box shown and narrows; web fullscreen toggle collapsed | Placeholder text and dot inside input box | Converted to scrolling danmaku |
| 768 px and up | Full control bar | Placeholder text and dot inside input box | Kept fixed top/bottom |

Font size scales smoothly relative to a standard desktop baseline and converts back to that baseline when saved. Display area margins measure from the top of the player container so letterbox bars can absorb danmaku and reduce obscuring the video.

## Login and permissions

### Why does the admin panel open yet API calls say "not logged in"?

The frontend performs only a quick presence check on the browser cookie; the backend validates sessions authoritatively. Signing in elsewhere replaces the previous session, or once the cookie expires (10 days by default), requests redirect back to `/login` for another sign-in.

### How do the default account and roles work?

First startup creates only the super user `admin` using `ECOHUB_ADMIN_PASSWORD` (default `admin`). Change it immediately after the first sign-in. Additional users are managed in "账号管理" (Accounts):

| Role | Permissions |
| --- | --- |
| 超级用户 (Super user) | Full access including site settings, account management, and upgrades |
| 普通用户 (Regular user) | Routine management, excluding super-user operations |
| 访客 (Visitor) | Read-only; all write requests are rejected |

### How do subscriptions work with private access on?

When "私有化访问" (Private access) is turned on under "网站配置" → "基本信息" → "运行与访问控制", a subscription key is required:

- Site pages and site APIs require an active sign-in or the subscription key.
- Subscription URLs (`/api/provide/app`, `/api/provide/tvbox`, `/api/provide/vod`) always require the subscription key.

Pass the key as `?key=` or in the `X-Provide-Key` header. Example with key `mysecret888`:

```text
http://<server-ip>:3000/api/provide/tvbox?key=mysecret888
```

## Related documents

[Deploy guide](./deploy.md) · [Operations and configuration](./operations.md) · [Core mechanics and data flow](../dev/architecture.md)
