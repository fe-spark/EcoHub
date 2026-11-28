# Core Mechanics and Data Flow

[中文](../../dev/architecture.md) | English

This document explains the internal mechanics and data pipelines across the EcoHub backend and frontend for developers and debugging. For user-facing Q&As, see the [FAQ](../guide/faq.md).

## Streaming collect publishing and index reseal

To keep collect throughput steady without list jitter during writes, EcoHub separates storage updates from full search-index updates (`server/internal/collect/` and `server/internal/film/snapshot/`):

1. **Streaming publish (`streamBuf`)**: after writing to `film_index`, changed film IDs enter a bounded buffer (flushing every `300` IDs or `3` s for the main source, `1,000` IDs or `10` s for other sources, with backpressure at `5,000` queued IDs), batch-upsert `search_tag_item`, immediately delete affected `PlayInfo` Redis keys, and trigger a `5`-second debounced Redis list cache cleanup.
2. **Search index reseal (`reseal`)**: rather than acquiring locks to rebuild after every batch, the in-memory search and filter index reseals once after all active collect queues finish—patching incrementally when changes are below `50,000` films and performing a sequential full rebuild at `50,000+` changes or when no usable index exists.

## Deduplication, cross-source matching, and scraping

- **Same-source deduplication**: `movie_source_mapping` locates existing records by `collect source + source vod_id`. Duplicate `vod_id` values on the same page keep only the last entry; distinct `vod_id` entries are not merged by match keys during collect. Category keys follow `source:{collect source ID}:{source category ID}`.
- **Cross-source matching**: at read time (play page and poster resolution), match keys are checked in order: `Douban ID + title` -> `title + top-level category` -> `title`. Without a Douban ID match, normalized top-level category compatibility (`peerCategoryCompatible`) is checked and films in hidden categories of non-primary sources are excluded. Multi-key hits count once, and play lines remain grouped per source.
- **TMDB scraping and homepage data**: film scraping updates `film_index` directly and locks modified columns while storing `(mid, tmdb_id, media_type)` in `film_tmdb_scrapes`. Banners (`banner_slot`) and recommendation groups (`home_recommend_group`, `home_recommend_item`) are scoped per collect source; Douban and Bangumi chart groups cache raw feeds for `6` hours and sync on collect-done hooks (`RegisterCollectDoneHook`).

## Collect records and retry model

- Runs start with a `running` row in `collect_records` and finalize per-source counts when writing completes. Single-film updates store target IDs in `collect_records.target_mids`.
- Writes to `film_index` update `last_record_id` (indexed by `idx_film_index_source_record_mid`). Telegram batch notifications paginate `film_index` by main source and `record_id` cursor with a two-level category/film menu.
- Retries replay recorded failed pages within `30` minutes of completion/start and degrade to source-level hour retries after `30` minutes. Finished records older than `30` days are pruned in batches on startup and daily at `03:30`; `MarkInterruptedCollectRecords` marks stale `running` rows as interrupted on restart.

## Read model, sorting, and Redis caching

Default ordering sorts by `film_index.update_stamp` descending, then `mid`. New films take the upstream timestamp; existing films update `update_stamp` only when play lines change or when saved manually in the admin panel.

For current main-source queries with an active read model, both the site category page and TVBox use the same `tagPageFromIndex` in-memory index (TVBox falls back to MySQL `film_index` only for non-primary queries or when the read model is unavailable):

| Query path | Cache store and TTL | Invalidation / bypass |
| --- | --- | --- |
| Site filter pages | Redis `2` hours (`1` min empty), pages `1–20` | `5`-second debounced cleanup on stream publish; pages `> 20` uncached |
| TVBox filter metadata | Redis `5` minutes (+ up to `60` s jitter) | Cleared when collect index reseals |
| TVBox regular lists | Redis `12` hours (+ up to `15` min jitter, `1` min empty), pages `1–20` | Searches, filters, and pages `> 20` query read model directly; `5`-second debounce cleanup |

## Danmaku layout scaling and sniff cache

- **Font scaling**: `web/src/components/public/VideoPlayer/danmakuLayout.ts` uses `BASE_WIDTH = 960` (`effectiveWidth = min(playerWidth, playerHeight × 16 ÷ 9)`) as the `1x` baseline. Above `960` px it scales via `1 + (ratio - 1) × 0.2` (capped at `1.2x`); below `960` px it scales down to a `0.5x` floor and caps at `playerWidth ÷ 22` when rendered width `< 960` px, clamped overall to `12–120` px. User edits convert back to the `1x` baseline before saving in `localStorage`.
- **Display area**: uses native `[topMargin, bottomMargin]` (default `[10, "50%"]`) from the top of the player container so letterbox bars can hold danmaku and reduce video occlusion. Below `768` px, fixed top/bottom danmaku switch to scrolling; below `560` px, the input box and web fullscreen toggle collapse.
- **Network danmaku cache**: successful sniffs are cached for `7` days keyed by API URL, title, and episode; misses and errors are not cached.

## Two-layer authentication validation

1. `web/src/proxy.ts` performs an edge check for the `ecohub_auth_token` cookie and redirects missing cookies to `/login`.
2. Opening `/manage` runs `web/src/app/manage/layout.tsx`, which requests `/api/manage/user/info`; the backend validates both the JWT signature (`10`-day TTL) and the active session token in Redis (enforcing single-session replacement).
3. `WriteAccess` (`403` for visitors on mutating routes) and `AdminAccess` (super-user restrictions) enforce route permissions server-side.

## Related documents

[Scheduled task conflicts](./cron-task-conflict.md) · [Development guide](./setup.md) · [Server](../../../server/README_EN.md) · [Web](../../../web/README_EN.md)
