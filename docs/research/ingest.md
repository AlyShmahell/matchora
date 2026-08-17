# Research: ingest

Admin upload of title rows. Parsed rows become jobs and are auto-matched. Matching is the same `runOne` path as scan.

## Formats

- **CSV** with a header. Canonical fields: `title` (required), `year`, `type`, `season`, `episode`, `imdb`.
- **JSON** array of the same fields.

Unknown CSV headers are mapped in Go from `{data_dir}/config/default.yaml` `ingest.aliases` (normalized names, e.g. `media_type` → `type`). If `title` is still missing, one instruct call uses `{data_dir}/config/ingest.md` plus a few sample rows and returns `{"columns":{"title":"…"}}` (source header names). Type cells are then rewritten from `ingest.types` (`episode` / `season` → `tv`). Chat failure keeps the alias map; missing `title` is still `400`.

Detect format from `Content-Type`, filename (`.csv` / `.json`), or the first non-space byte (`[` vs a header).

## Wire

`POST /v1/ingest` accepts `multipart/form-data` field `file` or a raw body. `202` returns the created jobs as `pending`; a background worker matches them one-by-one. Missing `title` or an empty payload is `400`.

Jobs persist at `{data_dir}/jobs.json` (`/home/matchora/.oraora/matchora/jobs.json` by default).
