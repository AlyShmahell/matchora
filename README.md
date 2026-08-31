# Matchora

Matchora scans a video library or ingests a list of titles, searches metadata APIs, and writes an NFO catalog. Candidates are ranked with SequenceMatcher. The browser UI is an admin console.

Shipped providers: [TVMaze](https://www.tvmaze.com/api) and [Jikan](https://docs.api.jikan.moe/) (no key). [OMDb](https://www.omdbapi.com/) and [TMDB](https://developer.themoviedb.org/) are optional and need API keys.

## Install

Download `matchora-<version>-linux-amd64.tar.gz` from [GitHub Releases](https://github.com/alyshmahell/matchora/releases). Unpack it. The archive root is `matchora/` (binary, `config/`, `public/`, `LICENSE`).

## Run

From that directory:

```bash
./matchora
```

The process listens on `http.addr` from `config/default.yaml` (shipped as port 7680). Open the admin console on that host and port.

Runtime data lives in `data/` next to the binary: session job files, `secrets`, an optional `config.yaml` overlay, and the NFO catalog. Pass `-config` to load a different `default.yaml`.

## Library and keys

The folder picker stays inside `browse_root`, which defaults to `data/`. Point it at a real library by setting `browse_root` in `data/config.yaml` (same keys as `default.yaml`).

TVMaze and Jikan need no key. Set OMDb and TMDB keys in the admin secrets panel, or in `data/secrets`.

## Use

- **Scan** a path under the browse root. Matchora groups files into titles, then searches providers.
- **Ingest** a CSV or JSON list of titles (optional year, type, season, episode, IMDb id).
- High-confidence hits auto-match. Close scores stay **manual** until you pick a candidate.
- Matched titles are written under `data/catalog` as NFO trees with posters.

## From source

- Developers build and package the same tarball with `./build/run` (rebuild and package). See [docs/dev/workflow.md](docs/dev/workflow.md). 
- Design notes: [architecture](docs/architecture.md), [match](docs/design/match.md), [gui](docs/design/gui.md).
