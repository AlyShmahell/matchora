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
	got := parseShows(raw, "Folder: 5 Centimeters Per Second (2007)\n", nil)
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
	got := parseShows(raw, "", nil)
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
	got := parseShows(raw, "", nil)
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
	got := parseShows(raw, "", nil)
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
	got := parseShows(raw, listing, nil)
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
	got := parseShows(raw, listing, nil)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Title != "Frieren" {
		t.Fatalf("title=%q", got[0].Title)
	}
}

func TestParseShowsStripsTrailingType(t *testing.T) {
	raw := []byte(`{"shows":[{"title":"Black Clover anime","year":"2019","type":"anime"}]}`)
	got := parseShows(raw, "", nil)
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
	got := parseShows(raw, listing, nil)
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
	got := parseShows(raw, listing, nil)
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
	got := parseShows(raw, "Folder: 29-sai\n", nil)
	if len(got) != 1 || got[0].Title != "29-sai" {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseShowsEmptyFallsBackToFolder(t *testing.T) {
	listing := "Folder: An Archdemon's Dilemma - How to Love Your Elf Bride\n  - Season 1/\n"
	got := parseShows([]byte(`{"shows":[]}`), listing, nil)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Title != "An Archdemon's Dilemma: How to Love Your Elf Bride" {
		t.Fatalf("title=%q", got[0].Title)
	}
}

func TestParseShowsBadJSONFallsBackToFolder(t *testing.T) {
	listing := "Folder: Frieren\n"
	got := parseShows([]byte("not json"), listing, nil)
	if len(got) != 1 || got[0].Title != "Frieren" {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseShowsEmptySkipsEpisodeFilenameHint(t *testing.T) {
	listing := "File: Show.S01E01.mkv\n"
	got := parseShows([]byte(`{"shows":[]}`), listing, nil)
	if len(got) != 0 {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseShowsNumericYear(t *testing.T) {
	listing := "Folder: Cowboy Bebop (1998)\n"
	got := parseShows([]byte(`{"shows":[{"title":"Cowboy Bebop","year":1998}]}`), listing, nil)
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
	got := parseShows([]byte(`{"shows":[{"title":"S1/","year":""}]}`), listing, nil)
	if len(got) != 1 || got[0].Title != "Japan Sinks" {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseShowsSeasonOnlyTreePrefersFolder(t *testing.T) {
	listing := "Folder: Solo Leveling\n  - Season 1/ (12 videos: S01E01.mkv)\n  - Season 2/ (5 videos: S02E01-You Aren't E-Rank, Are You.mkv, S02E03-Still a Long Way to Go.mkv)\n"
	got := parseShows([]byte(`{"shows":[{"title":"You Are A Long Way To Go","year":""}]}`), listing, nil)
	if len(got) != 1 || got[0].Title != "Solo Leveling" {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseShowsKeepsMixedSeasonAndMovie(t *testing.T) {
	listing := "Folder: KonoSuba\n  - Season 1/ (1 videos: S01E01.mkv)\n  - Legend of Crimson/ (1 videos: Legend of Crimson.mkv)\n"
	raw := []byte(`{"shows":[{"title":"KonoSuba","year":""},{"title":"Legend of Crimson","year":""}]}`)
	got := parseShows(raw, listing, nil)
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
	got := Group(context.Background(), cfg, "Folder: Frieren\n", nil)
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
	got := Group(context.Background(), cfg, "Folder: Frieren\n", nil)
	if len(got) != 1 || got[0].Title != "Frieren" {
		t.Fatalf("got=%+v", got)
	}
	if gotMax != float64(256) {
		t.Fatalf("max_tokens=%v", gotMax)
	}
}

func TestGroupSendsHigherMaxTokensWithFiles(t *testing.T) {
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
	got := Group(context.Background(), cfg, "Folder: Frieren\n", []JobFile{{Path: "/mnt/Frieren S01E01.mkv"}})
	if len(got) != 1 || got[0].Title != "Frieren" {
		t.Fatalf("got=%+v", got)
	}
	if gotMax != float64(2048) {
		t.Fatalf("max_tokens=%v", gotMax)
	}
}

func TestParseShowsKeepsListedFiles(t *testing.T) {
	ep := "/mnt/show/Season 1/Show S01E01.mkv"
	bare := "/mnt/show/Season 1/01.mkv"
	listing := "Folder: Show\n"
	raw := []byte(`{"shows":[{"title":"Show","year":"","files":[
		{"path":"` + ep + `","season":"1","episode":"1"},
		{"path":"` + bare + `","season":"","episode":""},
		{"path":"/other/invented.mkv","season":"9","episode":"9"}
	]}]}`)
	got := parseShows(raw, listing, []JobFile{{Path: ep}, {Path: bare}})
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if len(got[0].Files) != 2 {
		t.Fatalf("files=%+v", got[0].Files)
	}
	if got[0].Files[0].Path != ep || got[0].Files[0].Season != "1" || got[0].Files[0].Episode != "1" {
		t.Fatalf("e01 %#v", got[0].Files[0])
	}
	if got[0].Files[1].Path != bare || got[0].Files[1].Season != "" || got[0].Files[1].Episode != "" {
		t.Fatalf("bare %#v", got[0].Files[1])
	}
}

func TestParseShowsChatDownKeepsBlankFiles(t *testing.T) {
	ep := "/mnt/Frieren/Frieren S01E01.mkv"
	got := parseShows([]byte("not json"), "Folder: Frieren\n", []JobFile{{Path: ep, Season: "9"}})
	if len(got) != 1 || got[0].Title != "Frieren" {
		t.Fatalf("got=%+v", got)
	}
	if len(got[0].Files) != 1 || got[0].Files[0].Path != ep || got[0].Files[0].Season != "" {
		t.Fatalf("files=%+v", got[0].Files)
	}
}

func TestListingHintPrefersParentForSeasonFolder(t *testing.T) {
	listing := "Folder: Season 1\nParent: Frieren\n  - Frieren S01E01.mkv\n"
	if got := listingHint(listing); got != "Frieren" {
		t.Fatalf("hint=%q", got)
	}
}

func TestMergeJobsSameTitle(t *testing.T) {
	s1 := "/lib/Frieren/Season 1"
	s2 := "/lib/Frieren/Season 2"
	ep1 := s1 + "/Frieren S01E01.mkv"
	ep2 := s2 + "/Frieren S02E01.mkv"
	got := MergeJobs([]Job{
		{Title: "Frieren", Path: s1, Files: []JobFile{{Path: ep1, Season: "1", Episode: "1"}}},
		{Title: "Frieren", Path: s2, Files: []JobFile{{Path: ep2, Season: "2", Episode: "1"}}},
		{Title: "A Silent Voice", Year: "2016", Path: "/lib/A Silent Voice (2016).mkv"},
	})
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Title != "Frieren" || got[0].Path != "/lib/Frieren" {
		t.Fatalf("show=%+v", got[0])
	}
	if len(got[0].Files) != 2 {
		t.Fatalf("files=%+v", got[0].Files)
	}
	if got[1].Title != "A Silent Voice" {
		t.Fatalf("movie=%+v", got[1])
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
