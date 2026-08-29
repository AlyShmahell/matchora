# Admin GUI

The page under `matchora/gui/` is a verification console, not a product UI.

## Serving

The files live in `matchora/gui/` and the dist builder copies them to `{exeDir}/public`. The app serves that directory at `/`.

## Folder picker

The browser cannot see the host filesystem. The File System Access API and `webkitdirectory` upload host files; they do not pick a server path. The admin picker therefore calls `GET /v1/fs?path=` and stays inside `browse_root` from YAML.

## Secrets

A chrome panel lists secret slots from `GET /v1/secrets` (API keys only). Password inputs; **save** posts non-empty fields to `POST /v1/secrets`. **clear** posts `""` for that key. A successful POST restarts the process; the UI polls `/health` until it returns.

## Llama

Host and port fields load from `GET /v1/config` overlay (`llama.host` / `llama.port`, default `127.0.0.1` / `8080`). **save** posts `{"llama":{"host","port"}}` to `POST /v1/config` and waits for the restart.

## Upload

A file input posts to `POST /v1/ingest`. The UI stores `session` from the `202` body, shows `queued N titles` from `jobs`, and polls `GET /v1/jobs?session=` while any row is `pending`.

## Result cards

`GET /v1/jobs?session=` lists that run’s scan/ingest → match rows. The console keeps the last session in `localStorage` and reopens the newest id from `GET /v1/sessions` after refresh. Cards update as the background worker finishes each title. Chrome (upload + folder picker + secrets + llama) stays in its own card; the jobs pane and a **matching** wait log sit side by side. `GET /v1/match/log?session=` is an in-memory wait list (provider YAML key, `name/catalog`, `name/poster`, `name/detail`, `name/episode`, or ranker name; job title; `since`/`until`); the GUI counts seconds up on open rows. Jobs pane scrolls with **sticky** counter chips. A **skip episode posters** checkbox (persisted in `localStorage`) is sent as `skip_episode_posters` on ingest, scan, retry, select, and catalog so episode images are not fetched. Cards sort **failure** (`error` + `unmatched`) first, **manual** in the middle, then the rest. **retry errors** posts `POST /v1/retry?session=` (error **and** unmatched), shows `queued N titles`, and resumes the pending poll; matched cards are left alone. **clear** confirms, then `DELETE /v1/jobs?session=` (drops that jobs file, aborts grouping if it is the active scan). **scan this path** posts `POST /v1/scan` (`grouping N files…` / `grouped x/N`), stores the new `session`, and polls `GET /v1/scan/status?session=`. A sticky **group x/N** chip and `<progress>` track files grouped. A new scan cancels the previous grouping pass; the old session’s jobs file stays until TTL.

Counter chips: **total**, **success** (`matched`), **manual**, **failure** (`error` + `unmatched`), **group** (`done/files` while a scan is grouping). Candidate rows (and the chosen match line) use a 7-band heatmap (`heat-0` … `heat-6`) over score `[0,1]`. On `manual` cards, the candidate title/body `POST /v1/jobs/{id}/select`. Every candidate also has a **seasons** control that `POST /v1/jobs/{id}/catalog` without changing the match. Loaded catalogs render nested seasons and episodes under the job; the candidate whose `catalog_for` matches is marked.
