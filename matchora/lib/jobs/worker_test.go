package jobs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alyshmahell/matchora/lib/config"
	"github.com/alyshmahell/matchora/lib/match"
)

func TestWorkerRunsPendingJobsInParallel(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unlock)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"score": 1, "show": map[string]any{"id": 1, "name": "Girls", "premiered": "2012", "url": "http://t"}},
		})
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{
		HTTP:  config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 2000},
		Match: config.Match{MinScore: 0.72, MinMargin: 0.04, Workers: 2},
		Providers: map[string]config.Provider{
			"tvmaze": {
				Types:  []string{"tv", ""},
				Base:   srv.URL,
				URL:    "{base}/search/shows",
				Query:  map[string]string{"q": "{title}"},
				Items:  "$",
				Fields: map[string]string{"id": "show.id", "title": "show.name", "year": "show.premiered", "url": "show.url"},
			},
		},
	}
	store := New(t.TempDir())
	if err := store.ReplaceAll([]match.Job{
		{ID: "a", Title: "Girls", Status: "pending"},
		{ID: "b", Title: "Girls", Status: "pending"},
	}); err != nil {
		t.Fatal(err)
	}
	w := NewWorker(cfg, store)
	w.Kick()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("jobs did not overlap")
		}
	}
	open := 0
	for _, wait := range w.Waits() {
		if wait.Name == "tvmaze" && wait.Until == nil {
			open++
		}
	}
	if open != 2 {
		t.Fatalf("running waits=%d log=%+v", open, w.Waits())
	}
	unlock()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		list, err := store.List()
		if err != nil {
			t.Fatal(err)
		}
		ok := 0
		for _, j := range list {
			if j.Status == "matched" && j.Match != nil {
				ok++
			}
		}
		if ok == 2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	list, _ := store.List()
	t.Fatalf("jobs=%+v", list)
}

func TestWorkerWritesJobBeforeBatchFinishes(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unlock)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "Slow" {
			started <- struct{}{}
			<-release
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"score": 1, "show": map[string]any{"id": 2, "name": "Slow", "premiered": "2012", "url": "http://s"}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"score": 1, "show": map[string]any{"id": 1, "name": "Girls", "premiered": "2012", "url": "http://t"}},
		})
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{
		HTTP:  config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 2000},
		Match: config.Match{MinScore: 0.72, MinMargin: 0.04, Workers: 2},
		Providers: map[string]config.Provider{
			"tvmaze": {
				Types:  []string{"tv", ""},
				Base:   srv.URL,
				URL:    "{base}/search/shows",
				Query:  map[string]string{"q": "{title}"},
				Items:  "$",
				Fields: map[string]string{"id": "show.id", "title": "show.name", "year": "show.premiered", "url": "show.url"},
			},
		},
	}
	store := New(t.TempDir())
	if err := store.ReplaceAll([]match.Job{
		{ID: "fast", Title: "Girls", Status: "pending"},
		{ID: "slow", Title: "Slow", Status: "pending"},
	}); err != nil {
		t.Fatal(err)
	}
	NewWorker(cfg, store).Kick()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("slow job did not start")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		list, err := store.List()
		if err != nil {
			t.Fatal(err)
		}
		var fast, slow match.Job
		for _, j := range list {
			if j.ID == "fast" {
				fast = j
			}
			if j.ID == "slow" {
				slow = j
			}
		}
		if fast.Status == "matched" && slow.Status == "pending" {
			unlock()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	unlock()
	list, _ := store.List()
	t.Fatalf("expected fast matched while slow pending, jobs=%+v", list)
}

func TestWorkerSiblingTimeoutDoesNotStarveFast(t *testing.T) {
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Search": []any{map[string]any{"imdbID": "tt1", "Title": "Dune", "Year": "2021"}},
		})
	}))
	t.Cleanup(fast.Close)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(slow.Close)

	cfg := config.Config{
		HTTP:  config.HTTP{TimeoutMS: 200, Retries: 1, ProviderTimeoutMS: 400},
		Match: config.Match{MinScore: 0.72, MinMargin: 0.04, Workers: 2},
		Providers: map[string]config.Provider{
			"omdb": {
				Types:  []string{"movie"},
				Base:   fast.URL,
				URL:    "{base}",
				Query:  map[string]string{"s": "{title}"},
				Items:  "Search",
				Fields: map[string]string{"id": "imdbID", "title": "Title", "year": "Year", "url": "imdbID"},
			},
			"jikan": {
				Types:  []string{"anime"},
				Base:   slow.URL,
				URL:    "{base}/anime",
				Query:  map[string]string{"q": "{title}"},
				Items:  "data",
				Fields: map[string]string{"id": "mal_id", "title": "title", "year": "year", "url": "url"},
			},
		},
	}
	store := New(t.TempDir())
	if err := store.ReplaceAll([]match.Job{
		{ID: "movie", Title: "Dune", Type: "movie", Status: "pending"},
		{ID: "anime", Title: "Slow", Type: "anime", Status: "pending"},
	}); err != nil {
		t.Fatal(err)
	}
	NewWorker(cfg, store).Kick()

	deadline := time.Now().Add(3 * time.Second)
	var movie, anime match.Job
	for time.Now().Before(deadline) {
		list, err := store.List()
		if err != nil {
			t.Fatal(err)
		}
		for _, j := range list {
			if j.ID == "movie" {
				movie = j
			}
			if j.ID == "anime" {
				anime = j
			}
		}
		if movie.Status == "matched" && anime.Status != "pending" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if movie.Status != "matched" || movie.Match == nil || movie.Match.Provider != "omdb" {
		t.Fatalf("movie=%+v", movie)
	}
	if anime.Status == "pending" {
		t.Fatalf("anime still pending: %+v", anime)
	}
}

func TestWorkerBackfillsCatalog(t *testing.T) {
	var searchHits, catalogHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/search") {
			searchHits.Add(1)
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		catalogHits.Add(1)
		if strings.HasSuffix(r.URL.Path, "/seasons") {
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": 1, "number": 1, "name": "Season 1"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"id": 1, "number": 1, "season": 1, "name": "Pilot"},
		})
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{
		HTTP:  config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 2000},
		Match: config.Match{MinScore: 0.72, MinMargin: 0.04, Workers: 1},
		Providers: map[string]config.Provider{
			"tvmaze": {
				Types:  []string{"tv", ""},
				Base:   srv.URL,
				URL:    "{base}/search/shows",
				Query:  map[string]string{"q": "{title}"},
				Items:  "$",
				Fields: map[string]string{"id": "show.id", "title": "show.name", "year": "show.premiered", "url": "show.url"},
				Catalog: &config.Catalog{
					Seasons: &config.CatalogList{
						URL:    "{base}/shows/{id}/seasons",
						Items:  "$",
						Fields: map[string]string{"id": "id", "number": "number", "title": "name"},
					},
					Episodes: &config.CatalogList{
						URL:    "{base}/shows/{id}/episodes",
						Items:  "$",
						Fields: map[string]string{"id": "id", "number": "number", "title": "name", "season": "season"},
					},
				},
			},
		},
	}
	store := New(t.TempDir())
	if err := store.ReplaceAll([]match.Job{
		{
			ID:     "a",
			Title:  "Girls",
			Status: "matched",
			Match:  &match.Candidate{Provider: "tvmaze", ID: "139", Title: "Girls"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	NewWorker(cfg, store).Kick()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		list, err := store.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(list) == 1 && list[0].Status == "matched" && list[0].Catalog != nil {
			if searchHits.Load() != 0 {
				t.Fatalf("search hits=%d (rematch)", searchHits.Load())
			}
			if catalogHits.Load() < 1 {
				t.Fatalf("catalog hits=%d", catalogHits.Load())
			}
			if list[0].CatalogFor != "tvmaze:139" {
				t.Fatalf("catalog_for=%q", list[0].CatalogFor)
			}
			if len(list[0].Catalog) != 1 || list[0].Catalog[0].Title != "Season 1" {
				t.Fatalf("catalog=%+v", list[0].Catalog)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	list, _ := store.List()
	t.Fatalf("jobs=%+v search=%d catalog=%d", list, searchHits.Load(), catalogHits.Load())
}
