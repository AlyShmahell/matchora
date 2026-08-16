package config

import (
	"os"
	"path/filepath"
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

func TestMatchWorkers(t *testing.T) {
	if (Config{}).MatchWorkers() != 1 {
		t.Fatal("zero workers should be 1")
	}
	if (Config{Match: Match{Workers: 8}}).MatchWorkers() != 8 {
		t.Fatal("workers=8")
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
	if (Config{}).MatchCooldown() != time.Hour {
		t.Fatal("unset cooldown_ms should be 1h")
	}
	if (Config{Match: Match{CooldownMS: 1500}}).MatchCooldown() != 1500*time.Millisecond {
		t.Fatal("cooldown_ms=1500")
	}
}
