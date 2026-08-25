# Architecture

Matchora ingests title rows or scans a library path, searches metadata APIs defined in YAML, and ranks candidates with llama-server (installed on demand into `{exeDir}/vendor/llama.cpp`). Matching behavior is in [design/match.md](design/match.md). The admin console is in [design/gui.md](design/gui.md).

## Layout

| Path | Role |
|------|------|
| `matchora/app` | HTTP server (`-config`, `--prepare`) |
| `matchora/lib/config` | YAML loader (`-config` path, default `{exeDir}/config/default.yaml`); `prompt.md` and `ingest.md` are siblings of that file |
| `matchora/share/config` | Seed `default.yaml` + `prompt.md` + `ingest.md` copied into `build/dist/config` |
| `matchora/lib` | fs, ingest, jobs, llama runtime, match, scan |
| `matchora/gui` | admin console source; copied to `build/dist/public` |
| `build/` | Podman dist builder (Containerfile, compose, `run`) |
| `{exeDir}/public` | served admin UI |
| `{exeDir}/data` | `jobs.json`, optional `secrets` and `config.yaml` overlay |
| `{exeDir}/vendor/llama.cpp` | runtime llama-server + GGUFs (not in dist) |
| `tests/` | smoke (stub metadata + stub chat cleanup) + live profile |

Dist is binary + `config/` + `public/` only.

## HTTP

| Method | Path | Role |
|--------|------|------|
| GET | `/` | admin console |
| GET | `/health` | `{healthy, version, models}` |
| GET | `/v1/fs` | directory listing under `browse_root` |
| GET | `/v1/jobs` | persisted jobs |
| DELETE | `/v1/jobs` | empty the job list (`[]`); abort an in-flight scan |
| GET | `/v1/match/log` | in-memory provider/ranker wait list |
| POST | `/v1/ingest` | parse rows, store pending, match in background (`202`) |
| POST | `/v1/scan` | list videos, `202 {"files":N}`, group shows in the background, match in the worker |
| GET | `/v1/scan/status` | grouping progress `{files, done, chunks, chunk, running}` |
| POST | `/v1/match` | rematch all jobs in background (`202`) |
| POST | `/v1/retry` | rematch `error` and `unmatched` jobs (`202`) |
| POST | `/v1/jobs/{id}/select` | confirm a candidate on a `manual` row |

## Data and llama

`data_dir` defaults to `{exeDir}/data`. Empty `browse_root` follows `data_dir`. Provider keys live in `{data_dir}/secrets` (file name `secrets`, YAML map). Missing file or key leaves that provider off.

On start the app probes `llama.base_url`. A healthy listener is left alone (external or leftover llama-server). If it is down, matchora downloads `tarball_url` into `{exeDir}/vendor/llama.cpp` when `llama-server` is missing, stages the embed GGUF (and instruct when `LocalInstruct()`) into `vendor/llama.cpp/models`, and spawns one router: `--models-dir` (no `--model`), `--embeddings --pooling mean`, `--ctx-size 8192`, `-ngl` from config. Then it lists models (`/v1/models` and `/models`, `?reload=1` after a download) and `POST /models/load` if the embed/instruct file is still missing from the list. Instruct follows that path only when `llm_base_url` is empty or the same origin as `base_url` (default both `:8080`).

`--prepare` runs the same path, then `llama.Stop()` (SIGTERM to the spawned process group) and exits without listening on `:7680`. GPU offload uses the host’s Vulkan/Mesa.
