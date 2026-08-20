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

- **Master identity** (`film_index.content_key`): `vod_{id}` when the source `vod_id` exists, otherwise a title hash. Different `vod_id`s on the master stay as two rows even if the title or Douban ID matches.
- **Cross-site match** (`movie_match_key`): how slaves attach to a master film. Douban identity first (including season/segment in the title), then a normalized title. If one key hits several mids, the newer `update_stamp` wins.

### What happens if a collect is stopped manually?

Stop interrupts tasks that are still fetching pages and cancels their context. Sources already in `page_done` / `waiting_publish` still finish and publish.

A **master full** collect that errors or is stopped does **not** publish the pending mids from that run (they are discarded). The database may already contain partial writes, but the public read model stays on the previous snapshot. A master increment or a slave collect that already wrote rows still finalizes and publishes that increment.

## Snapshots and cache

### Why publish a snapshot after collect? Why can an incremental publish still take time?

Tables keep changing during collect. Finalize refreshes play summaries, publishes the list snapshot, and swaps the in-memory read model. Public lists, filters, the admin film list, and TVBox / MacCMS all read that result.

An incremental publish still processes affected mids: snapshot rows, filter indexes, and the read model. A large catalog costs database time and memory. The read model stays in memory for the whole library; it is not only a short spike during publish.

### Why didn’t the public site change after a config edit?

Saving site config updates Redis and clears the home-page cache. If the page still looks old, typical causes are browser, CDN, or reverse-proxy cache, or a process that is not running the latest code.

Film lists, categories, and filters follow the snapshot / read model. Changing sources does not fill the public site until collect finishes and a snapshot is published. TVBox plain lists have an extra Redis cache (up to about 12 hours), which is cleared on publish. If they still differ, check the running instance and request parameters.

### What does “recently updated” use? Do category pages match TVBox?

Recently updated sorts snapshots by `update_stamp` descending (then `mid`). That stamp is not refreshed on every master-field change:

- Master: new titles, or a strictly higher episode count. Remarks, cast, and cover do not move the sort.
- Slave: only when the film already matches a master mid **and** this source’s episode count is strictly higher than the historical maximum. Adding extra play lines, changing URLs, or changing the last-episode label without a higher count does not change the sort. A slave’s first attach keeps the master’s existing stamp.

Public category filters and TVBox share the same read-model rules (category, plot / region / language / year, sort). TVBox plain lists add a Redis cache, so short-lived mismatches are usually cache.

## Login and permissions

### Why can admin pages open while APIs report not logged in?

The edge middleware only checks that the `ecohub_auth_token` cookie exists. Opening `/manage` also has the server layout call `/api/manage/user/info`, where the backend validates the JWT and Redis token; failure redirects to `/login`. If a token later expires, is replaced by another device, or disappears from Redis while the admin page is already open, subsequent APIs may still return unauthorized. Trust the API response.

### How do guest and default accounts work?

Built-in accounts: `admin` / `admin` (read/write), `guest` / `guest` (visitor). Visitors may call admin GET; POST / PUT / PATCH / DELETE are rejected by `WriteAccess`. Default accounts are for first boot and demos. Change the passwords before any public deployment.
