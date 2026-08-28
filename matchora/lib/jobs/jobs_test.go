package jobs

import (
	"testing"

	"github.com/alyshmahell/matchora/lib/match"
)

func TestStoreClearEmptiesList(t *testing.T) {
	store := New(t.TempDir())
	if err := store.ReplaceAll([]match.Job{
		{ID: "a", Title: "Girls", Status: "matched"},
		{ID: "b", Title: "Bebop", Status: "pending"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	got, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("len=%d", len(got))
	}
}

func TestStoreCatalogNilVsEmpty(t *testing.T) {
	store := New(t.TempDir())
	empty := []match.CatalogSeason{}
	if err := store.ReplaceAll([]match.Job{
		{ID: "nilcat", Title: "A", Status: "matched"},
		{ID: "empty", Title: "B", Status: "matched", Catalog: empty, CatalogFor: "tvmaze:1"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	var nilJob, emptyJob match.Job
	for _, j := range got {
		if j.ID == "nilcat" {
			nilJob = j
		}
		if j.ID == "empty" {
			emptyJob = j
		}
	}
	if nilJob.Catalog != nil {
		t.Fatalf("nil catalog=%+v", nilJob.Catalog)
	}
	if emptyJob.Catalog == nil {
		t.Fatal("empty catalog became nil")
	}
	if len(emptyJob.Catalog) != 0 {
		t.Fatalf("empty len=%d", len(emptyJob.Catalog))
	}
}

func TestMarkPendingClearsCatalog(t *testing.T) {
	store := New(t.TempDir())
	if err := store.ReplaceAll([]match.Job{
		{
			ID:         "a",
			Title:      "Girls",
			Status:     "matched",
			Catalog:    []match.CatalogSeason{{Title: "Season 1"}},
			CatalogFor: "tvmaze:139",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkPending(); err != nil {
		t.Fatal(err)
	}
	got, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Status != "pending" {
		t.Fatalf("status=%s", got[0].Status)
	}
	if got[0].Catalog != nil || got[0].CatalogFor != "" {
		t.Fatalf("catalog=%+v for=%q", got[0].Catalog, got[0].CatalogFor)
	}
}
