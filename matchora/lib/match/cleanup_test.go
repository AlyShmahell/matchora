package match

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alyshmahell/matchora/lib/config"
)

func TestParseShowsSchemaTypeAndYear(t *testing.T) {
	raw := []byte(`{"shows":[{"title":"5 Centimeters Per Second (2007)","year":"2007","type":"anime|tv|movie"}]}`)
	got := parseShows(raw, "Folder: 5 Centimeters Per Second (2007)\n")
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if stringsContainsPipe(got[0].Type) {
		t.Fatalf("type still schema: %q", got[0].Type)
	}
	if got[0].Title != "5 Centimeters Per Second" {
		t.Fatalf("title=%q", got[0].Title)
	}
	if got[0].Type != "" {
		t.Fatalf("type=%q", got[0].Type)
	}
	if got[0].Year != "2007" {
		t.Fatalf("year=%q", got[0].Year)
	}
}

func TestParseShowsDropsType(t *testing.T) {
	raw := []byte(`{"shows":[
		{"title":"Girls","year":"2012","type":"tv"},
		{"title":"Cowboy Bebop","year":"1998","type":"anime"},
		{"title":"Legend of Crimson","year":"2019","type":"movie"}
	]}`)
	got := parseShows(raw, "")
	if len(got) != 3 {
		t.Fatalf("len=%d", len(got))
	}
	for i, g := range got {
		if g.Type != "" {
			t.Fatalf("%d type=%q", i, g.Type)
		}
	}
}

func TestParseShowsDropsSeasonEpisodeAndInvalidYear(t *testing.T) {
	raw := []byte(`{"shows":[
		{"title":"Aldnoah Zero","year":"Season 1","type":"anime","season":"1","episode":"1"},
		{"title":"Aldnoah Zero","year":"Season 2","type":"anime","season":"2","episode":"1"}
	]}`)
	got := parseShows(raw, "")
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Title != "Aldnoah Zero" {
		t.Fatalf("title=%q", got[0].Title)
	}
	if got[0].Year != "" {
		t.Fatalf("year=%q", got[0].Year)
	}
	if got[0].Season != "" || got[0].Episode != "" {
		t.Fatalf("season=%q episode=%q", got[0].Season, got[0].Episode)
	}
}

func TestParseShowsDropsEpisodeFilenamesAndTags(t *testing.T) {
	raw := []byte(`{"shows":[
		{"title":"[Tag] 29-sai","year":"2024","type":"anime"},
		{"title":"Black Clover S01E01.mkv","year":"2019","type":"anime"},
		{"title":"Black Clover","year":"2019","type":"anime","season":"S1","episode":"01"}
	]}`)
	got := parseShows(raw, "")
	if len(got) != 2 {
		t.Fatalf("len=%d got=%v", len(got), got)
	}
	if got[0].Title != "29-sai" {
		t.Fatalf("title0=%q", got[0].Title)
	}
	if got[1].Title != "Black Clover" || got[1].Season != "" {
		t.Fatalf("got1=%+v", got[1])
	}
}

func TestParseShowsReplacesTagWithFolder(t *testing.T) {
	listing := "Folder: Am I Actually the Strongest\n  - [Tag] Jitsu wa Ore - S01E01.mkv\n"
	raw := []byte(`{"shows":[{"title":"Tag","year":"2019","type":"anime"}]}`)
	got := parseShows(raw, listing)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Title != "Am I Actually the Strongest" {
		t.Fatalf("title=%q", got[0].Title)
	}
}

func TestParseShowsStripsTagPrefix(t *testing.T) {
	listing := "Folder: Frieren\n  - Season 1/ (1 videos: [TagA] Frieren - S01E01.mkv)\n  - [Tag] Frieren - S01E01.mkv\n"
	raw := []byte(`{"shows":[{"title":"Tag Frieren","year":"2023","type":"anime"}]}`)
	got := parseShows(raw, listing)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Title != "Frieren" {
		t.Fatalf("title=%q", got[0].Title)
	}
}

func TestParseShowsStripsTrailingType(t *testing.T) {
	raw := []byte(`{"shows":[{"title":"Black Clover anime","year":"2019","type":"anime"}]}`)
	got := parseShows(raw, "")
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Title != "Black Clover" {
		t.Fatalf("title=%q", got[0].Title)
	}
}

func TestParseShowsDropsInventedYearAndSpacedDash(t *testing.T) {
	listing := "Folder: An Archdemon's Dilemma - How to Love Your Elf Bride\n  - Season 1/\n"
	raw := []byte(`{"shows":[{"title":"An Archdemon's Dilemma - How to Love Your Elf Bride","year":"2019"}]}`)
	got := parseShows(raw, listing)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Title != "An Archdemon's Dilemma: How to Love Your Elf Bride" {
		t.Fatalf("title=%q", got[0].Title)
	}
	if got[0].Year != "" {
		t.Fatalf("year=%q", got[0].Year)
	}
}

func TestParseShowsKeepsYearInListing(t *testing.T) {
	listing := "File: Cowboy.Bebop.1998.1080p.BluRay.x264-Tag.mkv\n"
	raw := []byte(`{"shows":[{"title":"Cowboy Bebop","year":"1998"}]}`)
	got := parseShows(raw, listing)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Title != "Cowboy Bebop" {
		t.Fatalf("title=%q", got[0].Title)
	}
	if got[0].Year != "1998" {
		t.Fatalf("year=%q", got[0].Year)
	}
}

func TestParseShowsKeepsWordHyphen(t *testing.T) {
	raw := []byte(`{"shows":[{"title":"29-sai","year":""}]}`)
	got := parseShows(raw, "Folder: 29-sai\n")
	if len(got) != 1 || got[0].Title != "29-sai" {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseShowsEmptyFallsBackToFolder(t *testing.T) {
	listing := "Folder: An Archdemon's Dilemma - How to Love Your Elf Bride\n  - Season 1/\n"
	got := parseShows([]byte(`{"shows":[]}`), listing)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Title != "An Archdemon's Dilemma: How to Love Your Elf Bride" {
		t.Fatalf("title=%q", got[0].Title)
	}
}

func TestParseShowsBadJSONFallsBackToFolder(t *testing.T) {
	listing := "Folder: Frieren\n"
	got := parseShows([]byte("not json"), listing)
	if len(got) != 1 || got[0].Title != "Frieren" {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseShowsEmptySkipsEpisodeFilenameHint(t *testing.T) {
	listing := "File: Show.S01E01.mkv\n"
	got := parseShows([]byte(`{"shows":[]}`), listing)
	if len(got) != 0 {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseShowsNumericYear(t *testing.T) {
	listing := "Folder: Cowboy Bebop (1998)\n"
	got := parseShows([]byte(`{"shows":[{"title":"Cowboy Bebop","year":1998}]}`), listing)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Title != "Cowboy Bebop" {
		t.Fatalf("title=%q", got[0].Title)
	}
	if got[0].Year != "1998" {
		t.Fatalf("year=%q", got[0].Year)
	}
}

func TestParseShowsReplacesSeasonTitleWithFolder(t *testing.T) {
	listing := "Folder: Japan Sinks\n  - Season 1/ (10 videos: [Tag] Nihon Chinbotsu - S01E01.mkv, ...)\n"
	got := parseShows([]byte(`{"shows":[{"title":"S1/","year":""}]}`), listing)
	if len(got) != 1 || got[0].Title != "Japan Sinks" {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseShowsSeasonOnlyTreePrefersFolder(t *testing.T) {
	listing := "Folder: Solo Leveling\n  - Season 1/ (12 videos: S01E01.mkv)\n  - Season 2/ (5 videos: S02E01-You Aren't E-Rank, Are You.mkv, S02E03-Still a Long Way to Go.mkv)\n"
	got := parseShows([]byte(`{"shows":[{"title":"You Are A Long Way To Go","year":""}]}`), listing)
	if len(got) != 1 || got[0].Title != "Solo Leveling" {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseShowsKeepsMixedSeasonAndMovie(t *testing.T) {
	listing := "Folder: KonoSuba\n  - Season 1/ (1 videos: S01E01.mkv)\n  - Legend of Crimson/ (1 videos: Legend of Crimson.mkv)\n"
	raw := []byte(`{"shows":[{"title":"KonoSuba","year":""},{"title":"Legend of Crimson","year":""}]}`)
	got := parseShows(raw, listing)
	if len(got) != 2 {
		t.Fatalf("len=%d got=%+v", len(got), got)
	}
	if got[0].Title != "KonoSuba" || got[1].Title != "Legend of Crimson" {
		t.Fatalf("got=%+v", got)
	}
}

func TestGroupFallsBackWhenChatFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{Llama: config.Llama{LLMBaseURL: srv.URL + "/v1"}}
	got := Group(context.Background(), cfg, "Folder: Frieren\n")
	if len(got) != 1 || got[0].Title != "Frieren" {
		t.Fatalf("got=%+v", got)
	}
}

func TestGroupSendsMaxTokens(t *testing.T) {
	var gotMax any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		gotMax = req["max_tokens"]
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{"content": `{"shows":[{"title":"Frieren","year":""}]}`}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{Llama: config.Llama{LLMBaseURL: srv.URL + "/v1"}}
	got := Group(context.Background(), cfg, "Folder: Frieren\n")
	if len(got) != 1 || got[0].Title != "Frieren" {
		t.Fatalf("got=%+v", got)
	}
	if gotMax != float64(256) {
		t.Fatalf("max_tokens=%v", gotMax)
	}
}

func stringsContainsPipe(s string) bool {
	for _, r := range s {
		if r == '|' {
			return true
		}
	}
	return false
}
