# Matchora

Containerized Go service that will scan a video library, ingest title rows, and match them against metadata APIs using an embedder or an edge LLM via llama.cpp.

Ingest title rows (CSV/JSON) or scan a library path, search YAML-defined metadata APIs (TVMaze/Jikan, optional OMDb), and rank with llama-server downloaded into the data dir. Dirty scan names are cleaned with a local instruct model before provider search.

## Quick start

```bash
podman compose -f toolchains/compose.yaml --profile pull pull
./matchora/run
```

Open http://127.0.0.1:7680 — admin console (not a product UI).

## Layout

| Path | Purpose |
|------|---------|
| `matchora/` | App, `share/config/`, GUI, Containerfile, compose, run |
| `data/` | Host persistence → `/home/matchora/.oraora/matchora` (`:z`); llama runtime + GGUFs under `llamacpp/` |
| `tests/` | Podman smoke + live harness |
| `toolchains/` | Pull-only images |
| `docs/` | Architecture, workflow, research |

## Tests

See [docs/dev/workflow.md](docs/dev/workflow.md). **Podman only** — no host Go.
