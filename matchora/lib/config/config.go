package config

import (
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const runOverlay = "/run/matchora/config.yaml"

const fallbackPrompt = `You group one library folder or file into unique titles. Return JSON only.
Return JSON {"shows":[{"title":"","year":""}]} only.`

const fallbackIngestPrompt = `You map CSV headers to title fields. Return JSON only.
Return JSON {"columns":{"title":"","year":"","type":"","season":"","episode":"","imdb":""}} only.`

type Config struct {
	HTTP       HTTP                `yaml:"http"`
	DataDir    string              `yaml:"data_dir"`
	BrowseRoot string              `yaml:"browse_root"`
	Version    string              `yaml:"version"`
	Ranker     string              `yaml:"ranker"`
	Match      Match               `yaml:"match"`
	Llama      Llama               `yaml:"llama"`
	Ingest     Ingest              `yaml:"ingest"`
	Providers  map[string]Provider `yaml:"providers"`
	ConfigPath string              `yaml:"-"`
	ExeDir     string              `yaml:"-"`
}

type Ingest struct {
	SampleRows int               `yaml:"sample_rows"`
	Aliases    map[string]string `yaml:"aliases"`
	Types      map[string]string `yaml:"types"`
}

type HTTP struct {
	Addr              string   `yaml:"addr"`
	TimeoutMS         int      `yaml:"timeout_ms"`
	Retries           int      `yaml:"retries"`
	Backoff           ExpRange `yaml:"backoff"`
	ProviderTimeoutMS int      `yaml:"provider_timeout_ms"`
}

type Match struct {
	MinScore      float64                      `yaml:"min_score"`
	SoloMinScore  float64                      `yaml:"solo_min_score"`
	MinMargin     float64                      `yaml:"min_margin"`
	MinHits       int                          `yaml:"min_hits"`
	Workers       int                          `yaml:"workers"`
	CooldownFails int                          `yaml:"cooldown_fails"`
	Cooldown      ExpRange                     `yaml:"cooldown"`
	Prefer        map[string]map[string]string `yaml:"prefer"`
}

type ExpRange struct {
	MinExp int `yaml:"min_exp"`
	MaxExp int `yaml:"max_exp"`
}

type Llama struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	BaseURL      string `yaml:"base_url"`
	Embed        string `yaml:"embed"`
	LLMBaseURL   string `yaml:"llm_base_url"`
	Instruct     string `yaml:"instruct"`
	BinDir       string `yaml:"bin_dir"`
	ModelsDir    string `yaml:"models_dir"`
	TarballFile  string `yaml:"tarball_file"`
	TarballURL   string `yaml:"tarball_url"`
	EmbedFile    string `yaml:"embed_file"`
	EmbedURL     string `yaml:"embed_url"`
	InstructFile string `yaml:"instruct_file"`
	InstructURL  string `yaml:"instruct_url"`
	GPULayers    int    `yaml:"gpu_layers"`
}

type Provider struct {
	Types             []string          `yaml:"types"`
	Require           string            `yaml:"require"`
	Secret            string            `yaml:"secret"`
	APIKey            string            `yaml:"-"`
	Base              string            `yaml:"base"`
	URL               string            `yaml:"url"`
	Query             map[string]string `yaml:"query"`
	Items             string            `yaml:"items"`
	Fields            map[string]string `yaml:"fields"`
	Year              string            `yaml:"year"`
	URLPrefix         string            `yaml:"url_prefix"`
	PosterPrefix      string            `yaml:"poster_prefix"`
	MinIntervalMS     int               `yaml:"min_interval_ms"`
	Retries           int               `yaml:"retries"`
	ProviderTimeoutMS int               `yaml:"provider_timeout_ms"`
	Defer             bool              `yaml:"defer"`
	TypeParams        map[string]string `yaml:"type_params"`
	NFO               string            `yaml:"nfo"`
	UniqueID          string            `yaml:"uniqueid"`
	Episode           *Episode          `yaml:"episode"`
	Detail            *Episode          `yaml:"detail"`
	Catalog           *Catalog          `yaml:"catalog"`
}

type Episode struct {
	URL    string            `yaml:"url"`
	Query  map[string]string `yaml:"query"`
	Fields map[string]string `yaml:"fields"`
	Year   string            `yaml:"year"`
}

type Catalog struct {
	Seasons  *CatalogList `yaml:"seasons"`
	Episodes *CatalogList `yaml:"episodes"`
}

type CatalogList struct {
	URL          string            `yaml:"url"`
	Query        map[string]string `yaml:"query"`
	Items        string            `yaml:"items"`
	Fields       map[string]string `yaml:"fields"`
	Year         string            `yaml:"year"`
	PosterPrefix string            `yaml:"poster_prefix"`
}

func ExeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe), nil
}

func resolvePath(base, p, fallback string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		p = fallback
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

func Load(path string) (Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Config{}, fmt.Errorf("-config is required")
	}
	root, err := ExeDir()
	if err != nil {
		return Config{}, err
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
	cfg.ExeDir = root
	cfg.DataDir = resolvePath(root, cfg.DataDir, "data")
	if b, err := os.ReadFile(filepath.Join(cfg.DataDir, "config.yaml")); err == nil && len(b) > 0 {
		raw, err = merge(raw, b)
		if err != nil {
			return Config{}, err
		}
		cfg, err = decode(raw)
		if err != nil {
			return Config{}, err
		}
		cfg.ExeDir = root
		cfg.DataDir = resolvePath(root, cfg.DataDir, "data")
	}
	if strings.TrimSpace(cfg.BrowseRoot) == "" {
		cfg.BrowseRoot = cfg.DataDir
	} else {
		cfg.BrowseRoot = resolvePath(root, cfg.BrowseRoot, cfg.DataDir)
	}
	if cfg.Ranker != "llm" {
		cfg.Ranker = "embed"
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]Provider{}
	}
	cfg.ConfigPath = path
	cfg.clamp()
	if err := cfg.applyListen(); err != nil {
		return Config{}, err
	}
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

func (c Config) IngestPrompt() string {
	if c.ConfigPath != "" {
		p := filepath.Join(filepath.Dir(c.ConfigPath), "ingest.md")
		if b, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(b))) > 0 {
			return string(b)
		}
	}
	return fallbackIngestPrompt
}

func (c Config) IngestSampleRows() int {
	if c.Ingest.SampleRows < 1 {
		return 3
	}
	return c.Ingest.SampleRows
}

func (c Config) LlamaBinDir() string {
	return c.llamaPath(c.Llama.BinDir, "vendor/llama.cpp")
}

func (c Config) LlamaModelsDir() string {
	return c.llamaPath(c.Llama.ModelsDir, "vendor/llama.cpp/models")
}

func (c Config) llamaPath(rel, fallback string) string {
	base := c.ExeDir
	if base == "" {
		base, _ = ExeDir()
	}
	return resolvePath(base, rel, fallback)
}

func (c Config) LocalInstruct() bool {
	llm := strings.TrimSpace(c.Llama.LLMBaseURL)
	if llm == "" {
		return strings.TrimSpace(c.Llama.BaseURL) != ""
	}
	return originHost(llm) != "" && originHost(llm) == originHost(c.Llama.BaseURL)
}

func InstructFollowsListen(llm, probe string) bool {
	llm = strings.TrimSpace(llm)
	if llm == "" {
		return true
	}
	return originHost(llm) != "" && originHost(llm) == originHost(probe)
}

func (c Config) ChatBaseURL() string {
	if strings.TrimSpace(c.Llama.LLMBaseURL) == "" {
		return c.Llama.BaseURL
	}
	return c.Llama.LLMBaseURL
}

func (c Config) EmbedModel() string {
	if s := strings.TrimSpace(c.Llama.Embed); s != "" {
		return s
	}
	return modelStem(c.Llama.EmbedFile)
}

func (c Config) InstructModel() string {
	if s := strings.TrimSpace(c.Llama.Instruct); s != "" {
		return s
	}
	return modelStem(c.Llama.InstructFile)
}

func originHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Host)
}

func modelStem(file string) string {
	b := filepath.Base(strings.TrimSpace(file))
	return strings.TrimSuffix(b, ".gguf")
}

func (c Config) MatchWorkers() int {
	if c.Match.Workers < 1 {
		return 1
	}
	return c.Match.Workers
}

func (c Config) MatchSoloScore() float64 {
	if c.Match.SoloMinScore <= 0 {
		return c.Match.MinScore
	}
	return c.Match.SoloMinScore
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

func (c Config) MatchCooldown() ExpRange {
	return ClampExp(c.Match.Cooldown, 16, 19)
}

func (c Config) HTTPBackoff() ExpRange {
	return ClampExp(c.HTTP.Backoff, 10, 13)
}

func (c Config) LlamaProbeURL() string {
	return llamaHTTPURL(c.Llama.Host, c.Llama.Port)
}

func (c Config) LlamaVendorURL() string {
	return llamaHTTPURL("127.0.0.1", c.Llama.Port)
}

func llamaHTTPURL(host string, port int) string {
	return fmt.Sprintf("http://%s:%d/v1", host, port)
}

func (c *Config) applyListen() error {
	host := strings.TrimSpace(c.Llama.Host)
	port := c.Llama.Port
	if host == "" || port == 0 {
		if u, err := url.Parse(strings.TrimSpace(c.Llama.BaseURL)); err == nil && u.Host != "" {
			if host == "" {
				host = u.Hostname()
			}
			if port == 0 {
				if p := u.Port(); p != "" {
					n, err := strconv.Atoi(p)
					if err != nil {
						return fmt.Errorf("llama.base_url port: %w", err)
					}
					port = n
				}
			}
		}
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if port == 0 {
		port = 8080
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("llama.port %d out of range", port)
	}
	c.Llama.Host = host
	c.Llama.Port = port
	c.Llama.BaseURL = llamaHTTPURL(host, port)
	return nil
}

func (c *Config) clamp() {
	c.HTTP.Backoff = c.HTTPBackoff()
	c.Match.Cooldown = c.MatchCooldown()
}

func ClampExp(r ExpRange, defMin, defMax int) ExpRange {
	if r.MinExp == 0 && r.MaxExp == 0 {
		r.MinExp, r.MaxExp = defMin, defMax
	}
	if r.MinExp < 0 {
		r.MinExp = 0
	}
	if r.MaxExp < r.MinExp+2 {
		r.MaxExp = r.MinExp + 2
	}
	return r
}

func JitterExp(exp int) time.Duration {
	if exp < 1 {
		exp = 1
	}
	if exp > 62 {
		exp = 62
	}
	lo := int64(1) << (exp - 1)
	hi := int64(1) << exp
	n := lo
	if hi > lo {
		n = lo + rand.Int64N(hi-lo+1)
	}
	return time.Duration(n) * time.Millisecond
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

func SecretKeys(cfg Config) []string {
	seen := map[string]struct{}{}
	extra := make([]string, 0)
	for name, p := range cfg.Providers {
		k := strings.TrimSpace(p.Secret)
		if k == "" {
			if p.Require != "api_key" {
				continue
			}
			k = name
		}
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		extra = append(extra, k)
	}
	sort.Strings(extra)
	return extra
}

func SetSecrets(cfg *Config, updates map[string]string) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	allowed := map[string]struct{}{}
	for _, k := range SecretKeys(*cfg) {
		allowed[k] = struct{}{}
	}
	for k := range updates {
		if _, ok := allowed[k]; !ok {
			return fmt.Errorf("unknown secret key %q", k)
		}
	}
	keys, err := readSecretsMap(cfg.DataDir)
	if err != nil {
		return err
	}
	for k, v := range updates {
		v = strings.TrimSpace(v)
		if v == "" {
			delete(keys, k)
		} else {
			keys[k] = v
		}
	}
	return writeSecretsMap(cfg.DataDir, keys)
}

func SecretsStatus(cfg Config) map[string]bool {
	keys, err := readSecretsMap(cfg.DataDir)
	if err != nil {
		keys = map[string]string{}
	}
	out := make(map[string]bool, len(SecretKeys(cfg)))
	for _, k := range SecretKeys(cfg) {
		out[k] = strings.TrimSpace(keys[k]) != ""
	}
	return out
}

func secretsPath(dataDir string) string {
	return filepath.Join(dataDir, "secrets")
}

func readSecretsMap(dataDir string) (map[string]string, error) {
	b, err := os.ReadFile(secretsPath(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return map[string]string{}, nil
	}
	var keys map[string]string
	if err := yaml.Unmarshal(b, &keys); err != nil {
		return nil, err
	}
	if keys == nil {
		keys = map[string]string{}
	}
	return keys, nil
}

func writeSecretsMap(dataDir string, keys map[string]string) error {
	if keys == nil {
		keys = map[string]string{}
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(keys)
	if err != nil {
		return err
	}
	path := secretsPath(dataDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func applySecrets(cfg *Config) {
	for name, p := range cfg.Providers {
		p.APIKey = ""
		cfg.Providers[name] = p
	}
	keys, err := readSecretsMap(cfg.DataDir)
	if err != nil || len(keys) == 0 {
		return
	}
	for name, p := range cfg.Providers {
		keyName := name
		if s := strings.TrimSpace(p.Secret); s != "" {
			keyName = s
		}
		k, ok := keys[keyName]
		if !ok {
			continue
		}
		p.APIKey = strings.TrimSpace(k)
		cfg.Providers[name] = p
	}
}

func overlayPath(dataDir string) string {
	return filepath.Join(dataDir, "config.yaml")
}

func ReadOverlay(dataDir string) (map[string]any, error) {
	b, err := os.ReadFile(overlayPath(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return jsonMap(m), nil
}

func Overlay(cfg *Config, patch map[string]any) (map[string]any, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if patch == nil {
		return nil, fmt.Errorf("json object required")
	}
	cur, err := ReadOverlay(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	mergeMap(cur, patch)
	if err := coerceLlamaPort(cur); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	b, err := yaml.Marshal(cur)
	if err != nil {
		return nil, err
	}
	path := overlayPath(cfg.DataDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, err
	}
	return jsonMap(cur), nil
}

func coerceLlamaPort(m map[string]any) error {
	lm, ok := asMap(m["llama"])
	if !ok {
		return nil
	}
	m["llama"] = lm
	v, ok := lm["port"]
	if !ok || v == nil {
		return nil
	}
	n, err := intPort(v)
	if err != nil {
		return err
	}
	if n < 1 || n > 65535 {
		return fmt.Errorf("llama.port %d out of range", n)
	}
	lm["port"] = n
	return nil
}

func intPort(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case uint64:
		return int(n), nil
	case float64:
		if n != float64(int(n)) {
			return 0, fmt.Errorf("llama.port must be an integer")
		}
		return int(n), nil
	default:
		return 0, fmt.Errorf("llama.port invalid")
	}
}

func jsonMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = jsonValue(v)
	}
	return out
}

func jsonValue(v any) any {
	if m, ok := asMap(v); ok {
		return jsonMap(m)
	}
	return v
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
