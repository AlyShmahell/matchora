# Match

Providers are declared in [matchora/share/config/default.yaml](../../matchora/share/config/default.yaml). The engine is a generic GET + JSON-path walker. No provider names are hardcoded in Go. llama.cpp install and spawn are in [architecture](../architecture.md).

Shipped defaults:

| Provider | Auth | Search |
|----------|------|--------|
| [TVMaze](https://www.tvmaze.com/api) | none | `GET /search/shows?q=` — Episode: `/shows/{id}/episodebynumber?season=&number=` |
| [Jikan v4](https://docs.api.jikan.moe/) | none | `GET /anime?q=` — `min_interval_ms: 4000`, `defer: true` |
| OMDb | `{data_dir}/secrets` key `omdb` | `?s=` + `type` — skipped when `api_key` is empty (`require: api_key`) |

Type lists are an allowlist. A typed job only calls providers that list that type. Empty job type still means every provider (scan rows with no type). Defaults: `tv` / `""` → TVMaze (+ OMDb if keyed); `anime` → TVMaze then deferred Jikan; `movie` → OMDb if keyed.

`defer: true` providers run only if the fast pass is terrible: fewer than `match.min_hits` candidates (quantity) or best score below `match.min_score` (quality). `match.min_margin` is only for auto-`matched` vs `manual`. Within a pass, provider GETs for that title run in parallel. Untyped jobs may still fall back to providers that did not list `""`. The engine never switches on provider names.

Pending jobs run up to `match.workers` at a time (default `8`; values below 1 become 1). Each job gets its own `http.timeout_ms` clock; a slow deferred call does not hold or cancel the rest of the batch. Provider `min_interval_ms` still paces that provider across those goroutines.

GET retries, backoff, and **per-attempt** timeout (`provider_timeout_ms`) come from the `http` section. Provider search does not wrap all retries in that timeout. POST (embeddings / chat) is not retried. `POST /v1/retry` rematches `status: error` and `status: unmatched` rows.

A provider that **errors after retries** on 2 jobs in a row (`match.cooldown_fails`, default 2) is skipped for `match.cooldown_ms` (default 1 hour). A later success resets the streak. Empty 200s do not count. A dead parent context (batch leftover or cancel) is not a failure; a per-attempt timeout while the job context is still alive is. The cooldown list lives on the worker for the process lifetime.

After rank, auto-`matched` only if `score >= match.min_score` and the gap to second is `>= match.min_margin` (or a single candidate). Otherwise `status: manual`; the user picks via `POST /v1/jobs/{id}/select`. Zero hits stay `unmatched`.

Stdlib HTTP. User-Agent `matchora/{version}`.

## Ranking

1. **embed** (`llama.base_url`, default `http://127.0.0.1:8080/v1`) — MiniLM, `POST /v1/embeddings`, cosine similarity. Requests send `embed` or the embed file stem as `"model"`.
2. **llm** (`llama.llm_base_url`) — `POST /v1/chat/completions`. Default is the same origin as `base_url` (one local router). Point this URL elsewhere to skip the local instruct GGUF. Chat sends `instruct` or the instruct file stem as `"model"`.

`ranker: embed|llm` in YAML (default embed). If the embed server is down, lexical token overlap + year bonus; record `ranker: lexical`.

Grouped and ingested jobs share `runOne`.

## Ingest

`POST /v1/ingest` accepts `multipart/form-data` field `file` or a raw body. `202` returns created jobs as `pending`; the worker matches them. Missing `title` or an empty payload is `400`. Jobs persist at `{data_dir}/jobs.json`.

- **CSV** with a header. Canonical fields: `title` (required), `year`, `type`, `season`, `episode`, `imdb`.
- **JSON** array of the same fields.

Unknown CSV headers are mapped from `ingest.aliases` (normalized names, e.g. `media_type` → `type`). If `title` is still missing, one instruct call uses `{exeDir}/config/ingest.md` plus a few sample rows and returns `{"columns":{"title":"…"}}` (source header names). Type cells are then rewritten from `ingest.types` (`episode` / `season` → `tv`). Chat failure keeps the alias map; missing `title` is still `400`. Format is detected from `Content-Type`, filename (`.csv` / `.json`), or the first non-space byte (`[` vs a header).

## Scan

`POST /v1/scan` with `{ "path": "..." }` lists video files under a directory inside `browse_root`, returns `202 {"files": N}` immediately, then groups each immediate child with instruct chat into unique titles (`source: scan`) **before** provider search. Prompt lives in `{exeDir}/config/prompt.md`. `GET /v1/scan/status` reports `{files, done, chunks, chunk, running}` (`chunks` is the number of immediate children).

Go lists immediate children of the scan path (video files and directories). For a folder it sends a compact subcontent listing (child dirs with video counts and a few sample names). If grouping returns nothing or invalid JSON, use the Folder/File name (spaced dash to colon). Skip only when that hint is missing or looks like a filename / SxxExx.

Expected trees (Plex / Jellyfin / Infuse):

- Keep movies and series in separate roots.
- Movie: `Title (Year)/Title (Year).mkv` (optional `{tmdb-ID}` / `{imdb-tt…}`).
- Series: `Show (Year)/Season 01/Show (Year) - s01e01 - Episode.mkv`.
- Optional IDs in curly braces: `{tmdb-123}`, `{tvdb-123}`, `{imdb-tt123}`.
- Extras live in typed subfolders (`Behind The Scenes`, `Deleted Scenes`, `Trailers`, …).
