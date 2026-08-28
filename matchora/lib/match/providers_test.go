package match

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alyshmahell/matchora/lib/config"
)

func TestSearchProviderRetries504(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := n.Add(1)
		if i < 3 {
			time.Sleep(150 * time.Millisecond)
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{
				map[string]any{"mal_id": 1, "title": "Cowboy Bebop", "year": 1998, "url": "http://x"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{
		HTTP: config.HTTP{
			TimeoutMS:         5000,
			Retries:           3,
			Backoff:           config.ExpRange{MinExp: 0, MaxExp: 2},
			ProviderTimeoutMS: 200,
		},
		Providers: map[string]config.Provider{
			"jikan": {
				Types:  []string{"anime"},
				Base:   srv.URL,
				URL:    "{base}/anime",
				Query:  map[string]string{"q": "{title}"},
				Items:  "data",
				Fields: map[string]string{"id": "mal_id", "title": "title", "year": "year", "url": "url"},
			},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cands, err := searchProviders(ctx, cfg, newHTTP(cfg), Job{Title: "Cowboy Bebop", Type: "anime"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].Title != "Cowboy Bebop" {
		t.Fatalf("got %+v", cands)
	}
	if n.Load() < 3 {
		t.Fatalf("expected retries, got %d attempts", n.Load())
	}
}

func TestProviderRetriesOverride(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		HTTP: config.HTTP{
			TimeoutMS:         5000,
			Retries:           5,
			Backoff:           config.ExpRange{MinExp: 0, MaxExp: 2},
			ProviderTimeoutMS: 200,
		},
		Providers: map[string]config.Provider{
			"src": {
				Types:             []string{"anime"},
				Base:              srv.URL,
				URL:               "{base}/search",
				Query:             map[string]string{"q": "{title}"},
				Items:             "data",
				Fields:            map[string]string{"id": "id", "title": "title"},
				Retries:           1,
				ProviderTimeoutMS: 200,
			},
		},
	}
	_, err := searchProviders(context.Background(), cfg, newHTTP(cfg), Job{Title: "X", Type: "anime"})
	if err == nil {
		t.Fatal("expected error")
	}
	if n.Load() != 1 {
		t.Fatalf("attempts=%d", n.Load())
	}
}

func TestFetchDetailMergesSynopsis(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("i") != "tt1" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Title": "Dune", "Plot": "Sand.", "imdbID": "tt1"})
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		HTTP: config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000},
		Providers: map[string]config.Provider{
			"src": {
				Base: srv.URL,
				Detail: &config.Episode{
					URL:    "{base}",
					Query:  map[string]string{"i": "{id}"},
					Fields: map[string]string{"synopsis": "Plot"},
				},
			},
		},
	}
	cand := Candidate{Provider: "src", ID: "tt1", Title: "Dune"}
	fetchDetail(context.Background(), cfg, newHTTP(cfg), Job{Title: "Dune"}, &cand)
	if cand.Synopsis != "Sand." {
		t.Fatalf("synopsis=%q", cand.Synopsis)
	}
}

func TestUntypedSkipsDeferredProviders(t *testing.T) {
	var tv, jk atomic.Int32
	tvmaze := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tv.Add(1)
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
	cfg := config.Config{
		HTTP: config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000},
		Providers: map[string]config.Provider{
			"tvmaze": {
				Types:  []string{"tv", ""},
				Base:   tvmaze.URL,
				URL:    "{base}/search/shows",
				Query:  map[string]string{"q": "{title}"},
				Items:  "$",
				Fields: map[string]string{"id": "show.id", "title": "show.name", "year": "show.premiered", "url": "show.url"},
			},
			"jikan": {
				Types:  []string{"anime", "movie"},
				Base:   jikan.URL,
				URL:    "{base}/anime",
				Query:  map[string]string{"q": "{title}"},
				Items:  "data",
				Fields: map[string]string{"id": "mal_id", "title": "title", "year": "year", "url": "url"},
				Defer:  true,
			},
		},
	}
	cands, err := searchProviders(context.Background(), cfg, newHTTP(cfg), Job{Title: "Girls"})
	if err != nil {
		t.Fatal(err)
	}
	if tv.Load() != 1 || jk.Load() != 0 {
		t.Fatalf("tvmaze=%d jikan=%d", tv.Load(), jk.Load())
	}
	if len(cands) != 1 || cands[0].Provider != "tvmaze" {
		t.Fatalf("got %+v", cands)
	}
	slow, err := searchProvidersDefer(context.Background(), cfg, newHTTP(cfg), Job{Title: "Girls"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if jk.Load() != 1 {
		t.Fatalf("jikan=%d", jk.Load())
	}
	if len(slow) != 1 || slow[0].Provider != "jikan" {
		t.Fatalf("got %+v", slow)
	}
}

func TestNonDeferProvidersRunInParallel(t *testing.T) {
	var a, b atomic.Int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unlock)
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.Add(1)
		started <- struct{}{}
		<-release
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"score": 1, "show": map[string]any{"id": 1, "name": "Girls", "premiered": "2012", "url": "http://a"}},
		})
	}))
	t.Cleanup(srvA.Close)
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.Add(1)
		started <- struct{}{}
		<-release
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"score": 1, "show": map[string]any{"id": 2, "name": "Girls", "premiered": "2012", "url": "http://b"}},
		})
	}))
	t.Cleanup(srvB.Close)
	cfg := config.Config{
		HTTP: config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 2000},
		Providers: map[string]config.Provider{
			"one": {
				Types:  []string{"tv", ""},
				Base:   srvA.URL,
				URL:    "{base}/search/shows",
				Query:  map[string]string{"q": "{title}"},
				Items:  "$",
				Fields: map[string]string{"id": "show.id", "title": "show.name", "year": "show.premiered", "url": "show.url"},
			},
			"two": {
				Types:  []string{"tv", ""},
				Base:   srvB.URL,
				URL:    "{base}/search/shows",
				Query:  map[string]string{"q": "{title}"},
				Items:  "$",
				Fields: map[string]string{"id": "show.id", "title": "show.name", "year": "show.premiered", "url": "show.url"},
			},
		},
	}
	done := make(chan error, 1)
	go func() {
		cands, err := searchProviders(context.Background(), cfg, newHTTP(cfg), Job{Title: "Girls"})
		if err != nil {
			done <- err
			return
		}
		if len(cands) != 2 {
			done <- fmt.Errorf("len=%d", len(cands))
			return
		}
		done <- nil
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("providers did not overlap")
		}
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if a.Load() != 1 || b.Load() != 1 {
		t.Fatalf("one=%d two=%d", a.Load(), b.Load())
	}
}

func TestTypedSkipsUnwantedProviders(t *testing.T) {
	var tv atomic.Int32
	tvmaze := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tv.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(tvmaze.Close)
	jikan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"mal_id": 1, "title": "Cowboy Bebop", "year": 1998, "url": "http://j"}},
		})
	}))
	t.Cleanup(jikan.Close)
	cfg := config.Config{
		HTTP: config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000},
		Providers: map[string]config.Provider{
			"tvmaze": {
				Types:  []string{"tv", ""},
				Base:   tvmaze.URL,
				URL:    "{base}/search/shows",
				Query:  map[string]string{"q": "{title}"},
				Items:  "$",
				Fields: map[string]string{"id": "show.id", "title": "show.name", "year": "show.premiered", "url": "show.url"},
			},
			"jikan": {
				Types:  []string{"anime", "movie"},
				Base:   jikan.URL,
				URL:    "{base}/anime",
				Query:  map[string]string{"q": "{title}"},
				Items:  "data",
				Fields: map[string]string{"id": "mal_id", "title": "title", "year": "year", "url": "url"},
			},
		},
	}
	cands, err := searchProviders(context.Background(), cfg, newHTTP(cfg), Job{Title: "Cowboy Bebop", Type: "anime"})
	if err != nil {
		t.Fatal(err)
	}
	if tv.Load() != 0 {
		t.Fatalf("tvmaze should not be called, hits=%d", tv.Load())
	}
	if len(cands) != 1 || cands[0].Provider != "jikan" {
		t.Fatalf("got %+v", cands)
	}
}

func TestTypedMovieNeverHitsJikan(t *testing.T) {
	var jk atomic.Int32
	omdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Search": []any{map[string]any{"imdbID": "tt1", "Title": "Dune", "Year": "2021"}},
		})
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
		HTTP: config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000},
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
	job := Job{Title: "Dune", Type: "movie"}
	cands, err := searchProviders(context.Background(), cfg, newHTTP(cfg), job)
	if err != nil {
		t.Fatal(err)
	}
	if jk.Load() != 0 {
		t.Fatalf("jikan hits=%d", jk.Load())
	}
	if len(cands) != 1 || cands[0].Provider != "omdb" {
		t.Fatalf("got %+v", cands)
	}
	slow, err := searchProvidersDefer(context.Background(), cfg, newHTTP(cfg), job, true)
	if err != nil {
		t.Fatal(err)
	}
	if jk.Load() != 0 {
		t.Fatalf("deferred jikan hits=%d", jk.Load())
	}
	if len(slow) != 0 {
		t.Fatalf("slow=%+v", slow)
	}
}

func TestSearchProviderTMDB(t *testing.T) {
	var gotPath, gotKey, gotQuery, gotAdult string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.URL.Query().Get("api_key")
		gotQuery = r.URL.Query().Get("query")
		gotAdult = r.URL.Query().Get("include_adult")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []any{
				map[string]any{
					"id":           550,
					"title":        "Fight Club",
					"release_date": "1999-10-15",
					"overview":     "An insomniac office worker.",
					"poster_path":  "/pB8BM7pdSp6B6Ih7QZ4DrQ3PmJK.jpg",
				},
			},
		})
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		HTTP: config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000},
		Providers: map[string]config.Provider{
			"tmdb": {
				Types:        []string{"movie"},
				Require:      "api_key",
				APIKey:       "test-key",
				Base:         srv.URL,
				URL:          "{base}/search/movie",
				Query:        map[string]string{"api_key": "{api_key}", "query": "{title}", "include_adult": "false"},
				Items:        "results",
				Fields:       map[string]string{"id": "id", "title": "title", "year": "release_date", "url": "id", "synopsis": "overview", "poster": "poster_path"},
				Year:         "prefix4",
				URLPrefix:    "https://www.themoviedb.org/movie/",
				PosterPrefix: "https://image.tmdb.org/t/p/w185",
			},
		},
	}
	cands, err := searchProviders(context.Background(), cfg, newHTTP(cfg), Job{Title: "Fight Club", Type: "movie"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/search/movie" || gotKey != "test-key" || gotQuery != "Fight Club" || gotAdult != "false" {
		t.Fatalf("path=%q key=%q query=%q adult=%q", gotPath, gotKey, gotQuery, gotAdult)
	}
	if len(cands) != 1 {
		t.Fatalf("got %+v", cands)
	}
	c := cands[0]
	if c.Provider != "tmdb" || c.ID != "550" || c.Title != "Fight Club" || c.Year != "1999" {
		t.Fatalf("got %+v", c)
	}
	if c.URL != "https://www.themoviedb.org/movie/550" {
		t.Fatalf("url=%q", c.URL)
	}
	if c.Poster != "https://image.tmdb.org/t/p/w185/pB8BM7pdSp6B6Ih7QZ4DrQ3PmJK.jpg" {
		t.Fatalf("poster=%q", c.Poster)
	}
	if c.Synopsis != "An insomniac office worker." {
		t.Fatalf("synopsis=%q", c.Synopsis)
	}
}

func TestSearchProviderTMDBTV(t *testing.T) {
	var gotPath, gotKey, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.URL.Query().Get("api_key")
		gotQuery = r.URL.Query().Get("query")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []any{
				map[string]any{
					"id":             1396,
					"name":           "Breaking Bad",
					"first_air_date": "2008-01-20",
					"overview":       "A chemistry teacher.",
					"poster_path":    "/ggFHVNu6YYI5L9pCfOacjizRGt.jpg",
				},
			},
		})
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		HTTP: config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000},
		Providers: map[string]config.Provider{
			"tmdb_tv": {
				Types:        []string{"tv", "anime"},
				Require:      "api_key",
				APIKey:       "test-key",
				Secret:       "tmdb",
				Base:         srv.URL,
				URL:          "{base}/search/tv",
				Query:        map[string]string{"api_key": "{api_key}", "query": "{title}", "include_adult": "false"},
				Items:        "results",
				Fields:       map[string]string{"id": "id", "title": "name", "year": "first_air_date", "url": "id", "synopsis": "overview", "poster": "poster_path"},
				Year:         "prefix4",
				URLPrefix:    "https://www.themoviedb.org/tv/",
				PosterPrefix: "https://image.tmdb.org/t/p/w185",
				Defer:        true,
			},
		},
	}
	cands, err := searchProvidersDefer(context.Background(), cfg, newHTTP(cfg), Job{Title: "Breaking Bad", Type: "tv"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/search/tv" || gotKey != "test-key" || gotQuery != "Breaking Bad" {
		t.Fatalf("path=%q key=%q query=%q", gotPath, gotKey, gotQuery)
	}
	if len(cands) != 1 {
		t.Fatalf("got %+v", cands)
	}
	c := cands[0]
	if c.Provider != "tmdb_tv" || c.ID != "1396" || c.Title != "Breaking Bad" || c.Year != "2008" {
		t.Fatalf("got %+v", c)
	}
	if c.URL != "https://www.themoviedb.org/tv/1396" {
		t.Fatalf("url=%q", c.URL)
	}
	if c.Poster != "https://image.tmdb.org/t/p/w185/ggFHVNu6YYI5L9pCfOacjizRGt.jpg" {
		t.Fatalf("poster=%q", c.Poster)
	}
}

func TestTypedDoesNotFallBackToUnlisted(t *testing.T) {
	var tv atomic.Int32
	tvmaze := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tv.Add(1)
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"score": 1, "show": map[string]any{"id": 1, "name": "Black Clover", "premiered": "2017", "url": "http://t"}},
		})
	}))
	t.Cleanup(tvmaze.Close)
	jikan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	t.Cleanup(jikan.Close)
	cfg := config.Config{
		HTTP: config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 200},
		Providers: map[string]config.Provider{
			"tvmaze": {
				Types:  []string{"tv", ""},
				Base:   tvmaze.URL,
				URL:    "{base}/search/shows",
				Query:  map[string]string{"q": "{title}"},
				Items:  "$",
				Fields: map[string]string{"id": "show.id", "title": "show.name", "year": "show.premiered", "url": "show.url"},
			},
			"jikan": {
				Types:  []string{"anime"},
				Base:   jikan.URL,
				URL:    "{base}/anime",
				Query:  map[string]string{"q": "{title}"},
				Items:  "data",
				Fields: map[string]string{"id": "mal_id", "title": "title", "year": "year", "url": "url"},
			},
		},
	}
	cands, err := searchProviders(context.Background(), cfg, newHTTP(cfg), Job{Title: "Black Clover", Type: "anime"})
	if err == nil {
		t.Fatal("expected wanted-provider error")
	}
	if tv.Load() != 0 {
		t.Fatalf("tvmaze hits=%d", tv.Load())
	}
	if len(cands) != 0 {
		t.Fatalf("got %+v", cands)
	}
}

func TestUntypedEmptyPlus504IsNotError(t *testing.T) {
	tvmaze := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(tvmaze.Close)
	jikan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	t.Cleanup(jikan.Close)
	cfg := config.Config{
		HTTP: config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 200},
		Providers: map[string]config.Provider{
			"tvmaze": {
				Types:  []string{"tv", ""},
				Base:   tvmaze.URL,
				URL:    "{base}/search/shows",
				Query:  map[string]string{"q": "{title}"},
				Items:  "$",
				Fields: map[string]string{"id": "show.id", "title": "show.name", "year": "show.premiered", "url": "show.url"},
			},
			"jikan": {
				Types:  []string{"anime", "movie"},
				Base:   jikan.URL,
				URL:    "{base}/anime",
				Query:  map[string]string{"q": "{title}"},
				Items:  "data",
				Fields: map[string]string{"id": "mal_id", "title": "title", "year": "year", "url": "url"},
			},
		},
	}
	cands, err := searchProviders(context.Background(), cfg, newHTTP(cfg), Job{Title: "A Silent Voice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 0 {
		t.Fatalf("got %+v", cands)
	}
}

func TestPacedRetriesRespectInterval(t *testing.T) {
	var times []time.Time
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		times = append(times, time.Now())
		n := len(times)
		mu.Unlock()
		if n < 3 {
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"mal_id": 1, "title": "Cowboy Bebop", "year": 1998, "url": "http://x"}},
		})
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		HTTP: config.HTTP{
			TimeoutMS:         5000,
			Retries:           3,
			Backoff:           config.ExpRange{MinExp: 0, MaxExp: 2},
			ProviderTimeoutMS: 1000,
		},
		Providers: map[string]config.Provider{
			"jikan-paced": {
				Types:         []string{"anime"},
				Base:          srv.URL,
				URL:           "{base}/anime",
				Query:         map[string]string{"q": "{title}"},
				Items:         "data",
				Fields:        map[string]string{"id": "mal_id", "title": "title", "year": "year", "url": "url"},
				MinIntervalMS: 80,
			},
		},
	}
	cands, err := searchProviders(context.Background(), cfg, newHTTP(cfg), Job{Title: "Cowboy Bebop", Type: "anime"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 {
		t.Fatalf("got %+v", cands)
	}
	mu.Lock()
	got := append([]time.Time(nil), times...)
	mu.Unlock()
	if len(got) < 3 {
		t.Fatalf("attempts=%d", len(got))
	}
	gap := got[1].Sub(got[0])
	if gap < 70*time.Millisecond {
		t.Fatalf("gap=%s want >=80ms", gap)
	}
}

func TestCandidateFromSynopsisPoster(t *testing.T) {
	item := map[string]any{
		"show": map[string]any{
			"id":        139,
			"name":      "Girls",
			"premiered": "2012-04-15",
			"url":       "http://t",
			"summary":   "<p>Four friends in New York.</p>",
			"image":     map[string]any{"medium": "http://img/g.png"},
		},
	}
	spec := config.Provider{
		Fields: map[string]string{
			"id":       "show.id",
			"title":    "show.name",
			"year":     "show.premiered",
			"url":      "show.url",
			"synopsis": "show.summary",
			"poster":   "show.image.medium",
		},
		Year: "prefix4",
	}
	c, ok := candidateFrom("tvmaze", spec, item)
	if !ok {
		t.Fatal("expected candidate")
	}
	if c.Synopsis != "Four friends in New York." {
		t.Fatalf("synopsis=%q", c.Synopsis)
	}
	if c.Poster != "http://img/g.png" {
		t.Fatalf("poster=%q", c.Poster)
	}
	if c.Year != "2012" {
		t.Fatalf("year=%q", c.Year)
	}
}

func TestCandidateFromPosterPrefix(t *testing.T) {
	item := map[string]any{
		"id":           550,
		"title":        "Fight Club",
		"release_date": "1999-10-15",
		"poster_path":  "/pB8BM7pdSp6B6Ih7QZ4DrQ3PmJK.jpg",
	}
	spec := config.Provider{
		Fields: map[string]string{
			"id":     "id",
			"title":  "title",
			"year":   "release_date",
			"url":    "id",
			"poster": "poster_path",
		},
		Year:         "prefix4",
		URLPrefix:    "https://www.themoviedb.org/movie/",
		PosterPrefix: "https://image.tmdb.org/t/p/w185",
	}
	c, ok := candidateFrom("tmdb", spec, item)
	if !ok {
		t.Fatal("expected candidate")
	}
	if c.Year != "1999" {
		t.Fatalf("year=%q", c.Year)
	}
	if c.URL != "https://www.themoviedb.org/movie/550" {
		t.Fatalf("url=%q", c.URL)
	}
	if c.Poster != "https://image.tmdb.org/t/p/w185/pB8BM7pdSp6B6Ih7QZ4DrQ3PmJK.jpg" {
		t.Fatalf("poster=%q", c.Poster)
	}
	empty := map[string]any{"id": 1, "title": "No Poster"}
	c, ok = candidateFrom("tmdb", spec, empty)
	if !ok {
		t.Fatal("expected candidate")
	}
	if c.Poster != "" {
		t.Fatalf("empty poster=%q", c.Poster)
	}
}

func TestCandidateFromExtraAttrs(t *testing.T) {
	item := map[string]any{
		"show": map[string]any{
			"id":       10773,
			"name":     "Boku Dake ga Inai Machi",
			"type":     "Animation",
			"language": "Japanese",
		},
	}
	spec := config.Provider{
		Fields: map[string]string{
			"id":       "show.id",
			"title":    "show.name",
			"kind":     "show.type",
			"language": "show.language",
		},
	}
	c, ok := candidateFrom("tvmaze", spec, item)
	if !ok {
		t.Fatal("expected candidate")
	}
	if c.Attrs["kind"] != "Animation" || c.Attrs["language"] != "Japanese" {
		t.Fatalf("attrs=%v", c.Attrs)
	}
}
