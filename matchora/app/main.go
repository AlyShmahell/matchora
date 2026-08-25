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
	"time"

	"github.com/alyshmahell/matchora/lib/config"
	matchfs "github.com/alyshmahell/matchora/lib/fs"
	"github.com/alyshmahell/matchora/lib/ingest"
	"github.com/alyshmahell/matchora/lib/jobs"
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
	if err := llama.Start(cfg); err != nil {
		llama.Stop()
		log.Fatal(err)
	}
	if *prepare {
		llama.Stop()
		return
	}
	store := jobs.New(cfg.DataDir)
	worker := jobs.NewWorker(cfg, store)
	scans := newScanRun()

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
	mux.HandleFunc("GET /v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		list, err := store.List()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("GET /v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, scans.prog.snapshot())
	})
	mux.HandleFunc("GET /v1/match/log", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, worker.Waits())
	})
	mux.HandleFunc("POST /v1/scan", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path required"})
			return
		}
		videos, err := scan.ListVideos(cfg.BrowseRoot, body.Path)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if len(videos) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no titles in path"})
			return
		}
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
		if len(children) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no titles in path"})
			return
		}
		ctx := scans.start(len(videos), len(children))
		writeJSON(w, http.StatusAccepted, map[string]any{"files": len(videos)})
		go enqueueScan(ctx, cfg, store, worker, scans, children)
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
		if _, err := store.Append(created); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		worker.Kick()
		writeJSON(w, http.StatusAccepted, created)
	})
	mux.HandleFunc("POST /v1/match", func(w http.ResponseWriter, r *http.Request) {
		list, err := store.MarkPending()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		worker.Kick()
		writeJSON(w, http.StatusAccepted, list)
	})
	mux.HandleFunc("DELETE /v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		scans.abort()
		if err := store.Clear(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, []match.Job{})
	})
	mux.HandleFunc("POST /v1/retry", func(w http.ResponseWriter, r *http.Request) {
		list, err := store.MarkErrorsPending()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		worker.Kick()
		writeJSON(w, http.StatusAccepted, list)
	})
	mux.HandleFunc("POST /v1/jobs/{id}/select", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Provider string `json:"provider"`
			ID       string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Provider == "" || body.ID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider and id required"})
			return
		}
		job, err := store.Get(r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), cfg.HTTPTimeout())
		done, err := match.ApplySelect(ctx, cfg, job, body.Provider, body.ID)
		cancel()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := store.Update(map[string]match.Job{done.ID: done}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, done)
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
}

func enqueueScan(ctx context.Context, cfg config.Config, store *jobs.Store, worker *jobs.Worker, scans *scanRun, children []scan.Child) {
	defer scans.done()
	defer scans.prog.stop()
	for _, child := range children {
		if ctx.Err() != nil {
			return
		}
		created := jobsFromShows(match.Group(ctx, cfg, child.Listing), child.Path)
		if ctx.Err() != nil {
			return
		}
		if len(created) == 0 {
			log.Printf("scan: no shows for %s", child.Path)
		} else {
			if _, err := store.Append(created); err != nil {
				log.Printf("scan: append: %v", err)
				return
			}
			worker.Kick()
		}
		n := child.Videos
		if n < 1 {
			n = 1
		}
		scans.prog.step(n)
	}
}

func jobsFromShows(shows []match.Cleaned, path string) []match.Job {
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
		created[i].Path = path
	}
	return created
}

type scanRun struct {
	prog   *scanProg
	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newScanRun() *scanRun {
	return &scanRun{prog: newScanProg()}
}

func (s *scanRun) abort() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
	s.prog.reset()
}

func (s *scanRun) start(files, chunks int) context.Context {
	s.abort()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	s.mu.Lock()
	s.cancel = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	s.prog.start(files, chunks)
	return ctx
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

func (p *scanProg) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.files = 0
	p.done = 0
	p.chunks = 0
	p.chunk = 0
	p.running = false
}
