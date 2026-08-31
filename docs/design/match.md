# Match

Providers are declared in [matchora/share/config/default.yaml](../../matchora/share/config/default.yaml). The engine is a generic GET + JSON-path walker. No provider names are hardcoded in Go.

Shipped defaults:

| Provider | Auth | Search |
|----------|------|--------|
| [TVMaze](https://www.tvmaze.com/api) | none | `GET /search/shows?q=` — Episode: `/shows/{id}/episodebynumber?season=&number=` — Catalog: `/shows/{id}/seasons` plus `/shows/{id}/episodes` grouped by season |
| [Jikan v4](https://docs.api.jikan.moe/) | none | `GET /anime?q=` — `limit: 25`, `min_interval_ms: 1100`, `retries: 1`, `provider_timeout_ms: 4000`, `defer: true` |
| OMDb | `{data_dir}/secrets` key `omdb` (or `POST /v1/secrets`) | `?s=` + `type=movie` — movies only; skipped when `api_key` is empty (`require: api_key`) |
| [TMDB](https://developer.themoviedb.org/docs/search-and-query-for-details) movie | `{data_dir}/secrets` key `tmdb` (or `POST /v1/secrets`) | `GET /3/search/movie` — `defer: true`; skipped when `api_key` is empty |
| TMDB TV | `secret: tmdb` | `GET /3/search/tv` — `defer: true`; skipped when `api_key` is empty — Catalog: show `seasons`, then `/tv/{id}/season/{season}` |

Type lists are an allowlist. A typed job only calls providers that list that type. Empty job type still means every provider (scan rows with no type). Defaults: `tv` / `""` → TVMaze then deferred TMDB TV; `anime` → TVMaze then deferred TMDB movie / TMDB TV / Jikan; `movie` → OMDb then deferred TMDB movie (if keyed).

`defer: true` providers run only if the fast pass is terrible: fewer than `match.min_hits` candidates (quantity) or best score below `match.min_score` (quality). `match.min_margin` is only for auto-`matched` vs `manual`. Within a pass, provider GETs for that title run in parallel. Untyped jobs may still fall back to providers that did not list `""`. The engine never switches on provider names.

Pending jobs run up to `match.workers` at a time (default `8`; values below 1 become 1). Each job gets its own `http.timeout_ms` clock; a slow deferred call does not hold or cancel the rest of the batch. Provider `min_interval_ms` still paces that provider across those goroutines.

GET retries, per-attempt timeout (`provider_timeout_ms`), and **capped exponential backoff** come from the `http` section (`http.backoff.min_exp` / `max_exp`, default 10/13). A provider may set `retries` and/or `provider_timeout_ms` to override those values for its own GETs (search, episode, catalog, detail, poster). Zero or omitted keeps the global `http` values. After a failed GET the wait is uniform random in `[2^(exp-1), 2^exp]` ms, starting at `min_exp+1` and capped at `max_exp`. `Retry-After` still wins when present, capped at `2^max_exp` ms. Provider search does not wrap all retries in that timeout. `POST /v1/retry` rematches `status: error` and `status: unmatched` rows.

A provider that **errors after retries** on `match.cooldown_fails` jobs in a row (default 2 if unset; shipped YAML is 5) is skipped for a jittered cooldown from `match.cooldown.min_exp` / `max_exp` (default 16/19: first skip ~1–2 min, cap ~4.4–8.7 min). Each cooldown start bumps the exponent; a later success resets streak and exponent. Empty 200s do not count. A dead parent context (batch leftover or cancel) is not a failure; a per-attempt timeout while the job context is still alive is. The cooldown list lives on the worker for the process lifetime.

After rank, auto-`matched` only if `score >= match.min_score` and the gap to second is `>= match.min_margin` (or a single candidate). Otherwise `status: manual`; the user picks via `POST /v1/jobs/{id}/select`. Zero hits stay `unmatched`.

An optional YAML `catalog` block on a provider lists seasons and episodes with the same GET + JSON-path walker (`url`, `query`, `items`, `fields`, `year`, `poster_prefix`). Vars include `{id}`, `{season}`, `{season_id}`. If `episodes.url` contains `{season}` or `{season_id}`, the engine GETs once per season; otherwise one episodes dump is grouped by `fields.season`. Auto-match and `select` fetch the catalog for the chosen title. `POST /v1/jobs/{id}/catalog` `{provider,id}` loads it for any candidate on the job without changing `match` or `status`. `catalog: null` means not loaded; `[]` means loaded nothing. After pending work, the worker backfills `matched` jobs whose match provider has a catalog block and `catalog` is still null. `POST /v1/match` and retry clear catalog fields. Movies and providers without the block skip.

Matched, selected, and cataloged titles are also written under `{data_dir}/catalog` as `[uniqueid-id] Title (Year)/` with `tvshow.nfo` or `movie.nfo`, season folders, episode `.nfo` files, and downloaded posters. Optional `uniqueid` is both the folder prefix and `<uniqueid type>` (shipped: TMDB movie `tmdb-movie`, TMDB TV `tmdb-tv`, OMDb `imdb`). If omitted, the YAML provider key is used unchanged. Other optional keys: `nfo: movie|tvshow` (which root file to write), `detail` (same shape as `episode:` — GET after match/select and merge empty fields, used for OMDb plot). Job `type` is stored as `<type>` on the root NFO. `GET /v1/catalog?session=` and `GET /v1/catalog/{provider}/{id}?session=` read that tree filtered to titles the session matched (`{provider}` may be the YAML key or the `uniqueid` slug). `DELETE /v1/catalog` and `DELETE /v1/catalog/{provider}/{id}` remove on-disk titles and do not take a session; they return `409` if any unexpired session still has a matching `match`.

Requests that persist that tree (`POST /v1/ingest`, `/v1/scan`, `/v1/match`, `/v1/retry`, `/v1/jobs/{id}/select`, `/v1/jobs/{id}/catalog`) accept `skip_episode_posters` (query `true`/`1`, JSON body, or ingest form field). When true, episode image GETs are skipped; title/movie and season posters still download. Episode `.nfo` files are still written.

Stdlib HTTP. User-Agent `matchora/{version}`.

## Ranking

SequenceMatcher ratio of the job title against each candidate title (plus year). Exact normalized titles score `1.0`. Synopsis is not used. Matching year adds `0.15`. Jobs record `ranker: seq`.

Grouped and ingested jobs share `runOne`.

## Ingest

`POST /v1/ingest` accepts `multipart/form-data` field `file` or a raw body. `202` returns `{"session","jobs"}` with created rows as `pending`; the worker matches them. Missing `title` or an empty payload is `400`. Jobs persist at `{data_dir}/jobs-{session}.json` until `session.ttl_ms` (max one day) or `DELETE /v1/jobs?session=`.

- **CSV** with a header. Canonical fields: `title` (required), `year`, `type`, `season`, `episode`, `imdb`.
- **JSON** array of the same fields.

Unknown CSV headers are mapped from `ingest.aliases` (normalized names, e.g. `media_type` → `type`). If `title` is still missing, unused headers are matched to canonical fields (and alias keys) with SequenceMatcher when the ratio is at least `group.seq_threshold`. Type cells are then rewritten from `ingest.types` (`episode` / `season` → `tv`). Missing `title` is still `400`. Format is detected from `Content-Type`, filename (`.csv` / `.json`), or the first non-space byte (`[` vs a header).

## Scan

`POST /v1/scan` with `{ "path": "..." }` lists video files under a directory inside `browse_root`, returns `202 {"session","files"}` immediately, then groups each immediate child on disk into unique titles (`source: scan`) **before** provider search. The grouper is structure-first (seasons, extras, named siblings, release dumps) with SequenceMatcher clustering for leftover loose videos. Word lists and `seq_threshold` live under `group:` in YAML; match/http numbers live under those YAML sections. Missing required keys fail start. `GET /v1/scan/status?session=` reports `{files, done, chunks, chunk, running}` (`chunks` is the number of immediate children). A later scan or ingest mints a new session; poll `GET /v1/jobs?session=` for that id only.

Go lists immediate children of the scan path (video files and directories). Each child is walked on disk and emits `{title, year, path}` rows relative to `browse_root`. Season-only trees keep the folder title. Named sibling folders become their own titles. Year is taken only when a `(YYYY)` suffix or trailing year token appears in the tree. Each child is appended and matching is kicked before the next folder is grouped. Skip a child only when no usable title remains.

Expected trees (Plex / Jellyfin / Infuse):

- Keep movies and series in separate roots.
- Movie: `Title (Year)/Title (Year).mkv` (optional `{tmdb-ID}` / `{imdb-tt…}`).
- Series: `Show (Year)/Season 01/Show (Year) - s01e01 - Episode.mkv`.
- Optional IDs in curly braces: `{tmdb-123}`, `{tvdb-123}`, `{imdb-tt123}`.
- Extras live in typed subfolders (`Behind The Scenes`, `Deleted Scenes`, `Trailers`, …).
