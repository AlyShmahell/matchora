# Research: match

Providers are declared in [matchora/share/config/default.yaml](../../matchora/share/config/default.yaml). The engine is a generic GET + JSON-path walker. No provider names are hardcoded in Go.

Shipped defaults:

| Provider | Auth | Search |
|----------|------|--------|
| [TVMaze](https://www.tvmaze.com/api) | none | `GET /search/shows?q=` — Episode: `/shows/{id}/episodebynumber?season=&number=` |
| [Jikan v4](https://docs.api.jikan.moe/) | none | `GET /anime?q=` — `min_interval_ms: 4000`, `defer: true` |
| OMDb | `{data_dir}/secrets` key `omdb` | `?s=` + `type` — skipped when `api_key` is empty (`require: api_key`) |

Type lists on each provider: `tv` / `""` → TVMaze (+ OMDb if keyed); `anime` → Jikan; `movie` → OMDb if keyed else Jikan.

`defer: true` providers run only if the fast pass is terrible: fewer than `match.min_hits` candidates (quantity) or best score below `match.min_score` (quality). `match.min_margin` is only for auto-`matched` vs `manual`. Within a pass, provider GETs for that title run in parallel. Wanted-then-fallback stays sequential across passes. The engine never switches on provider names.

Pending jobs run up to `match.workers` at a time (default `8`; values below 1 become 1). Each job is written when it finishes; a slow deferred call does not hold the rest of the batch. Provider `min_interval_ms` still paces that provider across those goroutines.

GET retries, backoff, and **per-attempt** timeout (`provider_timeout_ms`) come from the `http` section. Provider search does not wrap all retries in that timeout. POST (embeddings / chat) is not retried. `POST /v1/retry` rematches `status: error` and `status: unmatched` rows.

A provider that **errors after retries** on 2 jobs in a row (`match.cooldown_fails`, default 2) is skipped for `match.cooldown_ms` (default 1 hour). A later success resets the streak. Empty 200s do not count. The cooldown list lives on the worker for the process lifetime.

After rank, auto-`matched` only if `score >= match.min_score` and the gap to second is `>= match.min_margin` (or a single candidate). Otherwise `status: manual`; the user picks via `POST /v1/jobs/{id}/select`. Zero hits stay `unmatched`.

Stdlib HTTP. User-Agent `matchora/{version}`.

## Ranking

The app downloads llama.cpp and GGUFs from [default.yaml](../../matchora/share/config/default.yaml) into `{data_dir}/llamacpp/` if missing.

1. **embed** (`llama.base_url`, default `http://127.0.0.1:8080/v1`) — MiniLM, `POST /v1/embeddings`, cosine similarity.
2. **llm** (`llama.llm_base_url`) — `POST /v1/chat/completions`. Default is a second local instruct server on `:8081` (scan grouping). Point this URL elsewhere to skip the local instruct GGUF.

`ranker: embed|llm` in YAML (default embed). If the embed server is down, lexical token overlap + year bonus; record `ranker: lexical`.

Scan grouping runs instruct chat on each immediate child (folder subcontent or a top-level file) **before** provider search. CSV ingest does not. Grouped jobs already have `title` / `type`. If grouping returns nothing or invalid JSON, use the Folder/File name; skip only when that hint is missing or looks like a filename / SxxExx.

Binaries and models land in `{data_dir}/llamacpp/` (app) or the `llama-cpp` volume (tests). Not committed.
