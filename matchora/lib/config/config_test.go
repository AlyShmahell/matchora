package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLoadReadsPath(t *testing.T) {
	cfg := loadOverlay(t, "version: \"9.9.9\"\n")
	if cfg.Version != "9.9.9" {
		t.Fatalf("version=%q", cfg.Version)
	}
	if cfg.ConfigPath == "" {
		t.Fatal("config path empty")
	}
}

func TestLoadRequiresPath(t *testing.T) {
	if _, err := Load(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadReadsIngest(t *testing.T) {
	cfg := loadOverlay(t, "ingest:\n  sample_rows: 2\n  aliases:\n    mediatype: type\n  types:\n    episode: tv\n")
	if cfg.Ingest.SampleRows != 2 {
		t.Fatalf("sample_rows=%d", cfg.Ingest.SampleRows)
	}
	if cfg.Ingest.Aliases["mediatype"] != "type" {
		t.Fatalf("aliases=%v", cfg.Ingest.Aliases)
	}
	if cfg.Ingest.Types["episode"] != "tv" {
		t.Fatalf("types=%v", cfg.Ingest.Types)
	}
}

func TestIngestSampleRows(t *testing.T) {
	if (Config{Ingest: Ingest{SampleRows: 5}}).IngestSampleRows() != 5 {
		t.Fatal("sample_rows=5")
	}
}

func TestMatchWorkers(t *testing.T) {
	if (Config{Match: Match{Workers: 8}}).MatchWorkers() != 8 {
		t.Fatal("workers=8")
	}
}

func TestMatchSoloScore(t *testing.T) {
	if (Config{Match: Match{MinScore: 0.72}}).MatchSoloScore() != 0.72 {
		t.Fatal("unset solo_min_score should be min_score")
	}
	if (Config{Match: Match{MinScore: 0.72, SoloMinScore: 0.01}}).MatchSoloScore() != 0.01 {
		t.Fatal("solo_min_score=0.01")
	}
}

func TestMatchMinHits(t *testing.T) {
	if (Config{Match: Match{MinHits: 3}}).MatchMinHits() != 3 {
		t.Fatal("min_hits=3")
	}
}

func TestMatchCooldownFails(t *testing.T) {
	if (Config{Match: Match{CooldownFails: 0}}).MatchCooldownFails() != 0 {
		t.Fatal("zero cooldown_fails should stay 0")
	}
	if (Config{Match: Match{CooldownFails: 4}}).MatchCooldownFails() != 4 {
		t.Fatal("cooldown_fails=4")
	}
}

func TestJitterExpBounds(t *testing.T) {
	for e := 1; e <= 12; e++ {
		lo := time.Duration(1<<(e-1)) * time.Millisecond
		hi := time.Duration(1<<e) * time.Millisecond
		for i := 0; i < 40; i++ {
			d := JitterExp(e)
			if d < lo || d > hi {
				t.Fatalf("exp=%d d=%s want [%s, %s]", e, d, lo, hi)
			}
		}
	}
}

func TestLoadSecretAlias(t *testing.T) {
	path, data := writeMerged(t, "providers:\n  tmdb: {}\n  tmdb_tv:\n    secret: tmdb\n")
	if err := os.WriteFile(filepath.Join(data, "secrets"), []byte("tmdb: abc123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers["tmdb"].APIKey != "abc123" {
		t.Fatalf("tmdb key=%q", cfg.Providers["tmdb"].APIKey)
	}
	if cfg.Providers["tmdb_tv"].APIKey != "abc123" {
		t.Fatalf("tmdb_tv key=%q", cfg.Providers["tmdb_tv"].APIKey)
	}
}

func TestLoadShareYAML(t *testing.T) {
	cfg := loadOverlay(t, "")
	if cfg.Version == "" {
		t.Fatal("share version empty")
	}
	if _, ok := cfg.GroupVideoExt()[".mkv"]; !ok {
		t.Fatal("share video ext")
	}
	if _, ok := cfg.GroupExtras()["extras"]; !ok {
		t.Fatal("share extras")
	}
	if cfg.SeqThreshold() != 0.72 {
		t.Fatalf("threshold=%v", cfg.SeqThreshold())
	}
	tl := cfg.Providers["tvmaze"].Titles
	if tl == nil || tl.Max != 3 || tl.URL != "{base}/shows/{id}/akas" || tl.Fields["title"] != "name" {
		t.Fatalf("tvmaze titles=%+v", tl)
	}
}

func TestLoadMissingExtras(t *testing.T) {
	_, err := loadOverlayErr(t, "group:\n  extras: []\n")
	if err == nil || !strings.Contains(err.Error(), "group.extras is empty") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadTunables(t *testing.T) {
	cfg := loadOverlay(t, "")
	if cfg.SampleVideos() != 5 || cfg.WaitCap() != 500 || cfg.SynopsisLimit() != 4000 {
		t.Fatalf("sample=%d wait=%d clip=%d", cfg.SampleVideos(), cfg.WaitCap(), cfg.SynopsisLimit())
	}
	if cfg.SessionTTLMax() != 24*time.Hour {
		t.Fatalf("ttl_max=%s", cfg.SessionTTLMax())
	}
}

func TestLoadMissingTunables(t *testing.T) {
	cases := []struct {
		overlay, want string
	}{
		{"scan:\n  sample_videos: 0\n", "scan.sample_videos must be > 0"},
		{"match:\n  wait_cap: 0\n", "match.wait_cap must be > 0"},
		{"match:\n  synopsis_limit: 0\n", "match.synopsis_limit must be > 0"},
		{"session:\n  ttl_max_ms: 0\n", "session.ttl_max_ms must be > 0"},
	}
	for _, tc := range cases {
		_, err := loadOverlayErr(t, tc.overlay)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("overlay %q err=%v", tc.overlay, err)
		}
	}
}

func TestLoadMissingPlotStop(t *testing.T) {
	_, err := loadOverlayErr(t, "match:\n  plot_stop: []\n")
	if err == nil || !strings.Contains(err.Error(), "match.plot_stop is empty") {
		t.Fatalf("err=%v", err)
	}
}

func TestPlotStopHelper(t *testing.T) {
	cfg := loadOverlay(t, "")
	if _, ok := cfg.PlotStop()["the"]; !ok {
		t.Fatalf("plot_stop=%v", cfg.PlotStop())
	}
}

func TestLoadDefaultDataDirBesideBinary(t *testing.T) {
	path, _ := writeMerged(t, "data_dir: \"\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	root, err := ExeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "data")
	if cfg.DataDir != want {
		t.Fatalf("data_dir=%q want %q", cfg.DataDir, want)
	}
	if cfg.BrowseRoot != want {
		t.Fatalf("browse_root=%q want %q", cfg.BrowseRoot, want)
	}
	if cfg.ExeDir != root {
		t.Fatalf("exe_dir=%q want %q", cfg.ExeDir, root)
	}
}

func TestLoadRelativeDataDir(t *testing.T) {
	cfg := loadOverlay(t, "data_dir: rel-data\n")
	root, err := ExeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "rel-data")
	if cfg.DataDir != want {
		t.Fatalf("data_dir=%q want %q", cfg.DataDir, want)
	}
}

func TestSeqThresholdFromYAML(t *testing.T) {
	cfg := loadOverlay(t, "group:\n  seq_threshold: 0.5\n")
	if cfg.SeqThreshold() != 0.5 {
		t.Fatalf("threshold=%v", cfg.SeqThreshold())
	}
}

func TestLoadProviderOptionalFields(t *testing.T) {
	cfg := loadOverlay(t, `
providers:
  src:
    retries: 1
    provider_timeout_ms: 4000
    nfo: movie
    uniqueid: tmdb
    detail:
      url: "{base}"
      query: { i: "{id}" }
      fields: { synopsis: Plot }
`)
	p := cfg.Providers["src"]
	if p.Retries != 1 || p.ProviderTimeoutMS != 4000 || p.NFO != "movie" || p.UniqueID != "tmdb" {
		t.Fatalf("provider=%+v", p)
	}
	if p.Detail == nil || p.Detail.Fields["synopsis"] != "Plot" || p.Detail.Query["i"] != "{id}" {
		t.Fatalf("detail=%+v", p.Detail)
	}
}

func TestSecretKeys(t *testing.T) {
	cfg := Config{
		Providers: map[string]Provider{
			"tvmaze":  {},
			"omdb":    {Require: "api_key"},
			"tmdb":    {Require: "api_key"},
			"tmdb_tv": {Require: "api_key", Secret: "tmdb"},
		},
	}
	got := SecretKeys(cfg)
	want := []string{"omdb", "tmdb"}
	if len(got) != len(want) {
		t.Fatalf("keys=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys=%v want %v", got, want)
		}
	}
}

func TestSetSecretsMergeDeleteAndMode(t *testing.T) {
	cfg := loadSecretsCfg(t, `
providers:
  tmdb:
    require: api_key
  tmdb_tv:
    secret: tmdb
  omdb:
    require: api_key
`)
	if err := SetSecrets(&cfg, map[string]string{"tmdb": "abc123"}); err != nil {
		t.Fatal(err)
	}
	st := SecretsStatus(cfg)
	if !st["tmdb"] || st["omdb"] {
		t.Fatalf("status=%v", st)
	}
	got, err := Load(cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Providers["tmdb"].APIKey != "abc123" || got.Providers["tmdb_tv"].APIKey != "abc123" {
		t.Fatalf("tmdb after set: %+v %+v", got.Providers["tmdb"], got.Providers["tmdb_tv"])
	}
	if err := SetSecrets(&cfg, map[string]string{"omdb": "omdb-key"}); err != nil {
		t.Fatal(err)
	}
	st = SecretsStatus(cfg)
	if !st["tmdb"] || !st["omdb"] {
		t.Fatalf("status after merge=%v", st)
	}
	got, err = Load(cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Providers["tmdb"].APIKey != "abc123" || got.Providers["omdb"].APIKey != "omdb-key" {
		t.Fatalf("merge keys tmdb=%q omdb=%q", got.Providers["tmdb"].APIKey, got.Providers["omdb"].APIKey)
	}
	info, err := os.Stat(filepath.Join(cfg.DataDir, "secrets"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm=%o", info.Mode().Perm())
	}
	if err := SetSecrets(&cfg, map[string]string{"tmdb": ""}); err != nil {
		t.Fatal(err)
	}
	st = SecretsStatus(cfg)
	if st["tmdb"] || !st["omdb"] {
		t.Fatalf("status after delete=%v", st)
	}
	got, err = Load(cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Providers["tmdb"].APIKey != "" || got.Providers["tmdb_tv"].APIKey != "" {
		t.Fatalf("tmdb after delete: %q %q", got.Providers["tmdb"].APIKey, got.Providers["tmdb_tv"].APIKey)
	}
	if got.Providers["omdb"].APIKey != "omdb-key" {
		t.Fatalf("omdb cleared: %q", got.Providers["omdb"].APIKey)
	}
}

func TestSetSecretsUnknownKey(t *testing.T) {
	cfg := loadSecretsCfg(t, "providers:\n  tmdb:\n    require: api_key\n")
	err := SetSecrets(&cfg, map[string]string{"nope": "x"})
	if err == nil || !strings.Contains(err.Error(), "unknown secret key") {
		t.Fatalf("err=%v", err)
	}
}

func TestSetSecretsCreatesMissingFile(t *testing.T) {
	cfg := loadSecretsCfg(t, "providers:\n  omdb:\n    require: api_key\n")
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "secrets")); !os.IsNotExist(err) {
		t.Fatalf("secrets should be missing: %v", err)
	}
	if err := SetSecrets(&cfg, map[string]string{"omdb": "k"}); err != nil {
		t.Fatal(err)
	}
	st := SecretsStatus(cfg)
	if !st["omdb"] {
		t.Fatalf("status=%v", st)
	}
	got, err := Load(cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Providers["omdb"].APIKey != "k" {
		t.Fatalf("omdb=%q", got.Providers["omdb"].APIKey)
	}
}

func TestOverlayMerge(t *testing.T) {
	cfg := loadSecretsCfg(t, "match:\n  min_score: 0.5\n")
	got, err := Overlay(&cfg, map[string]any{
		"match": map[string]any{"min_score": 0.9},
		"group": map[string]any{"seq_threshold": 0.8},
	})
	if err != nil {
		t.Fatal(err)
	}
	match, _ := got["match"].(map[string]any)
	if match["min_score"] != 0.9 {
		t.Fatalf("min_score=%v", match["min_score"])
	}
	group, _ := got["group"].(map[string]any)
	if group["seq_threshold"] != 0.8 {
		t.Fatalf("threshold=%v", group["seq_threshold"])
	}
	again, err := Overlay(&cfg, map[string]any{"match": map[string]any{"min_margin": 0.1}})
	if err != nil {
		t.Fatal(err)
	}
	match, _ = again["match"].(map[string]any)
	if match["min_score"] != 0.9 || match["min_margin"] != 0.1 {
		t.Fatalf("merged match=%v", match)
	}
	group, _ = again["group"].(map[string]any)
	if group["seq_threshold"] != 0.8 {
		t.Fatalf("group dropped=%v", group)
	}
}

func TestReadOverlayEmpty(t *testing.T) {
	cfg := loadSecretsCfg(t, "")
	got, err := ReadOverlay(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got=%v", got)
	}
}

func TestSessionTTLClamp(t *testing.T) {
	max := 24 * time.Hour
	if (Config{Session: Session{TTLMaxMS: 86400000}}).SessionTTL() != max {
		t.Fatalf("default=%s", Config{Session: Session{TTLMaxMS: 86400000}}.SessionTTL())
	}
	if (Config{Session: Session{TTLMS: -1, TTLMaxMS: 86400000}}).SessionTTL() != max {
		t.Fatal("negative")
	}
	got := (Config{Session: Session{TTLMS: 3600000, TTLMaxMS: 86400000}}).SessionTTL()
	if got != time.Hour {
		t.Fatalf("hour=%s", got)
	}
	if (Config{Session: Session{TTLMS: 200000000, TTLMaxMS: 86400000}}).SessionTTL() != max {
		t.Fatal("clamp")
	}
}

func TestOverlayRejectsEmptyExtras(t *testing.T) {
	cfg := loadOverlay(t, "")
	_, err := Overlay(&cfg, map[string]any{"group": map[string]any{"extras": []any{}}})
	if err == nil || !strings.Contains(err.Error(), "group.extras is empty") {
		t.Fatalf("err=%v", err)
	}
}

func shareYAML(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "share", "config", "default.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeMerged(t *testing.T, extra string) (path, data string) {
	t.Helper()
	dir := t.TempDir()
	data = filepath.Join(dir, "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	over := extra
	if !strings.Contains(over, "data_dir:") {
		over = "data_dir: " + strconv.Quote(data) + "\n" + extra
	}
	merged, err := merge(shareYAML(t), []byte(over))
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, "default.yaml")
	if err := os.WriteFile(path, merged, 0o644); err != nil {
		t.Fatal(err)
	}
	return path, data
}

func loadOverlay(t *testing.T, extra string) Config {
	t.Helper()
	cfg, err := loadOverlayErr(t, extra)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func loadOverlayErr(t *testing.T, extra string) (Config, error) {
	t.Helper()
	path, _ := writeMerged(t, extra)
	return Load(path)
}

func loadSecretsCfg(t *testing.T, body string) Config {
	return loadOverlay(t, body)
}
