package match

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alyshmahell/matchora/lib/config"
)

func TestWaitLogConcurrentStartEnd(t *testing.T) {
	log := NewWaitLog(500)
	var wg sync.WaitGroup
	n := 40
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			id := log.WaitStart("j", "Title", "p")
			log.WaitEnd(id, "")
		}(i)
	}
	wg.Wait()
	got := log.Snapshot()
	if len(got) != n {
		t.Fatalf("len=%d", len(got))
	}
	for _, w := range got {
		if w.Until == nil {
			t.Fatalf("open wait %+v", w)
		}
		if w.Name != "p" {
			t.Fatalf("name=%q", w.Name)
		}
	}
}

func TestRunWithReportsRunningProvider(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unlock)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"score": 1, "show": map[string]any{"id": 1, "name": "Girls", "premiered": "2012", "url": "http://t"}},
		})
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		HTTP:  config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 2000},
		Match: config.Match{MinScore: 0.72, MinMargin: 0.04},
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
	log := NewWaitLog(500)
	done := make(chan []Job, 1)
	go func() {
		done <- RunWith(context.Background(), cfg, []Job{{ID: "a", Title: "Girls"}}, log)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not start")
	}
	var open *Wait
	for _, w := range log.Snapshot() {
		if w.Name == "tvmaze" && w.Until == nil {
			cp := w
			open = &cp
			break
		}
	}
	if open == nil {
		t.Fatalf("expected running wait, got %+v", log.Snapshot())
	}
	unlock()
	select {
	case jobs := <-done:
		if len(jobs) != 1 || jobs[0].Status != "matched" {
			t.Fatalf("jobs=%+v", jobs)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run hung")
	}
	found := false
	for _, w := range log.Snapshot() {
		if w.Name == "tvmaze" && w.Until != nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected closed wait, got %+v", log.Snapshot())
	}
}

func TestFetchSkipsProviderPace(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg"))
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		HTTP: config.HTTP{TimeoutMS: 2000, Retries: 1, ProviderTimeoutMS: 1000},
		Providers: map[string]config.Provider{
			"tvmaze": {MinIntervalMS: 5000},
		},
	}
	if err := paceProvider(context.Background(), "tvmaze", 5000); err != nil {
		t.Fatal(err)
	}
	log := NewWaitLog(500)
	ctx := WithReporter(context.Background(), log)
	ctx = WithJob(ctx, Job{ID: "a", Title: "Girls"})
	start := time.Now()
	_, _, err := Fetch(ctx, cfg, srv.URL+"/static/poster.jpg", "tvmaze")
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) >= 2*time.Second {
		t.Fatalf("paced image fetch: %s", time.Since(start))
	}
	if hits != 1 {
		t.Fatalf("hits=%d", hits)
	}
	got := log.Snapshot()
	if len(got) != 1 || got[0].Name != "tvmaze/poster" || got[0].Until == nil {
		t.Fatalf("waits=%+v", got)
	}
}

func TestFetchReportsPoster(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg"))
	}))
	t.Cleanup(srv.Close)
	log := NewWaitLog(500)
	ctx := WithReporter(context.Background(), log)
	ctx = WithJob(ctx, Job{ID: "a", Title: "Girls"})
	cfg := config.Config{HTTP: config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000}}
	_, _, err := Fetch(ctx, cfg, srv.URL, "tvmaze")
	if err != nil {
		t.Fatal(err)
	}
	got := log.Snapshot()
	if len(got) != 1 || got[0].Name != "tvmaze/poster" || got[0].Title != "Girls" || got[0].Until == nil {
		t.Fatalf("waits=%+v", got)
	}
}

func TestFillCatalogReportsWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/seasons") {
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": 10, "number": 1, "name": "Season 1"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		HTTP:      config.HTTP{TimeoutMS: 5000, Retries: 1, ProviderTimeoutMS: 1000},
		Providers: map[string]config.Provider{"tvmaze": tvmazeCatalogSpec(srv.URL)},
	}
	log := NewWaitLog(500)
	cand := Candidate{Provider: "tvmaze", ID: "139", Title: "Girls"}
	job := Job{ID: "a", Title: "Girls", Status: "matched", Match: &cand}
	ctx := WithReporter(context.Background(), log)
	ctx = WithJob(ctx, job)
	out := FillCatalog(ctx, cfg, job)
	if len(out.Catalog) != 1 {
		t.Fatalf("catalog=%+v", out.Catalog)
	}
	found := false
	for _, w := range log.Snapshot() {
		if w.Name == "tvmaze/catalog" && w.Until != nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected catalog wait, got %+v", log.Snapshot())
	}
}
