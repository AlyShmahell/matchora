# Admin GUI

The page under `matchora/gui/` is a verification console, not a product UI.

## Serving

The files live in `matchora/gui/` and the dist builder copies them to `{exeDir}/public`. The app serves that directory at `/`.

## Folder picker

The browser cannot see the host filesystem. The File System Access API and `webkitdirectory` upload host files; they do not pick a server path. The admin picker therefore calls `GET /v1/fs?path=` and stays inside `browse_root` from YAML.

## Upload

A file input posts to `POST /v1/ingest`. The UI shows `queued N titles` and polls `GET /v1/jobs` while any row is `pending`.

## Result cards

`GET /v1/jobs` lists scan/ingest → match rows. Cards update as the background worker finishes each title. Chrome (upload + folder picker) stays in its own card; the jobs pane and a **matching** wait log sit side by side. `GET /v1/match/log` is an in-memory wait list (provider YAML key, `name/catalog`, `name/poster`, `name/detail`, `name/episode`, or ranker name; job title; `since`/`until`); the GUI counts seconds up on open rows. Jobs pane scrolls with **sticky** counter chips. A **skip episode posters** checkbox (persisted in `localStorage`) is sent as `skip_episode_posters` on ingest, scan, retry, select, and catalog so episode images are not fetched. Cards sort **failure** (`error` + `unmatched`) first, **manual** in the middle, then the rest. **retry errors** posts `POST /v1/retry` (error **and** unmatched), shows `queued N titles`, and resumes the pending poll; matched cards are left alone. **clear** confirms, then `DELETE /v1/jobs` (empties the list, aborts grouping). **scan this path** posts `POST /v1/scan` (`grouping N files…` / `grouped x/N`) and polls `GET /v1/scan/status`. A sticky **group x/N** chip and `<progress>` track files grouped. A new scan cancels the previous grouping pass.

Counter chips: **total**, **success** (`matched`), **manual**, **failure** (`error` + `unmatched`), **group** (`done/files` while a scan is grouping). Candidate rows (and the chosen match line) use a 7-band heatmap (`heat-0` … `heat-6`) over score `[0,1]`. On `manual` cards, the candidate title/body `POST /v1/jobs/{id}/select`. Every candidate also has a **seasons** control that `POST /v1/jobs/{id}/catalog` without changing the match. Loaded catalogs render nested seasons and episodes under the job; the candidate whose `catalog_for` matches is marked.
