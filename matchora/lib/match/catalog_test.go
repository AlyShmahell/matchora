package match

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alyshmahell/matchora/lib/config"
)

func tvmazeCatalogSpec(base string) config.Provider {
	return config.Provider{
		Base: base,
		Catalog: &config.Catalog{
			Seasons: &config.CatalogList{
				URL:    "{base}/shows/{id}/seasons",
				Items:  "$",
				Fields: map[string]string{"id": "id", "number": "number", "title": "name", "synopsis": "summary", "poster": "image.medium", "year": "premiereDate"},
				Year:   "prefix4",
			},
			Episodes: &config.CatalogList{
				URL:    "{base}/shows/{id}/episodes",
				Items:  "$",
				Fields: map[string]string{"id": "id", "number": "number", "title": "name", "synopsis": "summary", "poster": "image.medium", "season": "season", "year": "airdate"},
				Year:   "prefix4",
			},
		},
	}
}

func TestFetchCatalogTVMazeGrouped(t *testing.T) {
	var seasonsPath, episodesPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/seasons"):
			seasonsPath = r.URL.Path
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": 10, "number": 1, "name": "", "summary": "<p>One.</p>", "premiereDate": "2012-04-15", "image": map[string]any{"medium": "http://img/s1.png"}},
				map[string]any{"id": 11, "number": 2, "name": "Season Two", "summary": "Two.", "premiereDate": "2013-01-13"},
			})
		case strings.HasSuffix(r.URL.Path, "/episodes"):
			episodesPath = r.URL.Path
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": 1, "number": 1, "season": 1, "name": "Pilot", "summary": "<p>Hannah.</p>", "airdate": "2012-04-15", "image": map[string]any{"medium": "http://img/e1.png"}},
				map[string]any{"id": 2, "number": 1, "season": 2, "name": "It's About Time", "airdate": "2013-01-13"},
				map[string]any{"id": 3, "number": 2, "season": 2, "name": "I Get Ideas", "airdate": "2013-01-20"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		HTTP:      config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000},
		Providers: map[string]config.Provider{"tvmaze": tvmazeCatalogSpec(srv.URL)},
	}
	job := Job{Title: "Girls"}
	cat, forKey := fetchCatalog(context.Background(), cfg, newHTTP(cfg), job, Candidate{Provider: "tvmaze", ID: "139"})
	if forKey != "tvmaze:139" {
		t.Fatalf("catalog_for=%q", forKey)
	}
	if seasonsPath != "/shows/139/seasons" || episodesPath != "/shows/139/episodes" {
		t.Fatalf("seasons=%q episodes=%q", seasonsPath, episodesPath)
	}
	if len(cat) != 2 {
		t.Fatalf("seasons=%d %+v", len(cat), cat)
	}
	if cat[0].Title != "Season 1" || cat[0].Number != "1" || cat[0].Year != "2012" {
		t.Fatalf("s1=%+v", cat[0])
	}
	if cat[0].Synopsis != "One." || cat[0].Poster != "http://img/s1.png" {
		t.Fatalf("s1 extra=%+v", cat[0])
	}
	if len(cat[0].Episodes) != 1 || cat[0].Episodes[0].Title != "Pilot" || cat[0].Episodes[0].Year != "2012" {
		t.Fatalf("s1 eps=%+v", cat[0].Episodes)
	}
	if cat[1].Title != "Season Two" || len(cat[1].Episodes) != 2 {
		t.Fatalf("s2=%+v", cat[1])
	}
	if cat[1].Episodes[0].Title != "It's About Time" || cat[1].Episodes[1].Title != "I Get Ideas" {
		t.Fatalf("s2 eps=%+v", cat[1].Episodes)
	}
}

func TestFetchCatalogTMDBPerSeason(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Query().Get("api_key") != "test-key" {
			http.Error(w, "key", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/tv/1396":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"seasons": []any{
					map[string]any{"id": 3572, "season_number": 1, "name": "Season 1", "overview": "Year one.", "poster_path": "/s1.jpg"},
					map[string]any{"id": 3573, "season_number": 2, "name": "Season 2", "overview": "Year two.", "poster_path": "/s2.jpg"},
				},
			})
		case "/tv/1396/season/1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"episodes": []any{
					map[string]any{"id": 62085, "episode_number": 1, "name": "Pilot", "overview": "Walt.", "still_path": "/e1.jpg"},
				},
			})
		case "/tv/1396/season/2":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"episodes": []any{
					map[string]any{"id": 62086, "episode_number": 1, "name": "Seven Thirty-Seven", "overview": "More.", "still_path": "/e2.jpg"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	spec := config.Provider{
		Require:      "api_key",
		APIKey:       "test-key",
		Base:         srv.URL,
		PosterPrefix: "https://image.tmdb.org/t/p/w185",
		Catalog: &config.Catalog{
			Seasons: &config.CatalogList{
				URL:          "{base}/tv/{id}",
				Query:        map[string]string{"api_key": "{api_key}"},
				Items:        "seasons",
				Fields:       map[string]string{"id": "id", "number": "season_number", "title": "name", "synopsis": "overview", "poster": "poster_path"},
				PosterPrefix: "https://image.tmdb.org/t/p/w185",
			},
			Episodes: &config.CatalogList{
				URL:          "{base}/tv/{id}/season/{season}",
				Query:        map[string]string{"api_key": "{api_key}"},
				Items:        "episodes",
				Fields:       map[string]string{"id": "id", "number": "episode_number", "title": "name", "synopsis": "overview", "poster": "still_path"},
				PosterPrefix: "https://image.tmdb.org/t/p/w185",
			},
		},
	}
	cfg := config.Config{
		HTTP:      config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000},
		Providers: map[string]config.Provider{"tmdb_tv": spec},
	}
	cat, forKey := fetchCatalog(context.Background(), cfg, newHTTP(cfg), Job{Title: "Breaking Bad"}, Candidate{Provider: "tmdb_tv", ID: "1396"})
	if forKey != "tmdb_tv:1396" {
		t.Fatalf("catalog_for=%q", forKey)
	}
	joined := strings.Join(paths, " ")
	if !strings.Contains(joined, "/tv/1396/season/1") || !strings.Contains(joined, "/tv/1396/season/2") {
		t.Fatalf("paths=%v", paths)
	}
	if strings.Contains(joined, "{season}") {
		t.Fatalf("unexpanded season in %v", paths)
	}
	if len(cat) != 2 {
		t.Fatalf("seasons=%d %+v", len(cat), cat)
	}
	if cat[0].Poster != "https://image.tmdb.org/t/p/w185/s1.jpg" {
		t.Fatalf("poster=%q", cat[0].Poster)
	}
	if len(cat[0].Episodes) != 1 || cat[0].Episodes[0].Title != "Pilot" {
		t.Fatalf("s1 eps=%+v", cat[0].Episodes)
	}
	if cat[0].Episodes[0].Poster != "https://image.tmdb.org/t/p/w185/e1.jpg" {
		t.Fatalf("still=%q", cat[0].Episodes[0].Poster)
	}
	if len(cat[1].Episodes) != 1 || cat[1].Episodes[0].Title != "Seven Thirty-Seven" {
		t.Fatalf("s2 eps=%+v", cat[1].Episodes)
	}
}

func TestFetchCatalogEmptyTitleBecomesSeasonN(t *testing.T) {
	item := map[string]any{"id": 1, "number": 3, "name": ""}
	list := &config.CatalogList{
		Fields: map[string]string{"id": "id", "number": "number", "title": "name"},
	}
	s, ok := seasonFrom(config.Provider{}, list, item)
	if !ok {
		t.Fatal("expected season")
	}
	if s.Title != "Season 3" || s.Number != "3" || s.ID != "1" {
		t.Fatalf("got %+v", s)
	}
}

func TestFetchCatalogNoBlockNil(t *testing.T) {
	cfg := config.Config{
		HTTP: config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000},
		Providers: map[string]config.Provider{
			"omdb": {Base: "http://example.invalid"},
		},
	}
	cat, forKey := fetchCatalog(context.Background(), cfg, newHTTP(cfg), Job{Title: "Dune"}, Candidate{Provider: "omdb", ID: "tt1"})
	if cat != nil || forKey != "" {
		t.Fatalf("cat=%+v for=%q", cat, forKey)
	}
}

func TestApplyCatalogUnknownCandidate(t *testing.T) {
	job := Job{
		ID:     "j1",
		Title:  "Girls",
		Status: "matched",
		Match:  &Candidate{Provider: "tvmaze", ID: "139", Title: "Girls"},
		Candidates: []Candidate{
			{Provider: "tvmaze", ID: "139", Title: "Girls"},
		},
	}
	_, err := ApplyCatalog(context.Background(), config.Config{}, job, "tvmaze", "999")
	if err == nil || err.Error() != "candidate not found" {
		t.Fatalf("err=%v", err)
	}
}

func TestNeedsCatalog(t *testing.T) {
	cfg := config.Config{
		Providers: map[string]config.Provider{
			"tvmaze": tvmazeCatalogSpec("http://x"),
			"omdb":   {},
		},
	}
	matched := Job{Status: "matched", Match: &Candidate{Provider: "tvmaze", ID: "1"}}
	if !NeedsCatalog(cfg, matched) {
		t.Fatal("expected needs catalog")
	}
	matched.Catalog = []CatalogSeason{}
	if NeedsCatalog(cfg, matched) {
		t.Fatal("empty slice is loaded")
	}
	movie := Job{Status: "matched", Match: &Candidate{Provider: "omdb", ID: "tt1"}}
	if NeedsCatalog(cfg, movie) {
		t.Fatal("omdb has no catalog")
	}
}

func TestFinishRankFetchesCatalog(t *testing.T) {
	var catalogHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/seasons") || strings.Contains(r.URL.Path, "/episodes") {
			catalogHits.Add(1)
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"score": 1, "show": map[string]any{"id": 139, "name": "Girls", "premiered": "2012", "url": "http://t"}},
		})
	}))
	t.Cleanup(srv.Close)
	spec := tvmazeCatalogSpec(srv.URL)
	spec.URL = "{base}/search/shows"
	spec.Query = map[string]string{"q": "{title}"}
	spec.Items = "$"
	spec.Fields = map[string]string{"id": "show.id", "title": "show.name", "year": "show.premiered", "url": "show.url"}
	cfg := config.Config{
		HTTP:  config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000},
		Match: config.Match{MinScore: 0.01, MinMargin: 0.04},
		Providers: map[string]config.Provider{
			"tvmaze": spec,
		},
	}
	job := runOne(context.Background(), cfg, newHTTP(cfg), Job{Title: "Girls"})
	if job.Status != "matched" {
		t.Fatalf("status=%s err=%s", job.Status, job.Error)
	}
	if job.Catalog == nil {
		t.Fatal("catalog still nil")
	}
	if job.CatalogFor != "tvmaze:139" {
		t.Fatalf("catalog_for=%q", job.CatalogFor)
	}
	if catalogHits.Load() < 2 {
		t.Fatalf("catalog hits=%d", catalogHits.Load())
	}
}

func TestMapFilesOntoCatalog(t *testing.T) {
	ep := "/mnt/show/Season 1/Show S01E01.mkv"
	job := Job{
		Files: []JobFile{
			{Path: ep, Season: "1", Episode: "1"},
			{Path: "/mnt/show/Season 1/bonus.mkv"},
		},
		Catalog: []CatalogSeason{{
			Number: "01",
			Title:  "Season 1",
			Episodes: []CatalogEpisode{
				{Number: "1", Title: "Pilot"},
				{Number: "2", Title: "Next"},
			},
		}},
	}
	got := MapFilesOntoCatalog(job)
	if got.Catalog[0].Episodes[0].Path != ep {
		t.Fatalf("path=%q", got.Catalog[0].Episodes[0].Path)
	}
	if got.Catalog[0].Episodes[1].Path != "" {
		t.Fatal("unmapped episode got a path")
	}
}
