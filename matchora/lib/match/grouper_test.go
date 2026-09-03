package match

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/matchora/lib/config"
)

func writeTree(t *testing.T, root string, files []string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func titlesOf(got []Grouped) []string {
	out := make([]string, len(got))
	for i, g := range got {
		out[i] = g.Title
	}
	return out
}

func testCfg(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load(filepath.Join("..", "..", "share", "config", "default.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestGroupSeasonOnlyTree(t *testing.T) {
	lib := t.TempDir()
	child := filepath.Join(lib, "Aldnoah Zero")
	writeTree(t, lib, []string{
		"Aldnoah Zero/Season 1/Aldnoah Zero S01E01.mkv",
		"Aldnoah Zero/Season 2/Aldnoah Zero S02E01.mkv",
	})
	got := Group(testCfg(t), lib, child)
	if len(got) != 1 {
		t.Fatalf("got=%v", titlesOf(got))
	}
	if got[0].Title != "Aldnoah Zero" {
		t.Fatalf("title=%q", got[0].Title)
	}
	if got[0].Path != "Aldnoah Zero" {
		t.Fatalf("path=%q", got[0].Path)
	}
}

func TestGroupNamedSiblings(t *testing.T) {
	lib := t.TempDir()
	child := filepath.Join(lib, "KonoSuba")
	writeTree(t, lib, []string{
		"KonoSuba/God's Blessing on This Wonderful World!/Season 1/ep.mkv",
		"KonoSuba/An Explosion on This Wonderful World!/Season 1/ep.mkv",
		"KonoSuba/God's Blessing on This Wonderful World!/Legend of Crimson.mkv",
	})
	got := Group(testCfg(t), lib, child)
	seen := map[string]string{}
	for _, g := range got {
		seen[g.Title] = g.Path
	}
	if _, ok := seen["God's Blessing on This Wonderful World!"]; !ok {
		t.Fatalf("missing blessing: %v", titlesOf(got))
	}
	if _, ok := seen["An Explosion on This Wonderful World!"]; !ok {
		t.Fatalf("missing explosion: %v", titlesOf(got))
	}
	if _, ok := seen["KonoSuba"]; ok {
		t.Fatalf("franchise mash: %v", titlesOf(got))
	}
	if path, ok := seen["Legend of Crimson"]; !ok {
		t.Fatalf("missing movie: %v", titlesOf(got))
	} else if path == "KonoSuba" {
		t.Fatalf("movie path collapsed to parent: %q", path)
	}
	for _, g := range got {
		if g.Parent != "KonoSuba" {
			t.Fatalf("parent=%q title=%q", g.Parent, g.Title)
		}
	}
}

func TestGroupSkipsExtras(t *testing.T) {
	lib := t.TempDir()
	child := filepath.Join(lib, "Solo Leveling")
	writeTree(t, lib, []string{
		"Solo Leveling/Season 1/ep.mkv",
		"Solo Leveling/Openings & Endings/NCOP01.mkv",
		"Solo Leveling/NCOP/op.mkv",
	})
	got := Group(testCfg(t), lib, child)
	if len(got) != 1 || got[0].Title != "Solo Leveling" {
		t.Fatalf("got=%v", titlesOf(got))
	}
}

func TestGroupSmokeReleaseNames(t *testing.T) {
	lib := t.TempDir()
	writeTree(t, lib, []string{
		"Girls.S01.1080p.BluRay.x264-Tag/Girls.S01E01.mkv",
		"Cowboy.Bebop.1998.1080p.BluRay.x264-Tag.mkv",
	})
	cfg := testCfg(t)
	girls := Group(cfg, lib, filepath.Join(lib, "Girls.S01.1080p.BluRay.x264-Tag"))
	if len(girls) != 1 || girls[0].Title != "Girls" {
		t.Fatalf("girls=%v", girls)
	}
	bebop := Group(cfg, lib, filepath.Join(lib, "Cowboy.Bebop.1998.1080p.BluRay.x264-Tag.mkv"))
	if len(bebop) != 1 {
		t.Fatalf("bebop=%v", bebop)
	}
	if bebop[0].Title != "Cowboy Bebop" {
		t.Fatalf("title=%q", bebop[0].Title)
	}
	if bebop[0].Year != "1998" {
		t.Fatalf("year=%q", bebop[0].Year)
	}
}

func TestGroupYearSuffix(t *testing.T) {
	lib := t.TempDir()
	writeTree(t, lib, []string{
		"5 Centimeters Per Second (2007)/5 Centimeters Per Second (2007).mkv",
	})
	got := Group(testCfg(t), lib, filepath.Join(lib, "5 Centimeters Per Second (2007)"))
	if len(got) != 1 {
		t.Fatalf("got=%v", got)
	}
	if got[0].Title != "5 Centimeters Per Second" {
		t.Fatalf("title=%q", got[0].Title)
	}
	if got[0].Year != "2007" {
		t.Fatalf("year=%q", got[0].Year)
	}
}

func TestSeqRatioMatchesPython(t *testing.T) {
	if got := SeqRatio("abcd", "bcde"); got != 0.75 {
		t.Fatalf("ratio=%v", got)
	}
	if got := SeqRatio("cowboy bebop", "cowboy bebop"); got != 1 {
		t.Fatalf("ident=%v", got)
	}
	if got := SeqRatio("", ""); got != 1 {
		t.Fatalf("empty=%v", got)
	}
}
