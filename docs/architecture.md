# Architecture

Matchora ingests title rows or scans a library path, searches metadata APIs defined in YAML, and ranks candidates with llama-server (downloaded at runtime into the data dir).

## Layout

| Path | Role |
|------|------|
| `matchora/app` | HTTP server |
| `matchora/lib/config` | YAML loader (`-config` path); `prompt.md` and `ingest.md` are siblings of that file |
| `matchora/share/config` | Seed `default.yaml` + `prompt.md` + `ingest.md` copied into the image |
| `matchora/lib` | fs, ingest, jobs, llama runtime, match, scan |
| `matchora/gui` | admin console |
| `data/` | jobs.json, `config/default.yaml`, `config/prompt.md`, `config/ingest.md`, `llamacpp/`, optional `secrets` and `config.yaml` |
| `tests/` | smoke (stub metadata + stub chat cleanup) + live profile |

## HTTP

| Method | Path | Role |
|--------|------|------|
| GET | `/` | admin console |
| GET | `/health` | `{healthy, version}` |
| GET | `/v1/fs` | directory listing |
| GET | `/v1/jobs` | persisted jobs |
| DELETE | `/v1/jobs` | empty the job list (`[]`); abort an in-flight scan |
| POST | `/v1/ingest` | parse rows, store pending, match in background (`202`) |
| POST | `/v1/scan` | list videos, `202 {"files":N}`, group shows in the background, match in the worker |
| GET | `/v1/scan/status` | grouping progress `{files, done, chunks, chunk, running}` |
| POST | `/v1/match` | rematch all jobs in background (`202`) |
| POST | `/v1/retry` | rematch `error` and `unmatched` jobs (`202`) |
| POST | `/v1/jobs/{id}/select` | confirm a candidate on a `manual` row |

## Data and llama

`../data` → `/home/matchora/.oraora/matchora:z` (`data_dir` in YAML).

On start the app downloads the llama.cpp runtime and GGUFs listed in `{data_dir}/config/default.yaml` (seeded from [share/config/default.yaml](../matchora/share/config/default.yaml), passed as `-config`) into `{data_dir}/llamacpp/bin` and `{data_dir}/llamacpp/models` if missing, then starts MiniLM embeddings on `127.0.0.1:8080` and (when `llm_base_url` is local `:8081`) instruct cleanup on `127.0.0.1:8081`. Production compose passes `/dev/dri` for Vulkan. Provider keys live in `{data_dir}/secrets` (file name `secrets`).
