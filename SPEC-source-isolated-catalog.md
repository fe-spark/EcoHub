# EcoHub: Source-Isolated Catalog & Federated Playback Specification

Each source maintains an independent catalog record. The storefront strictly projects the catalog and taxonomy of the active primary source. The playback page dynamically federates streams from other enabled sources at read time using match keys. Multi-source entities are never consolidated into a single global catalog record.

**Scope**: `server`, `web`, TVBox / CMS. Excludes `app-for-android`, `app-for-ohos`, and `docs/RELEASE.md`.

---

## 1. Core Principles

1. **Implicit Primary via Min-Sort**: Among all enabled sources, the source with the lowest `sort` order is designated as the primary source. There is no `is_primary` database column, nor a manual "Set as Primary" action. The interface refers to it as the "Primary Source". When all sources are disabled, the source with the lowest `sort` order acts as the fallback.
2. **Strict Stream Isolation**: Playlists and streams are stored and presented strictly by their owning source. Cross-source episode interleaving or playlist patching is strictly prohibited.
3. **Master-Locked Storefront**: Home page sections, taxonomy listings, search results, and daily updates exclusively project films ingested by the primary source. Visitors cannot switch sources, and public endpoints ignore any `source` query parameters.

---

## 2. Catalog & Records (`film_index`)

- Each source maintains its own independent `film_index` record. The `first_source_id` column represents the owning source ID. The same title across two sources produces two distinct `mid` records.
- Intra-source identity resolution relies strictly on `movie_source_mapping` (`source_id` + upstream `vod_id`). Subsequent ingestion runs only update the source's own records. Within the same source and page, items sharing identical match keys are merged into a single record.
- Match keys are never used to resolve, merge, or overwrite another source's `mid`, title, category, poster, or description. Match keys are stored on the source's own record solely for read-time stream federation and poster resolution.
- Category namespace keys follow the format: `source:{source_id}:{type_id}`.

---

## 3. Storefront vs. Backoffice

- **Storefront**: Navigation bars, home page carousels/sections, category views, filter panels, search, and daily updates are strictly scoped to the primary source. Missing or client-supplied `source` parameters are ignored.
- **Taxonomy Views**: Category listings contain only the primary source's own category keys. Categories with identical names from other sources are never merged into the view.
- **Backoffice**: Film management in the admin console isolates catalog records by the explicitly selected source, remaining completely decoupled from the storefront master lock.
- **TVBox / CMS APIs**:
  - `/api/provide/tvbox`: Lists only enabled sources, with the active primary source ranked first. No global aggregate entry is provided.
  - `/api/provide/vod`: Defaults to the primary source when `source` is omitted; queries the specified source when provided. The detail endpoint ignores this parameter and federates playlists across all enabled sources via match keys.

---

## 4. Playback & Stream Federation

- The playback page renders the current catalog record's native playlists first, followed by federated playlists from other enabled sources matched via resolution keys.
- **Match Key Cascade**:
  1. Douban ID + Normalized Title
  2. Normalized Title + Primary Category
  3. Pure Normalized Title
- Multiple keys matching the same upstream title yield only one instance. Playlists are sorted primarily by source `sort`, and secondarily by intra-source playlist index. Cross-source episode mixing is forbidden.
- **Direct Streaming**: Posters and media streams connect directly to upstream sources without server-side reverse proxying. When a source has upstream proxying enabled, its playlists are flagged with a "Proxy" label.

---

## 5. Taxonomy & Normalization

- Each source maintains an isolated category mirror snapshot (`source_categories`), fetched upon source creation or URL modification. Toggling source status or reordering the primary source does not re-fetch categories. Sources lacking a category snapshot cannot ingest, and categories are never fetched ad-hoc during ingestion.
- `source_categories` preserves raw upstream category names. `category_mappings` maps upstream `type_id` values to local standard categories. Same-name categories across different sources retain separate rows and are never merged by name.
- The storefront recognizes only the active primary source's taxonomy. After switching the primary source, legacy category IDs belong to the new source; if unmapped, the page renders with the title "Category Stale".
- Category rewrite rules only alter the storefront presentation of the active primary source. Exact match is evaluated first, followed by regex rules in sequential order. A matched rule rewrites the display name; items with identical normalized names under the same parent category are consolidated into a single storefront category. Unmatched categories retain their raw names. Hidden categories do not enter the storefront and do not participate in consolidation. Saving or deleting rules takes effect upon page refresh without requiring re-ingestion or altering persisted category keys.
- Categories cannot be deleted—only shown or hidden. Disabled top-level categories are omitted from navigation and home sections; direct URL visits can still access the catalog.

---

## 6. Posters, Scraping & Banners

- Storefront posters are resolved dynamically at read time rather than baked into catalog records during ingest. The deprecated `movie_poster` table is unused.
- The **Designated Poster Source** is the enabled source flagged with `is_poster_source`, ordered by `sort ASC`, `created_at ASC`, `id ASC`. If no source is flagged, the top-sort enabled source serves as fallback.
- **Display Poster Resolution**:
  - Prioritizes the poster source's final image: Custom / Pinned Poster -> Scraped TMDB Image -> Ingested Source Image.
  - Resolves via `movie_match_key` using the same cascade hierarchy as playback federation.
  - Empty images do not overwrite existing ones.
  - If the owning source is itself the poster source, it uses its own image.
  - If unmatched, it falls back to the record's own image.
  - Custom or scraped covers on the primary record do not bypass this late-binding resolution layer. `film_index.picture` is never overwritten by external sources.
- **Film Editing & Field Locking**:
  - The "Custom Poster / Follow Poster Source" toggle controls whether the film's poster continues to receive ingestion updates. Scraped covers count as custom covers. Switching back to "Follow Poster Source" clears the custom flag while preserving scraped body text.
  - TMDB scraping writes selected fields directly into `film_index`. Title and category remain immutable. Rollback is not supported.
  - `film_tmdb_scrapes` records only `mid`, `tmdb_id`, and `media_type` to track scraped status and support re-scraping.
  - Re-ingestion preserves scraped columns tracked under `film_index.locked_fields` (internal metadata, hidden from APIs and UI).
- **Home Banner Scraping**:
  - Banner scraping operates in-place directly on `banners_record` rows: poster, backdrop, overview, rating, year, and `tmdb_id`.
  - Does not mutate film records, does not write to `film_tmdb_scrapes`, and requires no separate banner scrape table.
  - The storefront renders banner-specific media, overview, and rating, while episode update status tracks the catalog in real time.
  - When a banner row lacks both custom media and `tmdb_id`, the cover falls back to the film's current display poster.
  - Unsaved form autofill can be reverted via "Revert Autofill".

---

## 7. Master Failover, Deletion & Endpoint Changes

### 7.1 Master Failover
When drag-sorting or toggling changes the primary source:
- Storefront catalog listings, categories, search index, and daily updates immediately project the new primary source's records.
- Online banners and scheduling rules are cleared, category version is bumped, and search/inventory caches are invalidated.
- No catalog records are deleted, no playlists are migrated, no automated ingestion is triggered, and no categories are re-fetched.
- Ingestion in progress is not interrupted; ongoing page transactions continue writing to their respective source catalogs.
- Confirmation modal prompt: "Clear Banners and Scheduling".

### 7.2 Source Deletion & Purging
Deleting a source, updating its ingest URL, or selecting "Clear Source Data" triggers a source-scoped cascade purge limited exclusively to that source:
- Purges source catalog records (including streamless records), playlists linked to those `mid`s, match keys, mappings, banners, scrape records, failure logs, category mirror snapshots, and cron associations.
- Other sources remain completely untouched. Total master inventory reaches 0 only when all sources are deleted.

### 7.3 Ingest URL Changes
Updating a source's API endpoint purges only that source's data, then fetches categories from the new endpoint. If the fetch fails, the new URL remains saved, the source status becomes "Missing Category", and ingestion remains disabled. Modifying endpoints during active ingestion is prohibited. This operation is distinct from master failover.

---

## 8. Catalog Metrics & Telemetry

- **Playable Master Inventory**: Total count of active primary source records (unplayable entries lacking stream endpoints are filtered at ingestion, ensuring 100% playable capability).
- **Multi-Source Federated Films**: Number of primary source records enriched with multi-source coverage or secondary backup streams.
- **Per-Source Inventory**: Total catalog records owned independently by each source. The aggregate sum across all sources may exceed master inventory.
- **Admin Dashboard**: Displays the master metrics above first, followed by a per-source breakdown of films, categories, and failure logs. Category metrics include only active display categories (including hidden). Sources lacking mappings are labeled "Missing Category". Snapshot counts are deprecated.

---

## 9. Ingestion Pipeline

- **Page-Atomic Transactions**: Each page is processed within an isolated transaction writing only to the owning source's `film_index`, match keys, source mappings, and playlists. Once committed, films are immediately queryable. No snapshot tables are written.
- **Idempotent Ingestion**: Subsequent ingestion resolves records via intra-source mapping or intra-source match key. Playlists are overwritten only when content hash changes. Failed pages trigger a full page retry; partial failures are never committed as successes.
- **Garbage Collection Concurrency Gate**: Orphan cleanup routines are completely bypassed while ingestion is actively writing to the database.

---

## 10. Explicit Non-Goals

1. **No Full Database Wipe on Master Switch**: Never purge the entire database when switching primary sources to wait for new ingestion.
2. **No Primary-Only Ingestion**: Never discard secondary sources if they cannot be matched to primary records.
3. **No Stream-Only Deletion**: Never delete only playlists and defer record deletion until "zero playlists remain".
4. **No Ingest-Time Multi-Source Consolidation**: Never merge multiple sources into a single catalog record during ingestion.
5. **No Asset Cross-Contamination**: Never bake poster source image URLs into another source's records during ingestion.
6. **No Redundant Scrape Duplication**: No secondary scraped copy columns, no banner scrape tables, and no scrape rollback mechanisms.
7. **No Legacy Data Migration**: No migration scripts or cleanup sweeps for historical consolidated data.

---

## 11. Legacy Data Handling

- Historical merged records in legacy databases cannot reliably be disentangled by source. No data migration is provided. Post-deployment, environments should perform a "Data Reset" and re-ingest all sources under the new architecture.
- The `movie_poster` table is deprecated: it is never created, queried, or updated. Existing empty tables in legacy databases are preserved without issuing drop scripts.

---

## 12. Acceptance Criteria

1. **Master Failover**: With two sources ingested, drag the second source to the top sort position. The storefront immediately displays the second source's catalog and taxonomy; matching streams from the first source appear on playback pages. Master inventory is non-zero.
2. **Concurrent Ingestion**: Switching the primary source during active ingestion succeeds without interrupting running background crawlers.
3. **Poster Resolution**: With Source A as primary and Source B as poster source, storefront covers display Source B's current image (including B's scraped images). Switching primary to B while B remains poster source correctly displays B's native image.
4. **Scraping Field Immutability**: Scraping a film writes poster and overview to that specific record without rollback options. Re-ingestion does not overwrite scraped columns. `film_tmdb_scrapes` contains only metadata entries.
5. **In-Place Banner Scrapes**: Scraping a home banner writes metadata directly to the banner row. Film records remain unchanged and `film_tmdb_scrapes` count does not increment. Episode update status tracks catalog updates.
6. **Isolated Source Deletion**: Deleting a source reduces its records, streamless entries, playlists, mappings, match keys, banners, failures, and category snapshots to 0. Other sources remain intact.
7. **Complete Depletion**: Deleting all configured sources results in a master inventory count of exactly 0.
8. **Endpoint Update Isolation**: Updating a source's API endpoint clears only that specific source's data.
