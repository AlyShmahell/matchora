# Architecture

Matchora ingests title rows or scans a library path, searches metadata APIs defined in YAML, and ranks candidates with llama-server (installed on demand into `{exeDir}/vendor/llama.cpp`). Matching behavior is in [design/match.md](design/match.md). The admin console is in [design/gui.md](design/gui.md).

## Layout

| Path | Role |
|------|------|
| `matchora/app` | HTTP server (`-config`, `--prepare`) |
| `matchora/lib/config` | YAML loader (`-config` path, default `{exeDir}/config/default.yaml`); `prompt.md` and `ingest.md` are siblings of that file |
| `matchora/share/config` | Seed `default.yaml` + `prompt.md` + `ingest.md` copied into `build/dist/config` |
| `matchora/lib` | fs, ingest, jobs, library (NFO catalog), llama runtime, match, scan |
| `matchora/gui` | admin console source; copied to `build/dist/public` |
| `build/` | Podman dist builder (Containerfile, compose, `run`) |
| `{exeDir}/public` | served admin UI |
| `{exeDir}/data` | `jobs-{session}.json`, optional `secrets` and `config.yaml` overlay; matched titles under `catalog/` as NFO trees |
| `{exeDir}/vendor/llama.cpp` | runtime llama-server + GGUFs (not in dist; included in the `-llama` tarball with fetched licenses) |
| `tests/` | smoke (stub metadata + stub chat cleanup) + live profile |

Dist is binary + `config/` + `public/` only. The packager’s slim tarball is that tree plus `LICENSE`. The `-llama` tarball also includes `{exeDir}/vendor/llama.cpp` (runtime, GGUFs) and licenses fetched at pack time.

## HTTP

| Method | Path | Role |
|--------|------|------|
| GET | `/` | admin console |
| GET | `/health` | `{healthy, version, models}` |
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

## Data and llama

`data_dir` defaults to `{exeDir}/data`. Empty `browse_root` follows `data_dir`. Provider keys live in `{data_dir}/secrets` (file name `secrets`, YAML map). Set them with `POST /v1/secrets` or by editing the file. A provider may set `secret:` to reuse another map key. Missing file or key leaves that provider off. `GET /v1/secrets` reports which slots are set, never the values.

Other runtime YAML lives in `{data_dir}/config.yaml`, merged on `Load` the same way as `/run/matchora/config.yaml`. `GET`/`POST /v1/config` read and deep-merge that overlay (same shape as `default.yaml`). Listen address is `llama.host` / `llama.port` (defaults `127.0.0.1` / `8080`); `llama.base_url` is derived as `http://{host}:{port}/v1`. After a successful secrets or config POST the process writes the JSON body, stops a vendor llama-server if it spawned one, and `exec`s itself so the next `Load` / `llama.Start` applies the files.

Each `POST /v1/scan` or `/v1/ingest` mints a session id (`<UTC datetime>-<16 hex chars>`, e.g. `20260829T122800Z-a1b2c3d4e5f6g7h8`) and writes `{data_dir}/jobs-{session}.json`. `session.ttl_ms` (default and max 86400000) expires that file from the datetime in the id. Reads that need jobs or a filtered catalog take `?session=`. Matched titles are written under `{data_dir}/catalog` as `[uniqueid-id] Title (Year)/` with `.nfo` files and posters. `GET /v1/catalog?session=` returns only titles that session matched. Poster files are at `/v1/catalog/{provider}/{id}/poster.jpg?session=` (and season/episode variants). Deleting a session’s jobs file does not delete the catalog tree; `DELETE /v1/catalog` and `DELETE /v1/catalog/{provider}/{id}` do, and return `409` while any unexpired session still matches the target.

On start the app probes `http://{llama.host}:{llama.port}/v1`. A healthy listener is left alone (external or leftover llama-server). If it is down, matchora downloads `tarball_url` into `{exeDir}/vendor/llama.cpp` when `llama-server` is missing, stages the embed GGUF (and instruct when `LocalInstruct()`) into `vendor/llama.cpp/models`, and spawns one router on `127.0.0.1` at `llama.port`: `--models-dir` (no `--model`), `--embeddings --pooling mean`, `--ctx-size 8192`, `-ngl` from config. Client `base_url` becomes `http://127.0.0.1:{port}/v1`. Then it lists models (`/v1/models` and `/models`, `?reload=1` after a download) and `POST /models/load` if the embed/instruct file is still missing from the list. Instruct follows the effective embed URL when `llm_base_url` is empty or the same origin as the configured `host:port` (default both `:8080`); a distinct instruct URL (smoke stub) is left alone.

`--prepare` runs the same path, then `llama.Stop()` (SIGTERM to the spawned process group) and exits without listening on `:7680`. A spawned vendor server also gets `PR_SET_PDEATHSIG` SIGTERM so it exits if Matchora’s PID disappears. GPU offload uses the host’s Vulkan/Mesa.
