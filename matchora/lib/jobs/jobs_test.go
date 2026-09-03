package jobs

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alyshmahell/matchora/lib/config"
	"github.com/alyshmahell/matchora/lib/match"
)

func seed(t *testing.T, store *Store, list []match.Job) string {
	t.Helper()
	id := NewSessionID(time.Now().UTC())
	if err := store.ReplaceAll(id, list); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestStoreClearEmptiesList(t *testing.T) {
	store := New(t.TempDir())
	sess := seed(t, store, []match.Job{
		{ID: "a", Title: "Girls", Status: "matched"},
		{ID: "b", Title: "Bebop", Status: "pending"},
	})
	if err := store.Clear(sess); err != nil {
		t.Fatal(err)
	}
	if store.Has(sess) {
		t.Fatal("session file still present")
	}
	if _, err := store.List(sess); !os.IsNotExist(err) {
		t.Fatalf("list after clear: %v", err)
	}
}

func TestStoreCatalogNilVsEmpty(t *testing.T) {
	store := New(t.TempDir())
	empty := []match.CatalogSeason{}
	sess := seed(t, store, []match.Job{
		{ID: "nilcat", Title: "A", Status: "matched"},
		{ID: "empty", Title: "B", Status: "matched", Catalog: empty, CatalogFor: "tvmaze:1"},
	})
	got, err := store.List(sess)
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
	sess := seed(t, store, []match.Job{
		{
			ID:         "a",
			Title:      "Girls",
			Status:     "matched",
			Catalog:    []match.CatalogSeason{{Title: "Season 1"}},
			CatalogFor: "tvmaze:139",
		},
	})
	if _, err := store.MarkPending(sess); err != nil {
		t.Fatal(err)
	}
	got, err := store.List(sess)
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

func TestSecondSessionDoesNotRewriteFirst(t *testing.T) {
	store := New(t.TempDir())
	first := seed(t, store, []match.Job{{ID: "a", Title: "Girls", Status: "matched"}})
	second := seed(t, store, []match.Job{{ID: "b", Title: "Bebop", Status: "pending"}})
	got, err := store.List(first)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("first=%+v", got)
	}
	got, err = store.List(second)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("second=%+v", got)
	}
}

func TestInvalidSessionRejected(t *testing.T) {
	store := New(t.TempDir())
	if err := store.Create("../escape"); err != ErrInvalidSession {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.List("not-a-session"); err != ErrInvalidSession {
		t.Fatalf("list: %v", err)
	}
}

func TestSessionTTLClampAndExpire(t *testing.T) {
	cfg := config.Config{Session: config.Session{TTLMS: 200000000, TTLMaxMS: 86400000}}
	if cfg.SessionTTL() != 24*time.Hour {
		t.Fatalf("ttl=%s", cfg.SessionTTL())
	}
	store := New(t.TempDir())
	old := NewSessionID(time.Now().UTC().Add(-2 * time.Hour))
	if err := store.ReplaceAll(old, []match.Job{{ID: "a", Title: "Gone", Status: "matched", Match: &match.Candidate{Provider: "tvmaze", ID: "1"}}}); err != nil {
		t.Fatal(err)
	}
	gone := store.PurgeExpired(time.Hour)
	if len(gone) != 1 || gone[0] != old {
		t.Fatalf("purged=%v", gone)
	}
	if store.Has(old) {
		t.Fatal("expired file remains")
	}
	if _, err := store.List(old); !os.IsNotExist(err) {
		t.Fatalf("list expired: %v", err)
	}
	pins, err := store.Pinning(time.Hour, config.Config{}, "tvmaze", "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 0 {
		t.Fatalf("expired still pins: %v", pins)
	}
}

func TestJobsFileNamedBySession(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	sess := seed(t, store, []match.Job{{ID: "a", Title: "Girls"}})
	if _, err := os.Stat(filepath.Join(dir, "jobs-"+sess+".json")); err != nil {
		t.Fatal(err)
	}
}
