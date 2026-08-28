package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyshmahell/matchora/lib/config"
	"github.com/alyshmahell/matchora/lib/match"
)

func Save(ctx context.Context, cfg config.Config, job match.Job, cand match.Candidate, skipEpisodePosters bool) error {
	if cand.Provider == "" || cand.ID == "" || strings.TrimSpace(cfg.DataDir) == "" {
		return nil
	}
	root := Root(cfg.DataDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	dir, err := ensureDir(cfg, root, cand)
	if err != nil {
		return err
	}
	spec := cfg.Providers[cand.Provider]
	uid := spec.UniqueID
	if uid == "" {
		uid = cand.Provider
	}
	show := isShow(spec, job, cand)
	if show {
		if err := writeShowNFO(dir, cand, uid, job.Type); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(dir, "movie.nfo"))
	} else {
		if err := writeMovieNFO(dir, cand, uid, job.Type); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(dir, "tvshow.nfo"))
	}
	savePoster(ctx, cfg, cand.Provider, cand.Poster, dir, "poster")
	if !show || job.Catalog == nil || job.CatalogFor != cand.Key() {
		return nil
	}
	for _, s := range job.Catalog {
		sdir := filepath.Join(dir, seasonDirName(s.Number))
		if err := os.MkdirAll(sdir, 0o755); err != nil {
			return err
		}
		if err := writeSeasonNFO(sdir, uid, s); err != nil {
			return err
		}
		savePoster(ctx, cfg, cand.Provider, s.Poster, sdir, "poster")
		seasonNum := s.Number
		for _, e := range s.Episodes {
			base := episodeBase(seasonNum, e.Number, e.Title)
			if err := writeEpisodeNFO(filepath.Join(sdir, base+".nfo"), uid, seasonNum, e); err != nil {
				return err
			}
			if !skipEpisodePosters {
				savePoster(ctx, cfg, cand.Provider, e.Poster, sdir, base)
			}
		}
	}
	return nil
}

func isShow(spec config.Provider, job match.Job, cand match.Candidate) bool {
	switch spec.NFO {
	case "movie":
		return false
	case "tvshow":
		return true
	}
	if job.Catalog != nil && job.CatalogFor == cand.Key() && len(job.Catalog) > 0 {
		return true
	}
	return job.Type != "movie"
}

func ensureDir(cfg config.Config, root string, cand match.Candidate) (string, error) {
	want := DirName(cfg, cand)
	wantPath := filepath.Join(root, want)
	got, err := FindDir(cfg, root, cand.Provider, cand.ID)
	if err != nil {
		if err != os.ErrNotExist {
			return "", err
		}
		if err := os.MkdirAll(wantPath, 0o755); err != nil {
			return "", err
		}
		return wantPath, nil
	}
	if filepath.Base(got) != want {
		if err := os.Rename(got, wantPath); err == nil {
			return wantPath, nil
		}
	}
	return got, nil
}

func savePoster(ctx context.Context, cfg config.Config, provider, url, dir, base string) {
	if strings.TrimSpace(url) == "" {
		return
	}
	if ctx.Err() != nil {
		return
	}
	if posterExists(dir, base) {
		return
	}
	body, ctype, err := match.Fetch(ctx, cfg, url, provider)
	if err != nil || len(body) == 0 {
		return
	}
	ext := extFromType(ctype, url)
	_ = os.WriteFile(filepath.Join(dir, base+ext), body, 0o644)
}

func posterExists(dir, base string) bool {
	for _, ext := range []string{".jpg", ".png", ".webp", ".jpeg", ".gif"} {
		if _, err := os.Stat(filepath.Join(dir, base+ext)); err == nil {
			return true
		}
	}
	return false
}

func extFromType(ctype, rawURL string) string {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(ctype, ";")[0]))
	switch ct {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	}
	u := strings.ToLower(strings.TrimSpace(rawURL))
	switch {
	case strings.Contains(u, ".png"):
		return ".png"
	case strings.Contains(u, ".webp"):
		return ".webp"
	case strings.Contains(u, ".gif"):
		return ".gif"
	}
	return ".jpg"
}

func findPoster(dir, base string) string {
	for _, ext := range []string{".jpg", ".png", ".webp", ".jpeg", ".gif"} {
		name := base + ext
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return name
		}
	}
	return ""
}
