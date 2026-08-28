package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLoadReadsPathAndPrompt(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "default.yaml")
	if err := os.WriteFile(yamlPath, []byte("data_dir: /tmp/matchora\nversion: \"9.9.9\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("unique titles from prompt.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != "9.9.9" {
		t.Fatalf("version=%q", cfg.Version)
	}
	if cfg.ConfigPath != yamlPath {
		t.Fatalf("config path=%q", cfg.ConfigPath)
	}
	if !strings.Contains(cfg.Prompt(), "prompt.md") {
		t.Fatalf("prompt=%q", cfg.Prompt())
	}
}

func TestLoadRequiresPath(t *testing.T) {
	if _, err := Load(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestPromptFallback(t *testing.T) {
	cfg := Config{}
	if !strings.Contains(cfg.Prompt(), "unique titles") {
		t.Fatalf("fallback=%q", cfg.Prompt())
	}
}

func TestLoadReadsIngestAndPrompt(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "default.yaml")
	raw := "data_dir: /tmp/matchora\ningest:\n  sample_rows: 2\n  aliases:\n    mediatype: type\n  types:\n    episode: tv\n"
	if err := os.WriteFile(yamlPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ingest.md"), []byte("map CSV column headers from ingest.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Ingest.SampleRows != 2 {
		t.Fatalf("sample_rows=%d", cfg.Ingest.SampleRows)
	}
	if cfg.Ingest.Aliases["mediatype"] != "type" {
		t.Fatalf("aliases=%v", cfg.Ingest.Aliases)
	}
	if cfg.Ingest.Types["episode"] != "tv" {
		t.Fatalf("types=%v", cfg.Ingest.Types)
	}
	if !strings.Contains(cfg.IngestPrompt(), "ingest.md") {
		t.Fatalf("ingest prompt=%q", cfg.IngestPrompt())
	}
}

func TestIngestPromptFallback(t *testing.T) {
	cfg := Config{}
	if !strings.Contains(cfg.IngestPrompt(), "columns") {
		t.Fatalf("fallback=%q", cfg.IngestPrompt())
	}
}

func TestIngestSampleRows(t *testing.T) {
	if (Config{}).IngestSampleRows() != 3 {
		t.Fatal("unset sample_rows should be 3")
	}
	if (Config{Ingest: Ingest{SampleRows: 5}}).IngestSampleRows() != 5 {
		t.Fatal("sample_rows=5")
	}
}

func TestMatchWorkers(t *testing.T) {
	if (Config{}).MatchWorkers() != 1 {
		t.Fatal("zero workers should be 1")
	}
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
	if (Config{}).MatchMinHits() != 1 {
		t.Fatal("zero min_hits should be 1")
	}
	if (Config{Match: Match{MinHits: 3}}).MatchMinHits() != 3 {
		t.Fatal("min_hits=3")
	}
}

func TestMatchCooldownDefaults(t *testing.T) {
	if (Config{}).MatchCooldownFails() != 2 {
		t.Fatal("unset cooldown_fails should be 2")
	}
	if (Config{Match: Match{CooldownFails: -1}}).MatchCooldownFails() != 0 {
		t.Fatal("negative cooldown_fails should disable")
	}
	if (Config{Match: Match{CooldownFails: 4}}).MatchCooldownFails() != 4 {
		t.Fatal("cooldown_fails=4")
	}
	got := (Config{}).MatchCooldown()
	if got.MinExp != 16 || got.MaxExp != 19 {
		t.Fatalf("unset cooldown=%+v want 16/19", got)
	}
	got = (Config{Match: Match{Cooldown: ExpRange{MinExp: 4, MaxExp: 7}}}).MatchCooldown()
	if got.MinExp != 4 || got.MaxExp != 7 {
		t.Fatalf("cooldown=%+v", got)
	}
}

func TestClampExp(t *testing.T) {
	got := ClampExp(ExpRange{}, 10, 13)
	if got.MinExp != 10 || got.MaxExp != 13 {
		t.Fatalf("defaults=%+v", got)
	}
	got = ClampExp(ExpRange{MinExp: 3, MaxExp: 4}, 10, 13)
	if got.MinExp != 3 || got.MaxExp != 5 {
		t.Fatalf("max < min+2: %+v", got)
	}
	got = ClampExp(ExpRange{MinExp: -2, MaxExp: 3}, 10, 13)
	if got.MinExp != 0 || got.MaxExp != 3 {
		t.Fatalf("neg min=%+v", got)
	}
	got = ClampExp(ExpRange{MinExp: 0, MaxExp: 2}, 10, 13)
	if got.MinExp != 0 || got.MaxExp != 2 {
		t.Fatalf("explicit zero min=%+v", got)
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
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "secrets"), []byte("tmdb: abc123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(dir, "default.yaml")
	raw := "data_dir: " + strconv.Quote(data) + "\nproviders:\n  tmdb: {}\n  tmdb_tv:\n    secret: tmdb\n"
	if err := os.WriteFile(yamlPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(yamlPath)
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

func TestHTTPBackoffDefaults(t *testing.T) {
	got := (Config{}).HTTPBackoff()
	if got.MinExp != 10 || got.MaxExp != 13 {
		t.Fatalf("unset backoff=%+v want 10/13", got)
	}
}

func TestLoadDefaultDataDirBesideBinary(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "default.yaml")
	if err := os.WriteFile(yamlPath, []byte("version: \"1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(yamlPath)
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
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "default.yaml")
	if err := os.WriteFile(yamlPath, []byte("data_dir: rel-data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	root, err := ExeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "rel-data")
	if cfg.DataDir != want {
		t.Fatalf("data_dir=%q want %q", cfg.DataDir, want)
	}
}

func TestLlamaPathsBesideBinary(t *testing.T) {
	cfg := Config{ExeDir: "/opt/matchora"}
	if got := cfg.LlamaBinDir(); got != "/opt/matchora/vendor/llama.cpp" {
		t.Fatalf("bin=%q", got)
	}
	if got := cfg.LlamaModelsDir(); got != "/opt/matchora/vendor/llama.cpp/models" {
		t.Fatalf("models=%q", got)
	}
	cfg.Llama.BinDir = "custom/bin"
	cfg.Llama.ModelsDir = "/abs/models"
	if got := cfg.LlamaBinDir(); got != "/opt/matchora/custom/bin" {
		t.Fatalf("custom bin=%q", got)
	}
	if got := cfg.LlamaModelsDir(); got != "/abs/models" {
		t.Fatalf("abs models=%q", got)
	}
}

func TestLocalInstructSameOrigin(t *testing.T) {
	cfg := Config{Llama: Llama{
		BaseURL:    "http://127.0.0.1:8080/v1",
		LLMBaseURL: "http://127.0.0.1:8080/v1",
	}}
	if !cfg.LocalInstruct() {
		t.Fatal("same origin should be local")
	}
	cfg.Llama.LLMBaseURL = ""
	if !cfg.LocalInstruct() {
		t.Fatal("empty llm_base_url should be local")
	}
	cfg.Llama.LLMBaseURL = "http://stub:8080/v1"
	if cfg.LocalInstruct() {
		t.Fatal("stub should not be local")
	}
}

func TestEmbedInstructModelIDs(t *testing.T) {
	cfg := Config{Llama: Llama{
		EmbedFile:    "all-MiniLM-L6-v2-Q4_K_M.gguf",
		InstructFile: "SmolLM2-135M-Instruct-Q8_0.gguf",
	}}
	if got := cfg.EmbedModel(); got != "all-MiniLM-L6-v2-Q4_K_M" {
		t.Fatalf("embed=%q", got)
	}
	if got := cfg.InstructModel(); got != "SmolLM2-135M-Instruct-Q8_0" {
		t.Fatalf("instruct=%q", got)
	}
	cfg.Llama.Embed = "minilm"
	cfg.Llama.Instruct = "smol"
	if got := cfg.EmbedModel(); got != "minilm" {
		t.Fatalf("embed override=%q", got)
	}
	if got := cfg.InstructModel(); got != "smol" {
		t.Fatalf("instruct override=%q", got)
	}
}

func TestLoadProviderOptionalFields(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "default.yaml")
	raw := "data_dir: " + strconv.Quote(dir) + `
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
`
	if err := os.WriteFile(yamlPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
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

func TestApplyListenFromHostPort(t *testing.T) {
	cfg := loadSecretsCfg(t, `
llama:
  host: "10.0.0.2"
  port: 9090
  base_url: "http://127.0.0.1:8080/v1"
  llm_base_url: "http://stub:8080/v1"
`)
	if cfg.Llama.Host != "10.0.0.2" || cfg.Llama.Port != 9090 {
		t.Fatalf("listen=%s:%d", cfg.Llama.Host, cfg.Llama.Port)
	}
	if cfg.Llama.BaseURL != "http://10.0.0.2:9090/v1" {
		t.Fatalf("base=%q", cfg.Llama.BaseURL)
	}
	if cfg.Llama.LLMBaseURL != "http://stub:8080/v1" {
		t.Fatalf("llm=%q", cfg.Llama.LLMBaseURL)
	}
}

func TestApplyListenParsesBaseURL(t *testing.T) {
	cfg := loadSecretsCfg(t, `
llama:
  base_url: "http://192.168.1.5:1234/v1"
`)
	if cfg.Llama.Host != "192.168.1.5" || cfg.Llama.Port != 1234 {
		t.Fatalf("listen=%s:%d", cfg.Llama.Host, cfg.Llama.Port)
	}
	if cfg.Llama.BaseURL != "http://192.168.1.5:1234/v1" {
		t.Fatalf("base=%q", cfg.Llama.BaseURL)
	}
}

func TestApplyListenDefaults(t *testing.T) {
	cfg := loadSecretsCfg(t, "version: \"1\"\n")
	if cfg.Llama.Host != "127.0.0.1" || cfg.Llama.Port != 8080 {
		t.Fatalf("listen=%s:%d", cfg.Llama.Host, cfg.Llama.Port)
	}
	if cfg.Llama.BaseURL != "http://127.0.0.1:8080/v1" {
		t.Fatalf("base=%q", cfg.Llama.BaseURL)
	}
}

func TestOverlayMergeAndPort(t *testing.T) {
	cfg := loadSecretsCfg(t, "match:\n  min_score: 0.5\n")
	got, err := Overlay(&cfg, map[string]any{
		"match": map[string]any{"min_score": 0.9},
		"llama": map[string]any{"host": "127.0.0.1", "port": 8081},
	})
	if err != nil {
		t.Fatal(err)
	}
	llama, _ := got["llama"].(map[string]any)
	if llama["host"] != "127.0.0.1" {
		t.Fatalf("overlay llama=%v", llama)
	}
	switch p := llama["port"].(type) {
	case int:
		if p != 8081 {
			t.Fatalf("port=%v", p)
		}
	default:
		t.Fatalf("port type %T %v", p, p)
	}
	match, _ := got["match"].(map[string]any)
	if match["min_score"] != 0.9 {
		t.Fatalf("min_score=%v", match["min_score"])
	}
	again, err := Overlay(&cfg, map[string]any{"match": map[string]any{"min_margin": 0.1}})
	if err != nil {
		t.Fatal(err)
	}
	match, _ = again["match"].(map[string]any)
	if match["min_score"] != 0.9 || match["min_margin"] != 0.1 {
		t.Fatalf("merged match=%v", match)
	}
	llama, _ = again["llama"].(map[string]any)
	if llama["host"] != "127.0.0.1" {
		t.Fatalf("llama dropped=%v", llama)
	}
}

func TestOverlayRejectsBadPort(t *testing.T) {
	cfg := loadSecretsCfg(t, "")
	if _, err := Overlay(&cfg, map[string]any{"llama": map[string]any{"port": 0}}); err == nil {
		t.Fatal("expected port error")
	}
	if _, err := Overlay(&cfg, map[string]any{"llama": map[string]any{"port": 70000}}); err == nil {
		t.Fatal("expected range error")
	}
}

func TestLoadAppliesOverlayListen(t *testing.T) {
	cfg := loadSecretsCfg(t, `
llama:
  host: "127.0.0.1"
  port: 8080
  llm_base_url: "http://stub:8080/v1"
`)
	if _, err := Overlay(&cfg, map[string]any{"llama": map[string]any{"host": "10.1.2.3", "port": 9090}}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Llama.Host != "10.1.2.3" || got.Llama.Port != 9090 {
		t.Fatalf("listen=%s:%d", got.Llama.Host, got.Llama.Port)
	}
	if got.Llama.BaseURL != "http://10.1.2.3:9090/v1" {
		t.Fatalf("base=%q", got.Llama.BaseURL)
	}
	if got.Llama.LLMBaseURL != "http://stub:8080/v1" {
		t.Fatalf("llm=%q", got.Llama.LLMBaseURL)
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

func TestInstructFollowsListen(t *testing.T) {
	if !InstructFollowsListen("", "http://127.0.0.1:8080/v1") {
		t.Fatal("empty llm should follow")
	}
	if !InstructFollowsListen("http://127.0.0.1:8080/v1", "http://127.0.0.1:8080/v1") {
		t.Fatal("same origin should follow")
	}
	if InstructFollowsListen("http://stub:8080/v1", "http://127.0.0.1:8080/v1") {
		t.Fatal("stub should not follow")
	}
}

func TestLlamaProbeVendorURL(t *testing.T) {
	cfg := Config{Llama: Llama{Host: "10.0.0.2", Port: 9090}}
	if got := cfg.LlamaProbeURL(); got != "http://10.0.0.2:9090/v1" {
		t.Fatalf("probe=%q", got)
	}
	if got := cfg.LlamaVendorURL(); got != "http://127.0.0.1:9090/v1" {
		t.Fatalf("vendor=%q", got)
	}
}

func loadSecretsCfg(t *testing.T, body string) Config {
	t.Helper()
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(dir, "default.yaml")
	raw := "data_dir: " + strconv.Quote(data) + "\n" + body
	if err := os.WriteFile(yamlPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
