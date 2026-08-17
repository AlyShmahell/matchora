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

func TestRunOneTypedMovieSkipsJikanFallback(t *testing.T) {
	var jk atomic.Int32
	omdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"Search": []any{}})
	}))
	t.Cleanup(omdb.Close)
	jikan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jk.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"mal_id": 1, "title": "Dune", "year": 2021, "url": "http://j"}},
		})
	}))
	t.Cleanup(jikan.Close)
	cfg := config.Config{
		HTTP:  config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000},
		Match: config.Match{MinScore: 0.72, MinMargin: 0.04},
		Providers: map[string]config.Provider{
			"omdb": {
				Types:  []string{"movie", "tv", ""},
				Base:   omdb.URL,
				URL:    "{base}",
				Query:  map[string]string{"s": "{title}"},
				Items:  "Search",
				Fields: map[string]string{"id": "imdbID", "title": "Title", "year": "Year", "url": "imdbID"},
			},
			"jikan": {
				Types:  []string{"anime"},
				Base:   jikan.URL,
				URL:    "{base}/anime",
				Query:  map[string]string{"q": "{title}"},
				Items:  "data",
				Fields: map[string]string{"id": "mal_id", "title": "title", "year": "year", "url": "url"},
				Defer:  true,
			},
		},
	}
	job := runOne(context.Background(), cfg, newHTTP(cfg), Job{Title: "Dune", Type: "movie"})
	if jk.Load() != 0 {
		t.Fatalf("jikan hits=%d", jk.Load())
	}
	if job.Status != "unmatched" {
		t.Fatalf("status=%s err=%s match=%+v", job.Status, job.Error, job.Match)
	}
}

func TestPreferCandidatesKeepsAnimeShaped(t *testing.T) {
	cfg := config.Config{Match: config.Match{Prefer: map[string]map[string]string{
		"anime": {"language": "Japanese", "kind": "Animation"},
	}}}
	cands := []Candidate{
		{Title: "History Erased", Attrs: map[string]string{"language": "English", "kind": "Documentary"}},
		{Title: "Epithet Erased", Attrs: map[string]string{"language": "English", "kind": "Animation"}},
		{Title: "Boku Dake ga Inai Machi", Attrs: map[string]string{"language": "Japanese", "kind": "Animation"}},
	}
	got := preferCandidates(cfg, "anime", cands)
	if len(got) != 1 || got[0].Title != "Boku Dake ga Inai Machi" {
		t.Fatalf("got=%+v", got)
	}
}

func TestPreferCandidatesKeepsAllWhenNoneMatch(t *testing.T) {
	cfg := config.Config{Match: config.Match{Prefer: map[string]map[string]string{
		"anime": {"language": "Japanese", "kind": "Animation"},
	}}}
	cands := []Candidate{
		{Title: "History Erased", Attrs: map[string]string{"language": "English", "kind": "Documentary"}},
	}
	got := preferCandidates(cfg, "anime", cands)
	if len(got) != 1 || got[0].Title != "History Erased" {
		t.Fatalf("got=%+v", got)
	}
}

func TestAutoMatchSoloMinScore(t *testing.T) {
	cfg := config.Config{Match: config.Match{MinScore: 0.72, SoloMinScore: 0.01, MinMargin: 0.04}}
	if !autoMatch(cfg, []Candidate{{Score: 0.05}}) {
		t.Fatal("solo 0.05 should match")
	}
	if autoMatch(cfg, []Candidate{{Score: 0.80}, {Score: 0.79}}) {
		t.Fatal("two close high scores should stay manual")
	}
	if autoMatch(cfg, []Candidate{{Score: 0.50}, {Score: 0.10}}) {
		t.Fatal("two candidates still need min_score")
	}
}
