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
