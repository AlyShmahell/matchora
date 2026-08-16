# Research: containers

Checked 2026-08 against official image indexes and llama.cpp / Compose docs.

## Images

| Role | Image |
|------|--------|
| Go build | `docker.io/library/golang:1.26-bookworm` |
| Runtime | `docker.io/library/debian:trixie-slim` |

Debian trixie (Mesa 25+, RADV for gfx1150) + `libgomp1` / `libcurl4` / `libvulkan1` / `mesa-vulkan-drivers`. The llama.cpp tarball and GGUFs are **not** in the image; the app fetches them on first start using URLs in [default.yaml](../../matchora/share/config/default.yaml) into `{data_dir}/llamacpp/bin` and `{data_dir}/llamacpp/models` (soname symlinks kept). Entrypoint seeds `{data_dir}/config/` from `/usr/share/matchora/config` if missing, chowns the data dir, and execs matchora as uid 1000 with compose `-config`.

llama.cpp server exposes OpenAI-compatible `/v1/chat/completions` and `/v1/embeddings`. Embeddings require a pooling mode other than `none`.

Compose has a single `matchora` service and no `environment:` block. Volume target is the literal `data_dir` from YAML: `/home/matchora/.oraora/matchora`. Production compose passes `/dev/dri` and `group_add: keep-groups` (rootless Podman) so Vulkan can see the GPU. Tests do not pass devices.

## Secrets

Provider keys are a YAML map in `{data_dir}/secrets` (file name `secrets`), e.g. `omdb: "..."`. Missing file or key leaves that provider off. Not an env file.

## SELinux

`:z` (shared) relabels the host dir so multiple containers can use it. Matchora uses `:z` on `./data`. The library bind (`/mnt/microsd/media`) is exfat (`dosfs_t`); xattrs/` :z ` do not apply, so production compose sets `security_opt: label=disable` for that read.

## Conventions

Podman-first. `Containerfile` spelling matches Kothar/medora. Toolchains compose is pull-only (`profiles: ["pull"]`, `command: ["true"]`).
