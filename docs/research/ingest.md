# Research: ingest

Admin upload of title rows. Parsed rows become jobs and are auto-matched.

## Formats

- **CSV** with a header. Columns (case-insensitive): `title` (required), `year`, `type` (`movie`|`tv`|`anime`), `season`, `episode`, `imdb`.
- **JSON** array of the same fields.

Detect format from `Content-Type`, filename (`.csv` / `.json`), or the first non-space byte (`[` vs a header).

## Wire

`POST /v1/ingest` accepts `multipart/form-data` field `file` or a raw body. `202` returns the created jobs as `pending`; a background worker matches them one-by-one. Missing `title` or an empty payload is `400`.

Jobs persist at `{data_dir}/jobs.json` (`/home/matchora/.oraora/matchora/jobs.json` by default).
