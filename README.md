# Matchora

Go service that scans a video library, ingests title rows, and matches them against metadata APIs using an embedder or an edge LLM via llama.cpp.

The Podman builder produces a linux/amd64 tree (binary, config, admin UI). On first start, if `llama.base_url` is down, the app installs llama.cpp and the configured GGUFs into `{exeDir}/vendor/llama.cpp` and spawns one llama-server router. Ingest title rows (CSV/JSON) or scan a library path, search YAML-defined metadata APIs (TVMaze/Jikan, optional OMDb), and rank with that server. Dirty scan names are cleaned with a local instruct model before provider search.

## Quick start

```bash
./build/run
```

Pick **run** to start the current `build/dist/` tree. Pick **(re)build & run** to rebuild first. Open http://127.0.0.1:7680 — admin console (not a product UI). GPU offload needs host Vulkan/Mesa.

Pick **(re)build & prepare** to install llama.cpp and GGUFs into `{exeDir}/vendor/llama.cpp`, then exit.

Pick **(re)build & package** to write `build/package/matchora-*-linux-amd64.tar.gz` (archive root `matchora/`).

## Layout

| Path | Purpose |
|------|---------|
| `matchora/` | App, `share/config/`, GUI source |
| `build/` | Containerfile + compose (dist builder) and `run` |
| `build/dist/` | Unpackaged linux/amd64 tree (binary, `config/`, `public/`) |
| `build/package/` | Compressed tarball of that tree |
| `tests/` | Podman smoke + live harness |
| `docs/` | [Architecture](docs/architecture.md), [workflow](docs/dev/workflow.md), [match](docs/design/match.md), [gui](docs/design/gui.md) |

Writable runtime data (`jobs.json`, `secrets`, optional `config.yaml`) is `{exeDir}/data`, created on start.

## Tests

See [docs/dev/workflow.md](docs/dev/workflow.md). **Podman only** — no host Go.
