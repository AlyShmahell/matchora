package ingest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alyshmahell/matchora/lib/config"
)

func TestParseExactHeadersSkipsChat(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "unused", 500)
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		Llama: config.Llama{LLMBaseURL: srv.URL + "/v1"},
	}
	rows, err := Parse(context.Background(), cfg, strings.NewReader("title,year,type,season,episode\nGirls,2012,tv,1,1\n"), "titles.csv", "text/csv")
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("exact headers should skip instruct")
	}
	if len(rows) != 1 || rows[0].Title != "Girls" || rows[0].Year != "2012" || rows[0].Type != "tv" {
		t.Fatalf("rows=%v", rows)
	}
}

func TestParseAliasHeadersWithoutChat(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "unused", 500)
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		Llama: config.Llama{LLMBaseURL: srv.URL + "/v1"},
		Ingest: config.Ingest{
			Aliases: map[string]string{"mediatype": "type", "seasonnumber": "season", "episodenumber": "episode"},
			Types:   map[string]string{"episode": "tv"},
		},
	}
	csv := "title,year,media_type,season_number,episode_number\nGirls,2012,episode,1,1\n"
	rows, err := Parse(context.Background(), cfg, strings.NewReader(csv), "titles.csv", "text/csv")
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("alias headers should skip instruct")
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%v", rows)
	}
	got := rows[0]
	if got.Title != "Girls" || got.Type != "tv" || got.Season != "1" || got.Episode != "1" {
		t.Fatalf("row=%+v", got)
	}
}

func TestParseJSONTypeAlias(t *testing.T) {
	cfg := config.Config{Ingest: config.Ingest{Types: map[string]string{"episode": "tv", "film": "movie"}}}
	rows, err := Parse(context.Background(), cfg, strings.NewReader(`[{"title":"Girls","type":"episode"}]`), "titles.json", "application/json")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Type != "tv" {
		t.Fatalf("rows=%v", rows)
	}
}

func TestParseLLMMapsMissingTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(strings.ToLower(string(body)), "name") {
			t.Errorf("user message missing headers: %s", body)
		}
		writeChat(w, map[string]any{"columns": map[string]string{"title": "name", "year": "aired", "type": "kind"}})
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		Llama: config.Llama{LLMBaseURL: srv.URL + "/v1"},
		Ingest: config.Ingest{
			SampleRows: 2,
			Types:      map[string]string{"series": "tv"},
		},
	}
	csv := "name,aired,kind\nGirls,2012,series\n"
	rows, err := Parse(context.Background(), cfg, strings.NewReader(csv), "titles.csv", "text/csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Title != "Girls" || rows[0].Year != "2012" || rows[0].Type != "tv" {
		t.Fatalf("rows=%v", rows)
	}
}

func TestParseChatFailureFallsBackToAliases(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "down", 500)
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		Llama: config.Llama{LLMBaseURL: srv.URL + "/v1"},
		Ingest: config.Ingest{
			Aliases: map[string]string{"mediatype": "type"},
			Types:   map[string]string{"episode": "tv"},
		},
	}
	csv := "name,year,media_type\nGirls,2012,episode\n"
	_, err := Parse(context.Background(), cfg, strings.NewReader(csv), "titles.csv", "text/csv")
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("err=%v", err)
	}
	if !called {
		t.Fatal("missing title should ask instruct")
	}
}

func TestParseRejectsUnknownLLMHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeChat(w, map[string]any{"columns": map[string]string{"title": "missing"}})
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{Llama: config.Llama{LLMBaseURL: srv.URL + "/v1"}}
	_, err := Parse(context.Background(), cfg, strings.NewReader("name,year\nGirls,2012\n"), "titles.csv", "text/csv")
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseEmpty(t *testing.T) {
	if _, err := Parse(context.Background(), config.Config{}, strings.NewReader("  "), "x.csv", "text/csv"); err == nil {
		t.Fatal("expected empty payload")
	}
}

func writeChat(w http.ResponseWriter, content any) {
	raw, _ := json.Marshal(content)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{
			map[string]any{"message": map[string]string{"content": string(raw)}},
		},
	})
}
