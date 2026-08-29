package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alyshmahell/matchora/lib/config"
	matchfs "github.com/alyshmahell/matchora/lib/fs"
	"github.com/alyshmahell/matchora/lib/ingest"
	"github.com/alyshmahell/matchora/lib/jobs"
	"github.com/alyshmahell/matchora/lib/library"
	"github.com/alyshmahell/matchora/lib/llama"
	"github.com/alyshmahell/matchora/lib/match"
	"github.com/alyshmahell/matchora/lib/scan"
)

func main() {
	exeDir, err := config.ExeDir()
	if err != nil {
		log.Fatal(err)
	}
	configPath := flag.String("config", "", "path to default.yaml")
	prepare := flag.Bool("prepare", false, "install llama.cpp runtime and models, then exit")
	flag.Parse()
	path := strings.TrimSpace(*configPath)
	if path == "" {
		path = filepath.Join(exeDir, "config", "default.yaml")
	}
	cfg, err := config.Load(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(cfg.BrowseRoot, 0o755); err != nil {
		log.Fatal(err)
	}
	if err := llama.Start(&cfg); err != nil {
		llama.Stop()
		log.Fatal(err)
	}
	if *prepare {
		llama.Stop()
		return
	}
	store := jobs.New(cfg.DataDir)
	worker := jobs.NewWorker(&cfg, store)
	scans := newScanRun()
	worker.Kick()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"healthy": true,
			"version": cfg.Version,
			"models":  llama.Stats(cfg),
		})
	})
	mux.HandleFunc("GET /v1/fs", func(w http.ResponseWriter, r *http.Request) {
		listing, err := matchfs.List(cfg.BrowseRoot, r.URL.Query().Get("path"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, listing)
	})
	mux.HandleFunc("GET /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		for _, gone := range store.PurgeExpired(cfg.SessionTTL()) {
			scans.abortIf(gone)
		}
		ids, err := store.Sessions(cfg.SessionTTL())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if ids == nil {
			ids = []string{}
		}
		writeJSON(w, http.StatusOK, ids)
	})
	mux.HandleFunc("GET /v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		sess, ok := requireSession(w, r, store, cfg, scans)
		if !ok {
			return
		}
		list, err := store.List(sess)
		if err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": storeError(err)})
			return
		}
		writeJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("GET /v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		sess, ok := requireSession(w, r, store, cfg, scans)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, scans.snapshot(sess))
	})
	mux.HandleFunc("GET /v1/match/log", func(w http.ResponseWriter, r *http.Request) {
		sess, ok := requireSession(w, r, store, cfg, scans)
		if !ok {
			return
		}
		list, err := store.List(sess)
		if err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": storeError(err)})
			return
		}
		writeJSON(w, http.StatusOK, filterWaits(worker.Waits(), list))
	})
	mux.HandleFunc("GET /v1/secrets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, config.SecretsStatus(cfg))
	})
	mux.HandleFunc("POST /v1/secrets", func(w http.ResponseWriter, r *http.Request) {
		var updates map[string]string
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil || updates == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json object of secret keys required"})
			return
		}
		if err := config.SetSecrets(&cfg, updates); err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "unknown secret key") {
				status = http.StatusBadRequest
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, config.SecretsStatus(cfg))
		restartSoon()
	})
	mux.HandleFunc("GET /v1/config", func(w http.ResponseWriter, r *http.Request) {
		over, err := config.ReadOverlay(cfg.DataDir)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, over)
	})
	mux.HandleFunc("POST /v1/config", func(w http.ResponseWriter, r *http.Request) {
		var patch map[string]any
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil || patch == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "json object required"})
			return
		}
		over, err := config.Overlay(&cfg, patch)
		if err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "llama.port") || strings.Contains(err.Error(), "json object") {
				status = http.StatusBadRequest
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, over)
		restartSoon()
	})
	mux.HandleFunc("POST /v1/scan", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path               string `json:"path"`
			SkipEpisodePosters bool   `json:"skip_episode_posters"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path required"})
			return
		}
		skip := skipEpisodePosters(r) || body.SkipEpisodePosters
		worker.SetSkipEpisodePosters(skip)
		target := body.Path
		if target == "" {
			target = cfg.BrowseRoot
		}
		target = filepath.Clean(target)
		children, err := scan.Children(cfg.BrowseRoot, target)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		files := 0
		for _, c := range children {
			files += c.Videos
		}
		if len(children) == 0 || files == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no titles in path"})
			return
		}
		sess := jobs.NewSessionID(time.Now())
		if err := store.Create(sess); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		ctx := scans.start(sess, files, len(children))
		writeJSON(w, http.StatusAccepted, map[string]any{"session": sess, "files": files})
		go enqueueScan(ctx, cfg, store, worker, scans, sess, children)
	})
	mux.HandleFunc("POST /v1/ingest", func(w http.ResponseWriter, r *http.Request) {
		name, ct, body, err := ingestBody(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		defer body.Close()
		rows, err := ingest.Parse(r.Context(), cfg, body, name, ct)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		created := jobs.FromRows(rows, "ingest")
		sess := jobs.NewSessionID(time.Now())
		if err := store.Create(sess); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if _, err := store.Append(sess, created); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		worker.SetSkipEpisodePosters(skipEpisodePosters(r))
		worker.Kick()
		writeJSON(w, http.StatusAccepted, map[string]any{"session": sess, "jobs": created})
	})
	mux.HandleFunc("POST /v1/match", func(w http.ResponseWriter, r *http.Request) {
		sess, ok := requireSession(w, r, store, cfg, scans)
		if !ok {
			return
		}
		list, err := store.MarkPending(sess)
		if err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": storeError(err)})
			return
		}
		worker.SetSkipEpisodePosters(skipEpisodePosters(r))
		worker.Kick()
		writeJSON(w, http.StatusAccepted, list)
	})
	mux.HandleFunc("DELETE /v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		sess, ok := requireSession(w, r, store, cfg, scans)
		if !ok {
			return
		}
		scans.abortIf(sess)
		if err := store.Clear(sess); err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": storeError(err)})
			return
		}
		writeJSON(w, http.StatusOK, []match.Job{})
	})
	mux.HandleFunc("POST /v1/retry", func(w http.ResponseWriter, r *http.Request) {
		sess, ok := requireSession(w, r, store, cfg, scans)
		if !ok {
			return
		}
		list, err := store.MarkErrorsPending(sess)
		if err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": storeError(err)})
			return
		}
		worker.SetSkipEpisodePosters(skipEpisodePosters(r))
		worker.Kick()
		writeJSON(w, http.StatusAccepted, list)
	})
	mux.HandleFunc("POST /v1/jobs/{id}/select", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Provider           string `json:"provider"`
			ID                 string `json:"id"`
			SkipEpisodePosters bool   `json:"skip_episode_posters"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Provider == "" || body.ID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider and id required"})
			return
		}
		skip := skipEpisodePosters(r) || body.SkipEpisodePosters
		worker.SetSkipEpisodePosters(skip)
		sess, ok := requireSession(w, r, store, cfg, scans)
		if !ok {
			return
		}
		job, err := store.Get(sess, r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), cfg.HTTPTimeout())
		ctx = match.WithReporter(ctx, worker.Reporter())
		ctx = match.WithJob(ctx, job)
		done, err := match.ApplySelect(ctx, cfg, job, body.Provider, body.ID)
		if err != nil {
			cancel()
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := store.Update(sess, map[string]match.Job{done.ID: done}); err != nil {
			cancel()
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if done.Match != nil {
			if err := library.Save(ctx, cfg, done, *done.Match, skip); err != nil {
				log.Printf("library: save: %v", err)
			}
		}
		cancel()
		writeJSON(w, http.StatusOK, done)
	})
	mux.HandleFunc("POST /v1/jobs/{id}/catalog", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Provider           string `json:"provider"`
			ID                 string `json:"id"`
			SkipEpisodePosters bool   `json:"skip_episode_posters"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Provider == "" || body.ID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider and id required"})
			return
		}
		skip := skipEpisodePosters(r) || body.SkipEpisodePosters
		worker.SetSkipEpisodePosters(skip)
		sess, ok := requireSession(w, r, store, cfg, scans)
		if !ok {
			return
		}
		job, err := store.Get(sess, r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), cfg.HTTPTimeout())
		ctx = match.WithReporter(ctx, worker.Reporter())
		ctx = match.WithJob(ctx, job)
		done, err := match.ApplyCatalog(ctx, cfg, job, body.Provider, body.ID)
		if err != nil {
			cancel()
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := store.Update(sess, map[string]match.Job{done.ID: done}); err != nil {
			cancel()
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if cand, ok := match.LookupCandidate(done, body.Provider, body.ID); ok {
			if err := library.Save(ctx, cfg, done, cand, skip); err != nil {
				log.Printf("library: save: %v", err)
			}
		}
		cancel()
		writeJSON(w, http.StatusOK, done)
	})
	mux.HandleFunc("GET /v1/catalog", func(w http.ResponseWriter, r *http.Request) {
		sess, ok := requireSession(w, r, store, cfg, scans)
		if !ok {
			return
		}
		jobsList, err := store.List(sess)
		if err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": storeError(err)})
			return
		}
		list, err := library.List(cfg.DataDir)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, filterCatalog(cfg, list, jobsList))
	})
	mux.HandleFunc("DELETE /v1/catalog", func(w http.ResponseWriter, r *http.Request) {
		store.PurgeExpired(cfg.SessionTTL())
		pins, err := store.PinningAny(cfg.SessionTTL())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if len(pins) > 0 {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "pinned by " + strings.Join(pins, ", ")})
			return
		}
		if err := library.RemoveAll(cfg.DataDir); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, []library.Title{})
	})
	mux.HandleFunc("GET /v1/catalog/{provider}/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess, ok := requireSession(w, r, store, cfg, scans)
		if !ok {
			return
		}
		provider, id := r.PathValue("provider"), r.PathValue("id")
		jobsList, err := store.List(sess)
		if err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": storeError(err)})
			return
		}
		if !sessionHasTitle(cfg, jobsList, provider, id) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		got, err := library.Get(cfg, provider, id)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, got)
	})
	mux.HandleFunc("DELETE /v1/catalog/{provider}/{id}", func(w http.ResponseWriter, r *http.Request) {
		store.PurgeExpired(cfg.SessionTTL())
		provider, id := r.PathValue("provider"), r.PathValue("id")
		pins, err := store.Pinning(cfg.SessionTTL(), cfg, provider, id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if len(pins) > 0 {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "pinned by " + strings.Join(pins, ", ")})
			return
		}
		if err := library.Remove(cfg, provider, id); err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"provider": provider, "id": id})
	})
	mux.HandleFunc("GET /v1/catalog/{provider}/{id}/{path...}", func(w http.ResponseWriter, r *http.Request) {
		sess, ok := requireSession(w, r, store, cfg, scans)
		if !ok {
			return
		}
		jobsList, err := store.List(sess)
		if err != nil || !sessionHasTitle(cfg, jobsList, r.PathValue("provider"), r.PathValue("id")) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		season, episode, ok := parseCatalogFile(r.PathValue("path"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		path, err := library.PosterFile(cfg, r.PathValue("provider"), r.PathValue("id"), season, episode)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		http.ServeFile(w, r, path)
	})

	public := filepath.Join(exeDir, "public")
	if st, err := os.Stat(public); err != nil || !st.IsDir() {
		log.Fatalf("public dir missing: %s", public)
	}
	mux.Handle("GET /", http.FileServer(http.Dir(public)))

	log.Printf("matchora %s listening on %s (data=%s ranker=%s)", cfg.Version, cfg.HTTP.Addr, cfg.DataDir, cfg.Ranker)
	if err := http.ListenAndServe(cfg.HTTP.Addr, mux); err != nil {
		log.Fatal(err)
	}
}

func parseCatalogFile(rest string) (season, episode string, ok bool) {
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return "", "", false
	}
	parts := strings.Split(rest, "/")
	isPoster := func(name string) bool {
		return strings.HasPrefix(strings.ToLower(name), "poster.")
	}
	switch {
	case len(parts) == 1 && isPoster(parts[0]):
		return "", "", true
	case len(parts) == 3 && parts[0] == "seasons" && isPoster(parts[2]):
		return parts[1], "", true
	case len(parts) == 5 && parts[0] == "seasons" && parts[2] == "episodes" && isPoster(parts[4]):
		return parts[1], parts[3], true
	default:
		return "", "", false
	}
}

func ingestBody(r *http.Request) (name, contentType string, body io.ReadCloser, err error) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			return "", "", nil, err
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			return "", "", nil, err
		}
		name = hdr.Filename
		if hdr.Header != nil && hdr.Header.Get("Content-Type") != "" {
			ct = hdr.Header.Get("Content-Type")
		}
		return name, ct, f, nil
	}
	return "", ct, r.Body, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func restartSoon() {
	go func() {
		time.Sleep(time.Second)
		llama.Stop()
		exe, err := os.Executable()
		if err != nil {
			log.Fatalf("restart: %v", err)
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		log.Printf("matchora restarting")
		if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
			log.Fatalf("restart: %v", err)
		}
	}()
}

func skipEpisodePosters(r *http.Request) bool {
	if truthyFlag(r.URL.Query().Get("skip_episode_posters")) {
		return true
	}
	return truthyFlag(r.FormValue("skip_episode_posters"))
}

func truthyFlag(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func requireSession(w http.ResponseWriter, r *http.Request, store *jobs.Store, cfg config.Config, scans *scanRun) (string, bool) {
	id := strings.TrimSpace(r.URL.Query().Get("session"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session required"})
		return "", false
	}
	if !jobs.ValidSessionID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session"})
		return "", false
	}
	for _, gone := range store.PurgeExpired(cfg.SessionTTL()) {
		scans.abortIf(gone)
	}
	if jobs.SessionExpired(id, time.Now(), cfg.SessionTTL()) || !store.Has(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return "", false
	}
	return id, true
}

func storeStatus(err error) int {
	if err == jobs.ErrInvalidSession {
		return http.StatusBadRequest
	}
	if os.IsNotExist(err) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

func storeError(err error) string {
	if err == nil {
		return ""
	}
	if err == jobs.ErrInvalidSession {
		return "invalid session"
	}
	if os.IsNotExist(err) {
		return "not found"
	}
	return err.Error()
}

func filterCatalog(cfg config.Config, titles []library.Title, list []match.Job) []library.Title {
	out := make([]library.Title, 0)
	for _, t := range titles {
		if sessionHasTitle(cfg, list, t.Provider, t.ID) {
			out = append(out, t)
		}
	}
	return out
}

func sessionHasTitle(cfg config.Config, list []match.Job, provider, id string) bool {
	for _, j := range list {
		if j.Match != nil && library.SameTitle(cfg, j.Match.Provider, j.Match.ID, provider, id) {
			return true
		}
	}
	return false
}

func filterWaits(all []match.Wait, list []match.Job) []match.Wait {
	ids := map[string]bool{}
	for _, j := range list {
		ids[j.ID] = true
	}
	out := make([]match.Wait, 0)
	for _, w := range all {
		if ids[w.JobID] {
			out = append(out, w)
		}
	}
	return out
}

func enqueueScan(ctx context.Context, cfg config.Config, store *jobs.Store, worker *jobs.Worker, scans *scanRun, session string, children []scan.Child) {
	defer scans.done()
	defer func() {
		if ctx.Err() != nil {
			scans.halt(session)
			return
		}
		scans.stop(session)
	}()
	for _, child := range children {
		if ctx.Err() != nil {
			return
		}
		created := jobsFromShows(match.Group(ctx, cfg, childListing(cfg.BrowseRoot, child)), cfg.BrowseRoot, child)
		if ctx.Err() != nil {
			return
		}
		if len(created) == 0 {
			log.Printf("scan: no shows for %s", child.Path)
		} else {
			if _, err := store.Append(session, created); err != nil {
				log.Printf("scan: append: %v", err)
				return
			}
			worker.Kick()
		}
		n := child.Videos
		if n < 1 {
			n = 1
		}
		scans.step(session, n)
	}
}

func childListing(root string, child scan.Child) string {
	rel, err := filepath.Rel(root, child.Path)
	if err != nil {
		rel = filepath.Base(child.Path)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		rel = filepath.Base(child.Path)
	}
	var b strings.Builder
	b.WriteString("Path: ")
	b.WriteString(rel)
	b.WriteByte('\n')
	b.WriteString(child.Listing)
	return b.String()
}

func jobsFromShows(shows []match.Grouped, root string, child scan.Child) []match.Job {
	if len(shows) == 0 {
		return nil
	}
	rows := make([]ingest.Row, 0, len(shows))
	for _, s := range shows {
		rows = append(rows, ingest.Row{
			Title: s.Title,
			Year:  s.Year,
		})
	}
	created := jobs.FromRows(rows, "scan")
	for i := range created {
		created[i].Path = resolveShowPath(root, child.Path, shows[i].Path)
	}
	return created
}

func resolveShowPath(root, childAbs, rel string) string {
	if rel == "" {
		return childAbs
	}
	if filepath.IsAbs(rel) {
		if matchfs.Within(root, rel) {
			return filepath.Clean(rel)
		}
		return childAbs
	}
	abs := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	if !matchfs.Within(root, abs) {
		return childAbs
	}
	if _, err := os.Stat(abs); err != nil {
		return childAbs
	}
	return abs
}

type scanRun struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	session string
	wg      sync.WaitGroup
	progs   map[string]*scanProg
}

func newScanRun() *scanRun {
	return &scanRun{progs: map[string]*scanProg{}}
}

func (s *scanRun) abortIf(session string) {
	s.mu.Lock()
	active := s.session == session
	s.mu.Unlock()
	if active {
		s.abort()
	}
}

func (s *scanRun) abort() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	sess := s.session
	p := s.progs[sess]
	s.session = ""
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
	if p != nil {
		p.halt()
	}
}

func (s *scanRun) start(session string, files, chunks int) context.Context {
	s.abort()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	s.mu.Lock()
	s.cancel = cancel
	s.session = session
	if s.progs == nil {
		s.progs = map[string]*scanProg{}
	}
	p := newScanProg()
	p.start(files, chunks)
	s.progs[session] = p
	s.wg.Add(1)
	s.mu.Unlock()
	return ctx
}

func (s *scanRun) snapshot(session string) map[string]any {
	s.mu.Lock()
	p := s.progs[session]
	s.mu.Unlock()
	if p == nil {
		return map[string]any{"files": 0, "done": 0, "chunks": 0, "chunk": 0, "running": false}
	}
	return p.snapshot()
}

func (s *scanRun) step(session string, n int) {
	s.mu.Lock()
	p := s.progs[session]
	s.mu.Unlock()
	if p != nil {
		p.step(n)
	}
}

func (s *scanRun) halt(session string) {
	s.mu.Lock()
	p := s.progs[session]
	s.mu.Unlock()
	if p != nil {
		p.halt()
	}
}

func (s *scanRun) stop(session string) {
	s.mu.Lock()
	p := s.progs[session]
	s.mu.Unlock()
	if p != nil {
		p.stop()
	}
}

func (s *scanRun) done() {
	s.wg.Done()
}

type scanProg struct {
	mu            sync.Mutex
	files, done   int
	chunks, chunk int
	running       bool
}

func newScanProg() *scanProg {
	return &scanProg{}
}

func (p *scanProg) snapshot() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return map[string]any{
		"files":   p.files,
		"done":    p.done,
		"chunks":  p.chunks,
		"chunk":   p.chunk,
		"running": p.running,
	}
}

func (p *scanProg) start(files, chunks int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.files = files
	p.done = 0
	p.chunks = chunks
	p.chunk = 0
	p.running = true
}

func (p *scanProg) step(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.chunk++
	p.done += n
	if p.files > 0 && p.done > p.files {
		p.done = p.files
	}
}

func (p *scanProg) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = false
	p.done = p.files
	p.chunk = p.chunks
}

func (p *scanProg) halt() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = false
}
