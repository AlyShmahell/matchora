package ingest

import (
	"context"
	"strings"
	"testing"

	"github.com/alyshmahell/matchora/lib/config"
)

func TestParseExactHeaders(t *testing.T) {
	rows, err := Parse(context.Background(), config.Config{Group: config.Group{SeqThreshold: 0.72}}, strings.NewReader("title,year,type,season,episode\nGirls,2012,tv,1,1\n"), "titles.csv", "text/csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Title != "Girls" || rows[0].Year != "2012" || rows[0].Type != "tv" {
		t.Fatalf("rows=%v", rows)
	}
}

func TestParseAliasHeaders(t *testing.T) {
	cfg := config.Config{
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

func TestParseSeqMapsCloseHeader(t *testing.T) {
	csv := "titles,year,type\nGirls,2012,tv\n"
	rows, err := Parse(context.Background(), config.Config{Group: config.Group{SeqThreshold: 0.72}}, strings.NewReader(csv), "titles.csv", "text/csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Title != "Girls" || rows[0].Year != "2012" {
		t.Fatalf("rows=%v", rows)
	}
}

func TestParseMissingTitle(t *testing.T) {
	csv := "name,year,media_type\nGirls,2012,episode\n"
	_, err := Parse(context.Background(), config.Config{Group: config.Group{SeqThreshold: 0.72}}, strings.NewReader(csv), "titles.csv", "text/csv")
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseEmpty(t *testing.T) {
	if _, err := Parse(context.Background(), config.Config{}, strings.NewReader("  "), "x.csv", "text/csv"); err == nil {
		t.Fatal("expected empty payload")
	}
}
