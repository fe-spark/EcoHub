# FAQ

[中文](./README-FAQ.md) | English

## Collect and data

### Why is the site empty after setup?

The release image does not ship catalog data. First boot writes default collect sources, but it does not finish a collect by itself. Run collect in the administration panel. The first full collect may take several hours; more sources take longer.

The public site, the admin film list, and TVBox / MacCMS all read the snapshot / in-memory read model published after collect, not the tables while collect is still running. An empty site before that publish is expected.

### How do master and slave sites differ? Why is a master required?

The master writes film records (`film_index`, details, search entry). Slaves only write playlists. Detail and play pages attach slave sources under a matched master film. Admin can “update all sites” for one film.

Without a master, playlists have nowhere to attach, and the category tree is not built from slaves. Only one master is allowed. Adding or promoting a master demotes the previous one.

### Why rebuild after switching the master?

The master owns film basics, categories, and search. Switching the master, changing its URI, or demoting it stops collect and clears that master’s film data, then rebuilds from the new master so old and new categories, details, and lists do not mix. Slave playlists are kept and rematched after the new master’s full collect.

### How are titles deduplicated and aligned across sites?

Two keys, not “always merge by Douban ID”:

- **Identity**: one film is one `film_index.mid`. A source `vod_id` is bound to that mid through `movie_source_mapping`.
- **Cross-site match** (`movie_match_key`): Douban ID **plus** the normalized title first — the same Douban ID with a different title is not the same film. Title matching strips trailing progress/quality/language noise but keeps version identity (theatrical, 3D, motion comic). Then normalized title#category, plus a bare-title fallback. Category stays on `film_index.pid` / `cid` and is **not** baked into `mid`; the category suffix on the match key keeps same-title films in different categories apart (a finished short drama vs a still-updating cartoon that share a title).

**How play sources bind**: a slave is considered only when its normalized title equals the master’s. Douban ID, year, director, and episode shape **veto** a candidate when both sides have values and they conflict; a missing or unknown value on either side does not veto. After vetoes, a unique remaining title binds immediately. If the master catalog has several films with that title, pick by unique Douban ID, then category, then year (same year, then ±1); if none is unique, do not bind. Bound sources are shown as-is — episode shape is not used again as a display filter.

**Why same-title films must stay isolated**: the daily-update list is driven by `update_stamp`, which only bumps when this source’s episode count is strictly higher than the film’s current global max. If two same-title films share a bare title key, the finished short drama’s episode count is counted against the cartoon, so a new cartoon episode never makes the daily list.

**Collection order is not restricted**: when a slave’s title hits only one master film and is not vetoed, playlists are written onto that film’s stored primary key — including when the slave’s category is wrong. When several master films share the title, pick by Douban / category / year; otherwise they use the slave’s own candidate keys (Douban / title#category). Categorized slaves do not write the bare title key.

### What happens if a collect is stopped manually?

Stop interrupts tasks that are still fetching pages and cancels their context. Sources already in `page_done` / `waiting_publish` still finish and publish.

Successful pages are already in `film_index`. Stopping or failing later pages leaves those films visible; failed pages go to the retry queue.

## Snapshots and cache

### Why publish a snapshot after collect? Why can an incremental publish still take time?

Tables keep changing during collect. Each window refreshes play summaries for those mids and updates the in-memory search index. Public lists, filters, the admin film list, and TVBox / MacCMS read `film_index`. List caches debounce about 5 seconds so a full collect does not flush Redis on every page.

### Why didn’t the public site change after a config edit?

Saving site config updates Redis and clears the home-page cache. If the page still looks old, typical causes are browser, CDN, or reverse-proxy cache, or a process that is not running the latest code.

Film lists, categories, and filters read `film_index` and stay locked to the preferred source. Changing sources does not fill the public site until that source is collected. TVBox plain lists have an extra Redis cache (up to about 12 hours), which is cleared after collect. If they still differ, check the running instance and request parameters.

### What does “recently updated” use? Do category pages match TVBox?

Recently updated sorts snapshots by `update_stamp` descending (then `mid`). That stamp is not refreshed on every master-field change:

- Master: new titles, or a strictly higher episode count. Remarks, cast, and cover do not move the sort.
- Slave: only when the film already matches a master mid **and** this source’s episode count is strictly higher than the historical maximum. Adding extra play lines, changing URLs, or changing the last-episode label without a higher count does not change the sort. A slave’s first attach keeps the master’s existing stamp.

Public category filters and TVBox share the same read-model rules (category, plot / region / language / year, sort). TVBox plain lists add a Redis cache, so short-lived mismatches are usually cache.

## Login and permissions

### Why can admin pages open while APIs report not logged in?

The edge middleware only checks that the `ecohub_auth_token` cookie exists. Opening `/manage` also has the server layout call `/api/manage/user/info`, where the backend validates the JWT and Redis token; failure redirects to `/login`. If a token later expires, is replaced by another device, or disappears from Redis while the admin page is already open, subsequent APIs may still return unauthorized. Trust the API response.

### How do guest and default accounts work?

First boot creates only the super admin `admin` / `admin`. Create normal users and visitors in account management. Visitors may call admin GET; POST / PUT / PATCH / DELETE are rejected by `WriteAccess`. Change the default password before any public deployment.
