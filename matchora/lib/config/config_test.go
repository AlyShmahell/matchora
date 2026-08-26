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
