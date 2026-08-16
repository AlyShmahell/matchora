# Research: library scan

`POST /v1/scan` with `{ "path": "..." }` lists video files under a directory inside `browse_root`, returns `202 {"files": N}` immediately, then groups each immediate child with instruct chat into unique titles (`source: scan`).

## Folder conventions

Plex, Jellyfin, and Infuse agree on a small set of trees:

- Keep movies and series in separate roots.
- Movie: `Title (Year)/Title (Year).mkv` (optional `{tmdb-ID}` / `{imdb-tt…}`).
- Series: `Show (Year)/Season 01/Show (Year) - s01e01 - Episode.mkv`.
- Optional IDs in curly braces: `{tmdb-123}`, `{tvdb-123}`, `{imdb-tt123}`.
- Extras live in typed subfolders (`Behind The Scenes`, `Deleted Scenes`, `Trailers`, …).

## Walk

Go lists immediate children of the scan path (video files and directories). For a folder it sends a compact subcontent listing (child dirs with video counts and a few sample names). The instruct model decides whether that child is one show, two shows, a movie, a spin-off, or extras. Prompt lives in `{data_dir}/config/prompt.md`.

If grouping returns nothing or invalid JSON, use the Folder/File name (spaced dash to colon). Skip only when that hint is missing or looks like a filename / SxxExx. `GET /v1/scan/status` reports `{files, done, chunks, chunk, running}` while grouping (`chunks` is the number of immediate children).
