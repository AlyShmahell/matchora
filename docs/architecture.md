# Architecture

Matchora ingests title rows or scans a library path, searches metadata APIs defined in YAML, and ranks candidates with token-set Jaccard plus a residual plot score (SequenceMatcher is for disk grouping only). Matching behavior is in [design/match.md](design/match.md). The admin console is in [design/gui.md](design/gui.md).

## Layout

| Path | Role |
|------|------|
| `matchora/app` | HTTP server (`-config`) |
| `matchora/lib/config` | YAML loader (`-config` path, default `{exeDir}/config/default.yaml`) |
| `matchora/share/config` | Seed `default.yaml` copied into `build/dist/config`. Grouping word lists and match/http numbers come only from YAML; a missing required key fails start. |
| `matchora/lib` | fs, ingest, jobs, library (NFO catalog), match, scan |
| `matchora/gui` | admin console source; copied to `build/dist/public` |
| `build/` | Podman dist builder (Containerfile, compose, `run`) |
| `{exeDir}/public` | served admin UI |
| `{exeDir}/data` | `jobs-{session}.json`, optional `secrets` and `config.yaml` overlay; matched titles under `catalog/` as NFO trees |
| `tests/` | smoke (stub metadata) + live profile |

Dist is binary + `config/` + `public/` only. The packager’s tarball is that tree plus `LICENSE`.

## HTTP

| Method | Path | Role |
|--------|------|------|
| GET | `/` | admin console |
| GET | `/health` | `{healthy, version}` |
| GET | `/v1/fs` | directory listing under `browse_root` |
| GET | `/v1/sessions` | unexpired session ids, newest first |
| GET | `/v1/jobs` | jobs for `?session=` (`400` / `404` without a live session) |
| DELETE | `/v1/jobs` | delete that session’s jobs file; abort grouping if it is the active scan |
| GET | `/v1/match/log` | in-memory waits for jobs in `?session=` |
| POST | `/v1/ingest` | parse rows, mint a session, `202 {"session","jobs"}` |
| POST | `/v1/scan` | list videos, mint a session, `202 {"session","files"}`, group then match |
| GET | `/v1/scan/status` | grouping progress for `?session=` |
| POST | `/v1/match` | rematch all jobs in that session (`202`) |
| POST | `/v1/retry` | rematch `error` and `unmatched` jobs in that session (`202`) |
| POST | `/v1/jobs/{id}/select` | confirm a candidate on a `manual` row (`?session=`) |
| POST | `/v1/jobs/{id}/catalog` | fetch seasons/episodes for a candidate (`?session=`) |
| GET | `/v1/catalog` | titles in `{data_dir}/catalog` that this session matched |
| GET | `/v1/catalog/{provider}/{id}` | one title if this session matched it; 404 otherwise |
| DELETE | `/v1/catalog` | wipe the catalog tree; `409` if any unexpired session still has a match |
| DELETE | `/v1/catalog/{provider}/{id}` | remove one title; `409` if any session still matches it |
| GET | `/v1/secrets` | which secret slots are set (booleans only, never values) |
| POST | `/v1/secrets` | merge JSON map into `{data_dir}/secrets`, then re-exec |
| GET | `/v1/config` | `{data_dir}/config.yaml` overlay as JSON (`{}` if none) |
| POST | `/v1/config` | merge JSON into that overlay, then re-exec |

## Data

`data_dir` defaults to `{exeDir}/data`. Empty `browse_root` follows `data_dir`. Provider keys live in `{data_dir}/secrets` (file name `secrets`, YAML map). Set them with `POST /v1/secrets` or by editing the file. A provider may set `secret:` to reuse another map key. Missing file or key leaves that provider off. `GET /v1/secrets` reports which slots are set, never the values.

Other runtime YAML lives in `{data_dir}/config.yaml`, merged on `Load` the same way as `/run/matchora/config.yaml`. `GET`/`POST /v1/config` read and deep-merge that overlay (same shape as `default.yaml`). After a successful secrets or config POST the process writes the JSON body and `exec`s itself so the next `Load` applies the files.

Each `POST /v1/scan` or `/v1/ingest` mints a session id (`<UTC datetime>-<16 hex chars>`, e.g. `20260829T122800Z-a1b2c3d4e5f6g7h8`) and writes `{data_dir}/jobs-{session}.json`. `session.ttl_ms` (clamped to `session.ttl_max_ms`, shipped 86400000) expires that file from the datetime in the id. Reads that need jobs or a filtered catalog take `?session=`. Matched titles are written under `{data_dir}/catalog` as `[uniqueid-id] Title (Year)/` with `.nfo` files and posters. `GET /v1/catalog?session=` returns only titles that session matched. Poster files are at `/v1/catalog/{provider}/{id}/poster.jpg?session=` (and season/episode variants). Deleting a session’s jobs file does not delete the catalog tree; `DELETE /v1/catalog` and `DELETE /v1/catalog/{provider}/{id}` do, and return `409` while any unexpired session still matches the target.
