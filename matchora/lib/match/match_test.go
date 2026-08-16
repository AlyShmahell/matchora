package match

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/alyshmahell/matchora/lib/config"
)

func TestRunOneSkipsDeferredWhenAutoMatch(t *testing.T) {
	var jk atomic.Int32
	tvmaze := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"score": 1, "show": map[string]any{"id": 1, "name": "Girls", "premiered": "2012", "url": "http://t"}},
		})
	}))
	t.Cleanup(tvmaze.Close)
	jikan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jk.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"mal_id": 1, "title": "Girls", "year": 2012, "url": "http://j"}},
		})
	}))
	t.Cleanup(jikan.Close)
	cfg := deferPairConfig(tvmaze.URL, jikan.URL)
	job := runOne(context.Background(), cfg, newHTTP(cfg), Job{Title: "Girls"})
	if job.Status != "matched" {
		t.Fatalf("status=%s err=%s", job.Status, job.Error)
	}
	if job.Match == nil || job.Match.Provider != "tvmaze" {
		t.Fatalf("match=%+v", job.Match)
	}
	if jk.Load() != 0 {
		t.Fatalf("deferred provider hits=%d", jk.Load())
	}
}

func TestRunOneCallsDeferredWhenFastEmpty(t *testing.T) {
	var jk atomic.Int32
	tvmaze := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(tvmaze.Close)
	jikan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jk.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"mal_id": 1, "title": "Cowboy Bebop", "year": 1998, "url": "http://j"}},
		})
	}))
	t.Cleanup(jikan.Close)
	cfg := deferPairConfig(tvmaze.URL, jikan.URL)
	job := runOne(context.Background(), cfg, newHTTP(cfg), Job{Title: "Cowboy Bebop"})
	if jk.Load() != 1 {
		t.Fatalf("deferred provider hits=%d", jk.Load())
	}
	if job.Status != "matched" || job.Match == nil || job.Match.Provider != "jikan" {
		t.Fatalf("status=%s match=%+v err=%s", job.Status, job.Match, job.Error)
	}
}

func TestRunOneSkipsDeferredWhenManualHighScores(t *testing.T) {
	var jk atomic.Int32
	tvmaze := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"score": 1, "show": map[string]any{"id": 1, "name": "Girls", "premiered": "2012", "url": "http://t"}},
			map[string]any{"score": 1, "show": map[string]any{"id": 2, "name": "Girls Again", "premiered": "2013", "url": "http://t2"}},
		})
	}))
	t.Cleanup(tvmaze.Close)
	jikan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jk.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"mal_id": 1, "title": "Girls", "year": 2012, "url": "http://j"}},
		})
	}))
	t.Cleanup(jikan.Close)
	cfg := deferPairConfig(tvmaze.URL, jikan.URL)
	job := runOne(context.Background(), cfg, newHTTP(cfg), Job{Title: "Girls"})
	if job.Status != "manual" {
		t.Fatalf("status=%s err=%s candidates=%+v", job.Status, job.Error, job.Candidates)
	}
	if jk.Load() != 0 {
		t.Fatalf("deferred provider hits=%d", jk.Load())
	}
}

func TestRunOneCallsDeferredWhenSeveralWeakHits(t *testing.T) {
	var jk atomic.Int32
	tvmaze := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"score": 1, "show": map[string]any{"id": 1, "name": "Unrelated Show", "premiered": "1999", "url": "http://t"}},
			map[string]any{"score": 1, "show": map[string]any{"id": 2, "name": "Other Thing", "premiered": "2001", "url": "http://t2"}},
		})
	}))
	t.Cleanup(tvmaze.Close)
	jikan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jk.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"mal_id": 1, "title": "Cowboy Bebop", "year": 1998, "url": "http://j"}},
		})
	}))
	t.Cleanup(jikan.Close)
	cfg := deferPairConfig(tvmaze.URL, jikan.URL)
	job := runOne(context.Background(), cfg, newHTTP(cfg), Job{Title: "Cowboy Bebop"})
	if jk.Load() != 1 {
		t.Fatalf("deferred provider hits=%d", jk.Load())
	}
	if job.Status != "matched" || job.Match == nil || job.Match.Provider != "jikan" {
		t.Fatalf("status=%s match=%+v err=%s", job.Status, job.Match, job.Error)
	}
}

func TestRunOneCallsDeferredWhenFastBelowMinScore(t *testing.T) {
	var jk atomic.Int32
	tvmaze := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"score": 1, "show": map[string]any{"id": 1, "name": "Unrelated Show", "premiered": "1999", "url": "http://t"}},
		})
	}))
	t.Cleanup(tvmaze.Close)
	jikan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jk.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"mal_id": 1, "title": "Cowboy Bebop", "year": 1998, "url": "http://j"}},
		})
	}))
	t.Cleanup(jikan.Close)
	cfg := deferPairConfig(tvmaze.URL, jikan.URL)
	job := runOne(context.Background(), cfg, newHTTP(cfg), Job{Title: "Cowboy Bebop"})
	if jk.Load() != 1 {
		t.Fatalf("deferred provider hits=%d", jk.Load())
	}
	if job.Status != "matched" || job.Match == nil || job.Match.Provider != "jikan" {
		t.Fatalf("status=%s match=%+v candidates=%+v err=%s", job.Status, job.Match, job.Candidates, job.Error)
	}
}

func deferPairConfig(tvURL, jkURL string) config.Config {
	return config.Config{
		HTTP:  config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000},
		Match: config.Match{MinScore: 0.72, MinMargin: 0.04},
		Providers: map[string]config.Provider{
			"tvmaze": {
				Types:  []string{"tv", ""},
				Base:   tvURL,
				URL:    "{base}/search/shows",
				Query:  map[string]string{"q": "{title}"},
				Items:  "$",
				Fields: map[string]string{"id": "show.id", "title": "show.name", "year": "show.premiered", "url": "show.url"},
			},
			"jikan": {
				Types:  []string{"anime", "movie"},
				Base:   jkURL,
				URL:    "{base}/anime",
				Query:  map[string]string{"q": "{title}"},
				Items:  "data",
				Fields: map[string]string{"id": "mal_id", "title": "title", "year": "year", "url": "url"},
				Defer:  true,
			},
		},
	}
}
