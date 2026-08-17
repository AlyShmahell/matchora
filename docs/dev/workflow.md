# Workflow

Container toolchains only. Do not run host `go`, `curl`, or browsers against this repo’s automated path.

## Pull images

```bash
podman compose -f toolchains/compose.yaml --profile pull pull
```

## Run the app

```bash
./matchora/run
```

Admin console: http://127.0.0.1:7680

Config is `{data_dir}/config/default.yaml` (seeded from [matchora/share/config/default.yaml](../../matchora/share/config/default.yaml), passed as `-config` in compose). Grouping prompt: `{data_dir}/config/prompt.md`. Ingest column map: `{data_dir}/config/ingest.md`. Optional overlay: `data/config.yaml`. Provider keys: `data/secrets`.

## Tests

Podman only.

```bash
./tests/run
```

Smoke hits `/health`, the admin page (including counter chips), `POST /v1/ingest` (stub metadata + MiniLM), `POST /v1/scan` (`202` with `files`, stub grouping into shows), polls `GET /v1/jobs` until rows are matched, then `POST /v1/retry`. `./tests/run` also runs image check (Mesa) and `go test ./lib/llama ./lib/match ./lib/scan ./lib/config ./lib/jobs ./lib/ingest` in the Go toolchain image. First run downloads the llama.cpp runtime and embed GGUF into the `llama-cpp` volume (URLs in default.yaml).

Live is a skip unless you pass `MATCHORA_LIVE=1` to `./tests/run` (compose `--profile live`, real TVMaze/Jikan, same runtime llama).

Equivalent:

```bash
cd tests
podman compose -f compose.yaml down -v
podman compose -f compose.yaml up --build --abort-on-container-exit --exit-code-from tester
podman compose -f compose.yaml down -v
```
