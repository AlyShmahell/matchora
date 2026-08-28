package library

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alyshmahell/matchora/lib/config"
	"github.com/alyshmahell/matchora/lib/match"
)

func TestDirNameSanitize(t *testing.T) {
	got := DirName(config.Config{}, match.Candidate{Provider: "tvmaze", ID: "139", Title: `Girls: A/B`, Year: "2012"})
	if !strings.HasPrefix(got, "[tvmaze-139] ") {
		t.Fatalf("prefix=%q", got)
	}
	if !strings.Contains(got, "Girls") || !strings.Contains(got, "2012") {
		t.Fatalf("dir=%q", got)
	}
	if strings.ContainsAny(got, `\/:*?"<>|`) {
		t.Fatalf("bad chars in %q", got)
	}
}

func TestSaveGetTVRoundTrip(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-bytes"))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir, HTTP: config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000}}
	cand := match.Candidate{Provider: "tvmaze", ID: "139", Title: "Girls", Year: "2012", Synopsis: "Four friends.", Poster: srv.URL + "/show.jpg"}
	job := match.Job{
		Type:       "tv",
		Status:     "matched",
		Match:      &cand,
		CatalogFor: "tvmaze:139",
		Catalog: []match.CatalogSeason{
			{
				ID: "10", Number: "1", Title: "", Synopsis: "One.", Year: "2012", Poster: srv.URL + "/s1.jpg",
				Episodes: []match.CatalogEpisode{
					{ID: "1", Number: "1", Title: "Pilot", Synopsis: "Hannah.", Year: "2012", Poster: srv.URL + "/e1.jpg"},
				},
			},
		},
	}
	if err := Save(context.Background(), cfg, job, cand, false); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "catalog")
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name() != "[tvmaze-139] Girls (2012)" {
		t.Fatalf("dirs=%v", ents)
	}
	show := filepath.Join(root, ents[0].Name())
	if _, err := os.Stat(filepath.Join(show, "tvshow.nfo")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(show, "poster.jpg")); err != nil {
		t.Fatal(err)
	}
	sdir := filepath.Join(show, "Season 01")
	b, err := os.ReadFile(filepath.Join(sdir, "season.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Season 1") {
		t.Fatalf("season nfo=%s", b)
	}
	if _, err := os.Stat(filepath.Join(sdir, "S01E01 Pilot.nfo")); err != nil {
		t.Fatal(err)
	}
	got, err := Get(cfg, "tvmaze", "139")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Girls" || got.Year != "2012" || got.Type != "tv" || got.Provider != "tvmaze" {
		t.Fatalf("got=%+v", got)
	}
	if got.Poster != "/v1/catalog/tvmaze/139/poster.jpg" {
		t.Fatalf("poster=%q", got.Poster)
	}
	if len(got.Seasons) != 1 || got.Seasons[0].Title != "Season 1" || len(got.Seasons[0].Episodes) != 1 {
		t.Fatalf("seasons=%+v", got.Seasons)
	}
	if got.Seasons[0].Episodes[0].Title != "Pilot" {
		t.Fatalf("ep=%+v", got.Seasons[0].Episodes[0])
	}
	if got.Seasons[0].Poster != "/v1/catalog/tvmaze/139/seasons/1/poster.jpg" {
		t.Fatalf("season poster=%q", got.Seasons[0].Poster)
	}
	if got.Seasons[0].Episodes[0].Poster != "/v1/catalog/tvmaze/139/seasons/1/episodes/1/poster.jpg" {
		t.Fatalf("ep poster=%q", got.Seasons[0].Episodes[0].Poster)
	}
	list, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "139" {
		t.Fatalf("list=%+v", list)
	}
	n := hits.Load()
	if err := Save(context.Background(), cfg, job, cand, false); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != n {
		t.Fatalf("re-downloaded posters: %d -> %d", n, hits.Load())
	}
}

func TestSaveMovie(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir}
	cand := match.Candidate{Provider: "omdb", ID: "tt1", Title: "Dune", Year: "2021", Synopsis: "Sand."}
	job := match.Job{Type: "movie", Status: "matched", Match: &cand}
	if err := Save(context.Background(), cfg, job, cand, false); err != nil {
		t.Fatal(err)
	}
	got, err := Get(cfg, "omdb", "tt1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "movie" || got.Title != "Dune" || len(got.Seasons) != 0 {
		t.Fatalf("got=%+v", got)
	}
	path := filepath.Join(dir, "catalog", DirName(cfg, cand), "movie.nfo")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "<movie>") || !strings.Contains(string(b), "tt1") {
		t.Fatalf("nfo=%s", b)
	}
}

func TestGetMissing(t *testing.T) {
	_, err := Get(config.Config{DataDir: t.TempDir()}, "tvmaze", "999")
	if err == nil || !os.IsNotExist(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestListEmpty(t *testing.T) {
	got, err := List(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("nil list")
	}
	b, _ := json.Marshal(got)
	if string(b) != "[]" {
		t.Fatalf("json=%s", b)
	}
}

func TestFindDirPrefix(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		DataDir:   dir,
		Providers: map[string]config.Provider{"tmdb_tv": {UniqueID: "tmdb-tv"}},
	}
	cand := match.Candidate{Provider: "tmdb_tv", ID: "1396", Title: "Breaking Bad", Year: "2008"}
	if err := Save(context.Background(), cfg, match.Job{Type: "tv"}, cand, false); err != nil {
		t.Fatal(err)
	}
	got, err := FindDir(cfg, Root(dir), "tmdb_tv", "1396")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "[tmdb-tv-1396] Breaking Bad (2008)" {
		t.Fatalf("dir=%q", got)
	}
	cand.Title = "Breaking Bad Remastered"
	if err := Save(context.Background(), cfg, match.Job{Type: "tv"}, cand, false); err != nil {
		t.Fatal(err)
	}
	got, err = FindDir(cfg, Root(dir), "tmdb_tv", "1396")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "[tmdb-tv-1396] Breaking Bad Remastered (2008)" {
		t.Fatalf("renamed=%q", got)
	}
}

func TestPosterFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir, HTTP: config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000}}
	cand := match.Candidate{Provider: "tvmaze", ID: "1", Title: "X", Poster: srv.URL}
	job := match.Job{
		Type: "tv", CatalogFor: "tvmaze:1", Catalog: []match.CatalogSeason{
			{Number: "2", Title: "Season 2", Poster: srv.URL, Episodes: []match.CatalogEpisode{
				{Number: "3", Title: "Three", Poster: srv.URL},
			}},
		},
	}
	if err := Save(context.Background(), cfg, job, cand, false); err != nil {
		t.Fatal(err)
	}
	p, err := PosterFile(cfg, "tvmaze", "1", "", "")
	if err != nil || !strings.HasSuffix(p, "poster.png") {
		t.Fatalf("root=%q err=%v", p, err)
	}
	p, err = PosterFile(cfg, "tvmaze", "1", "2", "")
	if err != nil || !strings.HasSuffix(p, "poster.png") {
		t.Fatalf("season=%q err=%v", p, err)
	}
	if p, err := PosterFile(cfg, "tvmaze", "1", "2", "3"); err != nil || !strings.Contains(p, "S02E03") {
		t.Fatalf("ep=%q err=%v", p, err)
	}
}

func TestSaveUsesNFOAndUniqueID(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		DataDir: dir,
		Providers: map[string]config.Provider{
			"src": {NFO: "movie", UniqueID: "tmdb-movie"},
		},
	}
	cand := match.Candidate{Provider: "src", ID: "129", Title: "Spirited Away", Year: "2001", Synopsis: "Bathhouse."}
	job := match.Job{Type: "anime", Status: "matched", Match: &cand}
	if err := Save(context.Background(), cfg, job, cand, false); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "catalog")
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name() != "[tmdb-movie-129] Spirited Away (2001)" {
		t.Fatalf("dirs=%v", ents)
	}
	show := filepath.Join(root, ents[0].Name())
	if _, err := os.Stat(filepath.Join(show, "tvshow.nfo")); err == nil {
		t.Fatal("wrote tvshow.nfo")
	}
	b, err := os.ReadFile(filepath.Join(show, "movie.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "<movie>") || !strings.Contains(s, "<type>anime</type>") || !strings.Contains(s, "Bathhouse") {
		t.Fatalf("nfo=%s", b)
	}
	if !strings.Contains(s, `type="tmdb-movie"`) {
		t.Fatalf("uniqueid=%s", b)
	}
	got, err := Get(cfg, "src", "129")
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "anime" || got.Title != "Spirited Away" || len(got.Seasons) != 0 {
		t.Fatalf("got=%+v", got)
	}
}

func TestFindDirRenamesLegacyUnderscore(t *testing.T) {
	dir := t.TempDir()
	root := Root(dir)
	old := filepath.Join(root, "[tmdb_tv-1396] Breaking Bad (2008)")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		DataDir:   dir,
		Providers: map[string]config.Provider{"tmdb_tv": {UniqueID: "tmdb-tv"}},
	}
	cand := match.Candidate{Provider: "tmdb_tv", ID: "1396", Title: "Breaking Bad", Year: "2008"}
	if err := Save(context.Background(), cfg, match.Job{Type: "tv"}, cand, false); err != nil {
		t.Fatal(err)
	}
	got, err := FindDir(cfg, root, "tmdb_tv", "1396")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "[tmdb-tv-1396] Breaking Bad (2008)" {
		t.Fatalf("dir=%q", got)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("legacy dir still present: %v", err)
	}
}

func TestFindDirRenamesLegacyMoviePrefix(t *testing.T) {
	dir := t.TempDir()
	root := Root(dir)
	old := filepath.Join(root, "[tmdb-129] Spirited Away (2001)")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		DataDir:   dir,
		Providers: map[string]config.Provider{"tmdb": {NFO: "movie", UniqueID: "tmdb-movie"}},
	}
	cand := match.Candidate{Provider: "tmdb", ID: "129", Title: "Spirited Away", Year: "2001"}
	if err := Save(context.Background(), cfg, match.Job{Type: "anime"}, cand, false); err != nil {
		t.Fatal(err)
	}
	got, err := FindDir(cfg, root, "tmdb", "129")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "[tmdb-movie-129] Spirited Away (2001)" {
		t.Fatalf("dir=%q", got)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("legacy dir still present: %v", err)
	}
}

func TestSaveSkipEpisodePosters(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-bytes"))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir, HTTP: config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000}}
	cand := match.Candidate{Provider: "tvmaze", ID: "139", Title: "Girls", Year: "2012", Poster: srv.URL + "/show.jpg"}
	job := match.Job{
		Type:       "tv",
		Status:     "matched",
		Match:      &cand,
		CatalogFor: "tvmaze:139",
		Catalog: []match.CatalogSeason{
			{
				Number: "1", Title: "Season 1", Poster: srv.URL + "/s1.jpg",
				Episodes: []match.CatalogEpisode{
					{Number: "1", Title: "Pilot", Poster: srv.URL + "/e1.jpg"},
				},
			},
		},
	}
	if err := Save(context.Background(), cfg, job, cand, true); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("poster gets=%d want 2 (show+season)", hits.Load())
	}
	show := filepath.Join(dir, "catalog", DirName(cfg, cand))
	if _, err := os.Stat(filepath.Join(show, "Season 01", "S01E01 Pilot.nfo")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(show, "poster.jpg")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(show, "Season 01", "poster.jpg")); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(filepath.Join(show, "Season 01"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		n := strings.ToLower(e.Name())
		if strings.HasPrefix(n, "s01e01") && !strings.HasSuffix(n, ".nfo") {
			t.Fatalf("episode poster written: %s", e.Name())
		}
	}
}
