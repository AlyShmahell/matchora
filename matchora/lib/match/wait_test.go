package match

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alyshmahell/matchora/lib/config"
)

func TestWaitLogConcurrentStartEnd(t *testing.T) {
	log := &WaitLog{}
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
	log := &WaitLog{}
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
