package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChildrenListsImmediateFilesAndFolderSubcontent(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Aldnoah Zero")
	s1 := filepath.Join(show, "Season 1")
	s2 := filepath.Join(show, "Season 2")
	if err := os.MkdirAll(s1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s1, "Aldnoah Zero S01E01.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s2, "Aldnoah Zero S02E01.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	movie := filepath.Join(root, "A Silent Voice (2016).mkv")
	if err := os.WriteFile(movie, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Children(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	var folder, file *Child
	for i := range got {
		if strings.Contains(got[i].Listing, "Folder: Aldnoah Zero") {
			folder = &got[i]
		}
		if strings.Contains(got[i].Listing, "File: A Silent Voice (2016).mkv") {
			file = &got[i]
		}
	}
	if folder == nil || file == nil {
		t.Fatalf("listings=%v", []string{got[0].Listing, got[1].Listing})
	}
	if !strings.Contains(folder.Listing, "Season 1/") || !strings.Contains(folder.Listing, "Season 2/") {
		t.Fatalf("missing seasons: %s", folder.Listing)
	}
	if !strings.Contains(folder.Listing, "Aldnoah Zero S01E01.mkv") {
		t.Fatalf("missing sample: %s", folder.Listing)
	}
	if folder.Videos != 2 {
		t.Fatalf("folder videos=%d", folder.Videos)
	}
	if file.Videos != 1 {
		t.Fatalf("file videos=%d", file.Videos)
	}
}

func TestChildrenSeasonFolderHasParent(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Frieren")
	s1 := filepath.Join(show, "Season 1")
	if err := os.MkdirAll(s1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s1, "Frieren S01E01.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Children(root, show)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != s1 {
		t.Fatalf("children=%+v", got)
	}
	if !strings.Contains(got[0].Listing, "Folder: Season 1") {
		t.Fatalf("listing=%s", got[0].Listing)
	}
	if !strings.Contains(got[0].Listing, "Parent: Frieren") {
		t.Fatalf("missing parent: %s", got[0].Listing)
	}
}

func TestChildrenDoesNotSplitByFileCount(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "Black Clover")
	s1 := filepath.Join(show, "S1")
	if err := os.MkdirAll(s1, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 90; i++ {
		name := filepath.Join(s1, fmt.Sprintf("Black Clover S01E%02d.mkv", i))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Children(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d want 1", len(got))
	}
	if !strings.Contains(got[0].Listing, "90 videos") {
		t.Fatalf("listing=%s", got[0].Listing)
	}
	if got[0].Videos != 90 {
		t.Fatalf("videos=%d", got[0].Videos)
	}
}

func TestListVideosFollowsSymlinkLibrary(t *testing.T) {
	realRoot := t.TempDir()
	season := filepath.Join(realRoot, "Show", "Season 1")
	if err := os.MkdirAll(season, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(season, "Show S01E01.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	if err := os.Symlink(realRoot, lib); err != nil {
		t.Fatal(err)
	}

	videos, err := ListVideos(root, lib)
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 1 {
		t.Fatalf("videos=%v", videos)
	}

	got, err := ListVideos(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("parent walk videos=%v", got)
	}
}
