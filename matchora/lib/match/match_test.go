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
			map[string]any{"score": 1, "show": map[string]any{"id": 1, "name": "Dark Matter", "premiered": "2015", "url": "http://t"}},
			map[string]any{"score": 1, "show": map[string]any{"id": 2, "name": "Dark Matter", "premiered": "2024", "url": "http://t2"}},
		})
	}))
	t.Cleanup(tvmaze.Close)
	jikan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jk.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"mal_id": 1, "title": "Dark Matter", "year": 2015, "url": "http://j"}},
		})
	}))
	t.Cleanup(jikan.Close)
	cfg := deferPairConfig(tvmaze.URL, jikan.URL)
	job := runOne(context.Background(), cfg, newHTTP(cfg), Job{Title: "Dark Matter"})
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
		HTTP: config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000},
		Match: config.Match{
			MinScore: 0.72, MinMargin: 0.04,
			PlotStop: testPlotStop,
		},
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
	if autoMatch(cfg, []Candidate{{Score: 0.05}}, false) {
		t.Fatal("weak solo without prefer should not match")
	}
	if !autoMatch(cfg, []Candidate{{Score: 0.05}}, true) {
		t.Fatal("prefer-filtered solo with plot score should match")
	}
	if autoMatch(cfg, []Candidate{{Score: 0}}, true) {
		t.Fatal("prefer-filtered solo with score 0 should not match")
	}
	if !autoMatch(cfg, []Candidate{{Jaccard: 0.72, Score: 0.50}}, false) {
		t.Fatal("jaccard 0.72 should match on min_score path")
	}
	if autoMatch(cfg, []Candidate{{Jaccard: 0.80, Score: 0.80}, {Jaccard: 0.79, Score: 0.79}}, false) {
		t.Fatal("two close high scores should stay manual")
	}
	if autoMatch(cfg, []Candidate{{Score: 0.50, Jaccard: 0.50}, {Score: 0.10, Jaccard: 0.10}}, false) {
		t.Fatal("two candidates still need min_score")
	}
	if !autoMatch(cfg, []Candidate{{Score: 0.33, QueryCov: 1}, {Score: 0.25, QueryCov: 1}}, true) {
		t.Fatal("prefer + full query coverage + margin should match")
	}
}

func TestPreferCandidatesUntypedUsesAnime(t *testing.T) {
	cfg := config.Config{Match: config.Match{Prefer: map[string]map[string]string{
		"anime": {"language": "Japanese", "kind": "Animation"},
	}}}
	cands := []Candidate{
		{Title: "Arifureta Kiseki", Attrs: map[string]string{"language": "Japanese", "kind": "Scripted"}},
		{Title: "Arifureta: From Commonplace to World's Strongest", Attrs: map[string]string{"language": "Japanese", "kind": "Animation"}},
	}
	got := preferCandidates(cfg, "", cands)
	if len(got) != 1 || got[0].Title != "Arifureta: From Commonplace to World's Strongest" {
		t.Fatalf("got=%+v", got)
	}
}

func animeScanConfig(tvURL, jkURL string) config.Config {
	cfg := deferPairConfig(tvURL, jkURL)
	cfg.Match.SoloMinScore = 0.01
	cfg.Match.Prefer = map[string]map[string]string{
		"anime": {"language": "Japanese", "kind": "Animation"},
	}
	tv := cfg.Providers["tvmaze"]
	tv.Fields["kind"] = "show.type"
	tv.Fields["language"] = "show.language"
	cfg.Providers["tvmaze"] = tv
	jk := cfg.Providers["jikan"]
	jk.Fields["title_en"] = "title_english"
	jk.Attrs = map[string]string{"language": "Japanese", "kind": "Animation"}
	cfg.Providers["jikan"] = jk
	return cfg
}

func TestRunOneArifuretaPrefersMainSeries(t *testing.T) {
	var jk atomic.Int32
	tvmaze := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"show": map[string]any{"id": 1, "name": "Arifureta Kiseki", "premiered": "2008", "url": "http://t", "type": "Scripted", "language": "Japanese"}},
			map[string]any{"show": map[string]any{"id": 2, "name": "Arifureta: From Commonplace to World's Strongest", "premiered": "2019", "url": "http://t2", "type": "Animation", "language": "Japanese"}},
		})
	}))
	t.Cleanup(tvmaze.Close)
	jikan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jk.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"mal_id": 1, "title": "Arifureta: From Commonplace to World's Strongest", "year": 2019, "url": "http://j"}},
		})
	}))
	t.Cleanup(jikan.Close)
	cfg := animeScanConfig(tvmaze.URL, jikan.URL)
	job := runOne(context.Background(), cfg, newHTTP(cfg), Job{Title: "Arifureta"})
	if job.Status != "matched" || job.Match == nil || job.Match.Title != "Arifureta: From Commonplace to World's Strongest" {
		t.Fatalf("status=%s match=%+v candidates=%+v", job.Status, job.Match, job.Candidates)
	}
}

func TestRunOneErasedCallsJikan(t *testing.T) {
	var jk atomic.Int32
	tvmaze := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"show": map[string]any{"id": 1, "name": "Crashed", "premiered": "2017", "url": "http://t", "type": "Variety", "language": "English"}},
			map[string]any{"show": map[string]any{"id": 2, "name": "Epithet Erased", "premiered": "2021", "url": "http://t2", "type": "Animation", "language": "English"}},
		})
	}))
	t.Cleanup(tvmaze.Close)
	jikan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jk.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"mal_id": 31098, "title": "Boku Dake ga Inai Machi", "title_english": "Erased", "year": 2016, "url": "http://j"}},
		})
	}))
	t.Cleanup(jikan.Close)
	cfg := animeScanConfig(tvmaze.URL, jikan.URL)
	job := runOne(context.Background(), cfg, newHTTP(cfg), Job{Title: "Erased"})
	if jk.Load() != 1 {
		t.Fatalf("jikan hits=%d", jk.Load())
	}
	if job.Status != "matched" || job.Match == nil || job.Match.Title != "Boku Dake ga Inai Machi" {
		t.Fatalf("status=%s match=%+v candidates=%+v", job.Status, job.Match, job.Candidates)
	}
}

func TestRunOneFrierenBeatsFrieden(t *testing.T) {
	var jk atomic.Int32
	tvmaze := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"show": map[string]any{"id": 1, "name": "Frieden", "premiered": "2020", "url": "http://t", "type": "Scripted", "language": "German"}},
			map[string]any{"show": map[string]any{"id": 2, "name": "Frieren: Beyond Journey's End", "premiered": "2023", "url": "http://t2", "type": "Animation", "language": "Japanese"}},
			map[string]any{"show": map[string]any{"id": 3, "name": "Sousou no Frieren", "premiered": "2023", "url": "http://t3", "type": "Animation", "language": "Japanese"}},
		})
	}))
	t.Cleanup(tvmaze.Close)
	jikan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jk.Add(1)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(jikan.Close)
	cfg := animeScanConfig(tvmaze.URL, jikan.URL)
	job := runOne(context.Background(), cfg, newHTTP(cfg), Job{Title: "Frieren"})
	if job.Match == nil || job.Match.Title == "Frieden" {
		t.Fatalf("status=%s match=%+v candidates=%+v", job.Status, job.Match, job.Candidates)
	}
	if job.Status != "matched" {
		t.Fatalf("status=%s match=%+v", job.Status, job.Match)
	}
	if jk.Load() != 0 {
		t.Fatalf("jikan hits=%d", jk.Load())
	}
}

func TestRunOneScarletBondCallsJikan(t *testing.T) {
	var jk atomic.Int32
	tvmaze := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"show": map[string]any{"id": 1, "name": "Beauty's Bone, Scarlet Sleeves", "premiered": "2026", "url": "http://t", "type": "Scripted", "language": "Chinese"}},
		})
	}))
	t.Cleanup(tvmaze.Close)
	jikan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jk.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{
				"mal_id":        10218,
				"title":         "Tensei shitara Slime Datta Ken Movie: Guren no Kizuna-hen",
				"title_english": "That Time I Got Reincarnated as a Slime the Movie: Scarlet Bond",
				"year":          2022,
				"url":           "http://j",
			}},
		})
	}))
	t.Cleanup(jikan.Close)
	cfg := animeScanConfig(tvmaze.URL, jikan.URL)
	job := runOne(context.Background(), cfg, newHTTP(cfg), Job{
		Title:  "Scarlet Bond",
		Parent: "That Time I Got Reincarnated as a Slime",
	})
	if jk.Load() != 1 {
		t.Fatalf("jikan hits=%d status=%s match=%+v", jk.Load(), job.Status, job.Match)
	}
	if job.Status != "matched" || job.Match == nil || job.Match.Provider != "jikan" {
		t.Fatalf("status=%s match=%+v candidates=%+v", job.Status, job.Match, job.Candidates)
	}
}

func TestRunOneSinbadPrefersMagi(t *testing.T) {
	var jk atomic.Int32
	tvmaze := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"show": map[string]any{"id": 1, "name": "The Adventures of Sinbad", "premiered": "1996", "url": "http://t", "type": "Scripted", "language": "English"}},
			map[string]any{"show": map[string]any{"id": 2, "name": "Magi: Sinbad no Bouken", "premiered": "2016", "url": "http://t2", "type": "Animation", "language": "Japanese"}},
		})
	}))
	t.Cleanup(tvmaze.Close)
	jikan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jk.Add(1)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(jikan.Close)
	cfg := animeScanConfig(tvmaze.URL, jikan.URL)
	job := runOne(context.Background(), cfg, newHTTP(cfg), Job{
		Title:  "adventure of sinbad",
		Parent: "Magi The Labyrinth of Magic",
	})
	if job.Status != "matched" || job.Match == nil || job.Match.Title != "Magi: Sinbad no Bouken" {
		t.Fatalf("status=%s match=%+v candidates=%+v", job.Status, job.Match, job.Candidates)
	}
	if jk.Load() != 0 {
		t.Fatalf("jikan hits=%d", jk.Load())
	}
}
