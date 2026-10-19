# FAQ

[中文](./README-FAQ.md) | English

## Collect and data

### Why is the site empty after setup?

The release image does not ship catalog data. First boot writes default collect sources, but it does not finish a collect by itself. Run collect in the administration panel. The first full collect may take several hours; more sources take longer.

The public site, the admin film list, and TVBox / MacCMS all read the snapshot / in-memory read model published after collect, not the tables while collect is still running. Films from the main source appear as each window is published. An empty site before that publish is expected.

### What is the main source?

The main source is the first enabled collect source in sort order. The public home page, categories, search, and daily updates show only that source’s own collected films and categories. Moving another enabled source to the front shows that source’s stored data immediately. Nothing is re-collected, and other sources’ films stay.

The play page starts from the main source’s film and appends play lines already stored by other enabled sources, matched by key. Lines stay separated by source. The poster source is a separate flag and does not decide the main source. Public covers use the matched poster-source film’s current cover. The main film’s image is used only when nothing matches. When the poster source is the main source, its own image is used. If every source is disabled, the same order picks the first one as a fallback.

### Where do scrape results live?

A film scrape writes the selected cover, blurb, score, and related fields onto that film. The title and category stay. There is no restore. A homepage banner scrape is stored on that banner row. It does not change the film, and there is no second scrape table. The homepage uses the banner row’s own image and blurb. The update remark still comes from the library.

### What does switching the main source clear?

Films are not deleted and collect does not restart. The live banner deck and the saved scheduling recipe are cleared, and the scheduled banner job is turned off. Switching is allowed while a collect is running.

Changing one source’s collect URL clears that source’s films, play lines, failure records, and categories, including films with no play lines, then fetches categories again. Other sources stay. That is separate from switching the main source. Deleting a source clears only that source.

### How are titles deduplicated and aligned across sites?

Each source keeps its own film row. Collect does not merge rows.

- Inside one source, `movie_source_mapping` finds the old row by source id and source `vod_id`. A later collect updates that row only. Items on the same page that share a match key become one film.
- Across sources, match keys do not merge rows. The play page uses them to find the same film on other enabled sources and append those sources’ own lines. Order is Douban ID plus title, then title#category, then bare title. Several keys hitting one film show it once. Lines stay separated by source.

Category keys are `source:{this source}:{typeId}` and match that source’s own category tree.

### What happens if a collect is stopped manually?

Stop interrupts tasks that are still fetching pages and cancels their context. Sources already in `page_done` / `waiting_publish` still finish and publish.

Successful pages are already in `film_index`. Stopping or failing later pages leaves those films visible; failed pages go to the retry queue.

## Snapshots and cache

### Why publish a snapshot after collect? Why can an incremental publish still take time?

Tables keep changing during collect. Each window refreshes play summaries for those mids and updates the in-memory search index. Public lists, filters, the admin film list, and TVBox / MacCMS read `film_index`. List caches debounce about 5 seconds so a full collect does not flush Redis on every page.

### Why didn’t the public site change after a config edit?

Saving site config updates Redis and clears the home-page cache. If the page still looks old, typical causes are browser, CDN, or reverse-proxy cache, or a process that is not running the latest code.

Film lists, categories, and filters show only the current main source’s own films. Switching the main source shows films that source has already stored. A source that has not been collected yet stays empty. TVBox plain lists have an extra Redis cache (up to about 12 hours), which is cleared after collect. If they still differ, check the running instance and request parameters.

### What does “recently updated” use? Do category pages match TVBox?

Recently updated sorts `film_index.update_stamp` descending (then `mid`). A new film keeps the time given by its source. An existing film moves to the current collect time only when its play-line content changes. Download-only lines, remarks, cast, and covers do not move the sort. Every collect source uses the same rule.

Public category filters and TVBox share the same read-model rules (category, plot / region / language / year, sort). TVBox plain lists add a Redis cache, so short-lived mismatches are usually cache.

## Login and permissions

### Why can admin pages open while APIs report not logged in?

The edge middleware only checks that the `ecohub_auth_token` cookie exists. Opening `/manage` also has the server layout call `/api/manage/user/info`, where the backend validates the JWT and Redis token; failure redirects to `/login`. If a token later expires, is replaced by another device, or disappears from Redis while the admin page is already open, subsequent APIs may still return unauthorized. Trust the API response.

### How do guest and default accounts work?

First boot creates only the super admin `admin` / `admin`. Create normal users and visitors in account management. Visitors may call admin GET; POST / PUT / PATCH / DELETE are rejected by `WriteAccess`. Change the default password before any public deployment.
