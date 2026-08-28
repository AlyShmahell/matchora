# Workflow

Container toolchains only. Do not run host `go` or browsers against this repo’s automated path. Podman-first (`Containerfile` spelling). Images are pulled on first compose build. The packager uses host `curl` to fetch third-party licenses into the `-llama` tarball.

## Dist

[build/Containerfile](../../build/Containerfile) is a one-shot **builder**, not a runtime. Go stage: `docker.io/library/golang:1.26-bookworm`, `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`. Final stage: `debian:trixie-slim` copies the binary, `share/config` → `config/`, and `gui/` → `public/`. Dist is binary + `config/` + `public/` only; llama.cpp and GGUFs are not in the tree. Compose bind-mounts `build/dist` (`:z`) and copies out; the container exits.

## Run the app

```bash
./build/run
```

Choose **run**, **(re)build & run**, **(re)build & prepare**, or **(re)build & package** (arrow keys, Enter). **run** execs the current `build/dist/matchora` (error if missing). The rebuild options refresh `build/dist/` first. Prepare runs `matchora --prepare`. Package writes two archives with root `matchora/` and Matchora’s `LICENSE`: `build/package/matchora-<version>-linux-amd64.tar.gz` (binary, `config/`, `public/`) and `matchora-<version>-linux-amd64-llama.tar.gz` (same plus `vendor/llama.cpp` and fetched llama.cpp / GGUF licenses). The `-llama` tarball runs `--prepare` then `curl`s those licenses from GitHub and Hugging Face.

Admin console: http://127.0.0.1:7680

The binary defaults to `{exeDir}/config/default.yaml` (copied from [matchora/share/config/default.yaml](../../matchora/share/config/default.yaml)). Grouping prompt: `{exeDir}/config/prompt.md`. Ingest column map: `{exeDir}/config/ingest.md`. Writable data: `{exeDir}/data` (`jobs.json`, optional `config.yaml` overlay, `secrets`). llama.cpp and GGUFs install on demand into `{exeDir}/vendor/llama.cpp`. `matchora --prepare` runs that install, verifies health/models, stops the spawned llama-server, and exits. Host Vulkan/Mesa is required for GPU offload.

## Tests

Podman only. The runner image is Debian trixie-slim + `libgomp1` / `libcurl4` / `libvulkan1` / `mesa-vulkan-drivers`. Tests do not run the builder image. They bind-mount `build/dist` at `/opt/matchora` (`:z`), tmpfs on `{exeDir}/data`, and named volume `matchora-vendor` on `/opt/matchora/vendor` so dist rebuilds do not re-fetch llama.cpp. Matchora `start_period` is 300s. No `/dev/dri`.

```bash
./tests/run
```

`./tests/run` first builds `build/dist/` via [build/compose.yaml](../../build/compose.yaml), then runs check, unit, and smoke. Smoke hits `/health`, the admin page (including counter chips), `POST /v1/ingest` (stub metadata + MiniLM), `POST /v1/scan` (`202` with `files`, stub grouping into shows), polls `GET /v1/jobs` until rows are matched, then `POST /v1/retry`. First start downloads the llama.cpp runtime and MiniLM (instruct is skipped because smoke overlays `llm_base_url` to the stub). Check asserts the dist layout (binary / config / public) and Mesa 24+ RADV in the runner. Unit tests are `go test ./lib/llama ./lib/match ./lib/scan ./lib/config ./lib/jobs ./lib/ingest` in the Go toolchain image.

Live is a skip unless you pass `MATCHORA_LIVE=1` to `./tests/run` (compose `--profile live`, real TVMaze/Jikan, same on-demand llama under the `matchora-vendor` volume).

Equivalent after dist exists:

```bash
cd tests
podman compose -f compose.yaml down
podman compose -f compose.yaml up --build --abort-on-container-exit --exit-code-from tester
podman compose -f compose.yaml down
```
