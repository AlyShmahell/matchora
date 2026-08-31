# Workflow

Container toolchains only. Do not run host `go` or browsers against this repo’s automated path. Podman-first (`Containerfile` spelling). Images are pulled on first compose build.

## Dist

[build/Containerfile](../../build/Containerfile) is a one-shot **builder**, not a runtime. Go stage: `docker.io/library/golang:1.26-bookworm`, `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`. Final stage: `debian:trixie-slim` copies the binary, `share/config` → `config/`, and `gui/` → `public/`. Dist is binary + `config/` + `public/` only. Compose bind-mounts `build/dist` (`:z`) and copies out; the container exits.

## Run the app

```bash
./build/run
```

Choose **run**, **(re)build & run**, or **(re)build & package** (arrow keys, Enter). **run** execs the current `build/dist/matchora` (error if missing). The rebuild options refresh `build/dist/` first. Package writes one archive with root `matchora/` and Matchora’s `LICENSE`: `build/package/matchora-<version>-linux-amd64.tar.gz` (binary, `config/`, `public/`).

The admin console is served at `http.addr` from `config/default.yaml` (shipped as port 7680).

The binary defaults to `{exeDir}/config/default.yaml` (copied from [matchora/share/config/default.yaml](../../matchora/share/config/default.yaml)). Writable data: `{exeDir}/data` (`jobs-{session}.json`, optional `config.yaml` overlay, `secrets`).

## Tests

Podman only. The runner image is Debian trixie-slim. Tests do not run the builder image. They bind-mount `build/dist` at `/opt/matchora` (`:z`) and tmpfs on `{exeDir}/data`.

```bash
./tests/run
```

`./tests/run` first builds `build/dist/` via [build/compose.yaml](../../build/compose.yaml), then runs check, unit, and smoke. Smoke hits `/health`, the admin page (including counter chips and secrets), `GET /v1/config`, `GET`/`POST /v1/secrets` (set and clear a dummy OMDb key, waiting until `/health` drops then returns after each restart), `POST /v1/ingest` (stub metadata), `POST /v1/scan` (`202` with `session` and `files`, filesystem grouping into shows), polls `GET /v1/jobs?session=` until rows are matched, then `POST /v1/retry?session=`. Check asserts the dist layout (binary / config / public). Unit tests are `go test ./lib/match ./lib/scan ./lib/config ./lib/jobs ./lib/ingest ./lib/library` in the Go toolchain image.

Live is a skip unless you pass `MATCHORA_LIVE=1` to `./tests/run` (compose `--profile live`, real TVMaze/Jikan).

Equivalent after dist exists:

```bash
cd tests
podman compose -f compose.yaml down
podman compose -f compose.yaml up --build --abort-on-container-exit --exit-code-from tester
podman compose -f compose.yaml down
```
