package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/shows", func(w http.ResponseWriter, r *http.Request) {
		q := strings.ToLower(r.URL.Query().Get("q"))
		hits := []any{}
		if strings.Contains(q, "girls") {
			hits = append(hits, map[string]any{
				"score": 1,
				"show": map[string]any{
					"id":        139,
					"name":      "Girls",
					"premiered": "2012-04-15",
					"url":       "https://www.tvmaze.com/shows/139/girls",
					"summary":   "<p>Four friends in New York.</p>",
					"image":     map[string]any{"medium": "https://static.tvmaze.com/uploads/images/medium_portrait/31/78286.jpg"},
				},
			})
		}
		writeJSON(w, hits)
	})
	mux.HandleFunc("GET /shows/{id}/episodebynumber", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id":      1,
			"name":    "Pilot",
			"url":     "https://www.tvmaze.com/episodes/1/girls-1x01-pilot",
			"airdate": "2012-04-15",
		})
	})
	mux.HandleFunc("GET /shows/{id}/seasons", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []any{
			map[string]any{
				"id":           1,
				"number":       1,
				"name":         "",
				"summary":      "<p>Hannah and her friends.</p>",
				"premiereDate": "2012-04-15",
				"image":        map[string]any{"medium": "https://static.tvmaze.com/uploads/images/medium_portrait/31/78286.jpg"},
			},
		})
	})
	mux.HandleFunc("GET /shows/{id}/episodes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []any{
			map[string]any{
				"id":      1,
				"number":  1,
				"season":  1,
				"name":    "Pilot",
				"summary": "<p>Hannah returns to New York.</p>",
				"airdate": "2012-04-15",
				"image":   map[string]any{"medium": "https://static.tvmaze.com/uploads/images/medium_landscape/1/4388.jpg"},
			},
		})
	})
	mux.HandleFunc("GET /anime", func(w http.ResponseWriter, r *http.Request) {
		q := strings.ToLower(r.URL.Query().Get("q"))
		data := []any{}
		if strings.Contains(q, "bebop") || strings.Contains(q, "cowboy") {
			data = append(data, map[string]any{
				"mal_id":    1,
				"title":     "Cowboy Bebop",
				"year":      1998,
				"url":       "https://myanimelist.net/anime/1",
				"synopsis":  "The ragtag crew of the spaceship Bebop.",
				"images":    map[string]any{"jpg": map[string]any{"image_url": "https://cdn.myanimelist.net/images/anime/4/19644.jpg"}},
			})
		}
		writeJSON(w, map[string]any{"data": data})
	})
	addr := ":8080"
	if v := os.Getenv("STUB_ADDR"); v != "" {
		addr = v
	}
	http.ListenAndServe(addr, mux)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
