package match

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

func TestRankEmbedUsesMaxOfTitleAndSynopsis(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Input string `json:"input"`
		}
		_ = json.Unmarshal(body, &req)
		vec := []float64{0, 1}
		in := strings.ToLower(req.Input)
		if strings.Contains(in, "bofuri") || strings.Contains(in, "pain") || strings.Contains(in, "defense") {
			vec = []float64{1, 0}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"embedding": vec}},
		})
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{Llama: config.Llama{BaseURL: srv.URL + "/v1"}}
	httpc := newHTTP(cfg)

	syn, err := rankEmbed(context.Background(), cfg, httpc, "BOFURI: I Don't Want to Get Hurt", []Candidate{
		{Title: "Itai no wa Iya nano de", Year: "2020", Synopsis: "She dislikes pain and maxes defense."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(syn) != 1 || syn[0].Score < 0.99 {
		t.Fatalf("synopsis score=%v", syn)
	}

	title, err := rankEmbed(context.Background(), cfg, httpc, "BOFURI", []Candidate{
		{Title: "BOFURI", Year: "2020", Synopsis: "unrelated camping trip"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(title) != 1 || title[0].Score < 0.99 {
		t.Fatalf("title score=%v", title)
	}
}
