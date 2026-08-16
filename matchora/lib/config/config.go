package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const runOverlay = "/run/matchora/config.yaml"

const fallbackPrompt = `You group one library folder or file into unique titles. Return JSON only.
Return JSON {"shows":[{"title":"","year":""}]} only.`

type Config struct {
	HTTP       HTTP                `yaml:"http"`
	DataDir    string              `yaml:"data_dir"`
	BrowseRoot string              `yaml:"browse_root"`
	Version    string              `yaml:"version"`
	Ranker     string              `yaml:"ranker"`
	Match      Match               `yaml:"match"`
	Llama      Llama               `yaml:"llama"`
	Providers  map[string]Provider `yaml:"providers"`
	ConfigPath string              `yaml:"-"`
}

type HTTP struct {
	Addr              string `yaml:"addr"`
	TimeoutMS         int    `yaml:"timeout_ms"`
	Retries           int    `yaml:"retries"`
	BackoffMS         []int  `yaml:"backoff_ms"`
	ProviderTimeoutMS int    `yaml:"provider_timeout_ms"`
}

type Match struct {
	MinScore      float64 `yaml:"min_score"`
	MinMargin     float64 `yaml:"min_margin"`
	MinHits       int     `yaml:"min_hits"`
	Workers       int     `yaml:"workers"`
	CooldownFails int `yaml:"cooldown_fails"`
	CooldownMS    int `yaml:"cooldown_ms"`
}

type Llama struct {
	BaseURL      string `yaml:"base_url"`
	Model        string `yaml:"model"`
	LLMBaseURL   string `yaml:"llm_base_url"`
	BinDir       string `yaml:"bin_dir"`
	ModelsDir    string `yaml:"models_dir"`
	TarballFile  string `yaml:"tarball_file"`
	TarballURL   string `yaml:"tarball_url"`
	ModelFile    string `yaml:"model_file"`
	ModelURL     string `yaml:"model_url"`
	InstructFile string `yaml:"instruct_file"`
	InstructURL  string `yaml:"instruct_url"`
	GPULayers    int    `yaml:"gpu_layers"`
}

type Provider struct {
	Types         []string          `yaml:"types"`
	Require       string            `yaml:"require"`
	APIKey        string            `yaml:"-"`
	Base          string            `yaml:"base"`
	URL           string            `yaml:"url"`
	Query         map[string]string `yaml:"query"`
	Items         string            `yaml:"items"`
	Fields        map[string]string `yaml:"fields"`
	Year          string            `yaml:"year"`
	URLPrefix     string            `yaml:"url_prefix"`
	MinIntervalMS int               `yaml:"min_interval_ms"`
	Defer         bool              `yaml:"defer"`
	TypeParams    map[string]string `yaml:"type_params"`
	Episode       *Episode          `yaml:"episode"`
}

type Episode struct {
	URL    string            `yaml:"url"`
	Query  map[string]string `yaml:"query"`
	Fields map[string]string `yaml:"fields"`
	Year   string            `yaml:"year"`
}

func Load(path string) (Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Config{}, fmt.Errorf("-config is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if b, err := os.ReadFile(runOverlay); err == nil && len(b) > 0 {
		raw, err = merge(raw, b)
		if err != nil {
			return Config{}, err
		}
	}
	cfg, err := decode(raw)
	if err != nil {
		return Config{}, err
	}
	if cfg.DataDir != "" {
		if b, err := os.ReadFile(filepath.Join(cfg.DataDir, "config.yaml")); err == nil && len(b) > 0 {
			raw, err = merge(raw, b)
			if err != nil {
				return Config{}, err
			}
			cfg, err = decode(raw)
			if err != nil {
				return Config{}, err
			}
		}
	}
	if cfg.BrowseRoot == "" {
		cfg.BrowseRoot = cfg.DataDir
	}
	if cfg.Ranker != "llm" {
		cfg.Ranker = "embed"
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]Provider{}
	}
	cfg.ConfigPath = path
	applySecrets(&cfg)
	return cfg, nil
}

func (c Config) Prompt() string {
	if c.ConfigPath != "" {
		p := filepath.Join(filepath.Dir(c.ConfigPath), "prompt.md")
		if b, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(b))) > 0 {
			return string(b)
		}
	}
	return fallbackPrompt
}

func (c Config) LlamaBinDir() string {
	return c.llamaPath(c.Llama.BinDir, "llamacpp/bin")
}

func (c Config) LlamaModelsDir() string {
	return c.llamaPath(c.Llama.ModelsDir, "llamacpp/models")
}

func (c Config) llamaPath(rel, fallback string) string {
	if rel == "" {
		rel = fallback
	}
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(c.DataDir, rel)
}

func (c Config) LocalInstruct() bool {
	u := strings.ToLower(strings.TrimSpace(c.Llama.LLMBaseURL))
	return strings.Contains(u, "127.0.0.1:8081") || strings.Contains(u, "localhost:8081")
}

func (c Config) MatchWorkers() int {
	if c.Match.Workers < 1 {
		return 1
	}
	return c.Match.Workers
}

func (c Config) MatchMinHits() int {
	if c.Match.MinHits < 1 {
		return 1
	}
	return c.Match.MinHits
}

func (c Config) MatchCooldownFails() int {
	if c.Match.CooldownFails < 0 {
		return 0
	}
	if c.Match.CooldownFails == 0 {
		return 2
	}
	return c.Match.CooldownFails
}

func (c Config) MatchCooldown() time.Duration {
	if c.Match.CooldownMS <= 0 {
		return time.Hour
	}
	return time.Duration(c.Match.CooldownMS) * time.Millisecond
}

func (c Config) HTTPTimeout() time.Duration {
	if c.HTTP.TimeoutMS <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.HTTP.TimeoutMS) * time.Millisecond
}

func (c Config) ProviderTimeout() time.Duration {
	if c.HTTP.ProviderTimeoutMS <= 0 {
		return 10 * time.Second
	}
	return time.Duration(c.HTTP.ProviderTimeoutMS) * time.Millisecond
}

func (c Config) Backoffs() []time.Duration {
	if len(c.HTTP.BackoffMS) == 0 {
		return []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	}
	out := make([]time.Duration, len(c.HTTP.BackoffMS))
	for i, ms := range c.HTTP.BackoffMS {
		out[i] = time.Duration(ms) * time.Millisecond
	}
	return out
}

func applySecrets(cfg *Config) {
	b, err := os.ReadFile(filepath.Join(cfg.DataDir, "secrets"))
	if err != nil || len(b) == 0 {
		return
	}
	var keys map[string]string
	if err := yaml.Unmarshal(b, &keys); err != nil {
		return
	}
	for name, key := range keys {
		p, ok := cfg.Providers[name]
		if !ok {
			continue
		}
		p.APIKey = strings.TrimSpace(key)
		cfg.Providers[name] = p
	}
}

func merge(base, over []byte) ([]byte, error) {
	var a, b map[string]any
	if err := yaml.Unmarshal(base, &a); err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(over, &b); err != nil {
		return nil, err
	}
	mergeMap(a, b)
	return yaml.Marshal(a)
}

func mergeMap(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := asMap(v); ok {
			if dm, ok := asMap(dst[k]); ok {
				mergeMap(dm, sm)
				dst[k] = dm
				continue
			}
		}
		dst[k] = v
	}
}

func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			out[stringify(k)] = val
		}
		return out, true
	default:
		return nil, false
	}
}

func stringify(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func decode(b []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
