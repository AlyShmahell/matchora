package match

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alyshmahell/matchora/lib/config"
)

func always504Config(url string) config.Config {
	return config.Config{
		HTTP: config.HTTP{
			TimeoutMS:         5000,
			Retries:           2,
			BackoffMS:         []int{1, 1},
			ProviderTimeoutMS: 200,
		},
		Match: config.Match{CooldownFails: 2, CooldownMS: 3600000},
		Providers: map[string]config.Provider{
			"slow": {
				Types:  []string{"tv", ""},
				Base:   url,
				URL:    "{base}/search/shows",
				Query:  map[string]string{"q": "{title}"},
				Items:  "$",
				Fields: map[string]string{"id": "show.id", "title": "show.name", "year": "show.premiered", "url": "show.url"},
			},
		},
	}
}

func TestCircuitSkipsAfterTwoFailedJobs(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	t.Cleanup(srv.Close)
	cfg := always504Config(srv.URL)
	cool := NewCircuit()
	ctx := WithCircuit(context.Background(), cool)
	httpc := newHTTP(cfg)
	job := Job{Title: "Girls"}
	for i := 0; i < 2; i++ {
		cands, errs, ok := collectProviders(ctx, cfg, httpc, job, true, false)
		if len(cands) != 0 || ok != 0 || len(errs) == 0 {
			t.Fatalf("fail %d: cands=%v ok=%d errs=%v", i, cands, ok, errs)
		}
	}
	hits := n.Load()
	if hits < 2 {
		t.Fatalf("hits after two jobs=%d", hits)
	}
	cands, errs, ok := collectProviders(ctx, cfg, httpc, job, true, false)
	if len(cands) != 0 || ok != 0 {
		t.Fatalf("cooled: cands=%v ok=%d", cands, ok)
	}
	if len(errs) != 1 || errs[0] != "slow: cooldown" {
		t.Fatalf("errs=%v", errs)
	}
	if n.Load() != hits {
		t.Fatalf("provider called during cooldown: before=%d after=%d", hits, n.Load())
	}
}

func TestCircuitSuccessResetsStreak(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := n.Add(1)
		if i == 1 {
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"score": 1, "show": map[string]any{"id": 1, "name": "Girls", "premiered": "2012", "url": "http://t"}},
		})
	}))
	t.Cleanup(srv.Close)
	cfg := always504Config(srv.URL)
	cfg.HTTP.Retries = 1
	cool := NewCircuit()
	ctx := WithCircuit(context.Background(), cool)
	httpc := newHTTP(cfg)
	job := Job{Title: "Girls"}
	_, errs, ok := collectProviders(ctx, cfg, httpc, job, true, false)
	if ok != 0 || len(errs) == 0 {
		t.Fatalf("first fail: ok=%d errs=%v", ok, errs)
	}
	cands, errs, ok := collectProviders(ctx, cfg, httpc, job, true, false)
	if ok != 1 || len(errs) != 0 || len(cands) != 1 {
		t.Fatalf("success: cands=%v ok=%d errs=%v", cands, ok, errs)
	}
	if cool.Cooling("slow") {
		t.Fatal("cooled after a success")
	}
}

func TestCircuitExpiresAfterTTL(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	t.Cleanup(srv.Close)
	cfg := always504Config(srv.URL)
	cfg.Match.CooldownMS = 30
	cfg.HTTP.Retries = 1
	cool := NewCircuit()
	ctx := WithCircuit(context.Background(), cool)
	httpc := newHTTP(cfg)
	job := Job{Title: "Girls"}
	for i := 0; i < 2; i++ {
		collectProviders(ctx, cfg, httpc, job, true, false)
	}
	if !cool.Cooling("slow") {
		t.Fatal("expected cooling")
	}
	hits := n.Load()
	collectProviders(ctx, cfg, httpc, job, true, false)
	if n.Load() != hits {
		t.Fatal("called during cooldown")
	}
	time.Sleep(40 * time.Millisecond)
	collectProviders(ctx, cfg, httpc, job, true, false)
	if n.Load() <= hits {
		t.Fatalf("expected call after ttl, hits=%d then %d", hits, n.Load())
	}
}

func TestCircuitIgnoresCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	t.Cleanup(srv.Close)
	cfg := always504Config(srv.URL)
	cfg.HTTP.Retries = 1
	cool := NewCircuit()
	ctx, cancel := context.WithCancel(WithCircuit(context.Background(), cool))
	cancel()
	collectProviders(ctx, cfg, newHTTP(cfg), Job{Title: "Girls"}, true, false)
	if cool.Cooling("slow") {
		t.Fatal("canceled ctx should not trip cooldown")
	}
	cool.Fail("slow", 2, time.Hour)
	cool.Fail("slow", 2, time.Hour)
	if !cool.Cooling("slow") {
		t.Fatal("two real fails should cool")
	}
}
