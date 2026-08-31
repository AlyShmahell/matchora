package config

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const runOverlay = "/run/matchora/config.yaml"

type Config struct {
	HTTP       HTTP                `yaml:"http"`
	DataDir    string              `yaml:"data_dir"`
	BrowseRoot string              `yaml:"browse_root"`
	Version    string              `yaml:"version"`
	Match      Match               `yaml:"match"`
	Session    Session             `yaml:"session"`
	Group      Group               `yaml:"group"`
	Ingest     Ingest              `yaml:"ingest"`
	Providers  map[string]Provider `yaml:"providers"`
	ConfigPath string              `yaml:"-"`
	ExeDir     string              `yaml:"-"`
}

type Group struct {
	SeqThreshold float64  `yaml:"seq_threshold"`
	VideoExt     []string `yaml:"video_ext"`
	Extras       []string `yaml:"extras"`
	Release      []string `yaml:"release"`
	Kinds        []string `yaml:"kinds"`
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

const SessionTTLMax = 24 * time.Hour

type Session struct {
	TTLMS int `yaml:"ttl_ms"`
}

func (c Config) SessionTTL() time.Duration {
	if c.Session.TTLMS <= 0 {
		return SessionTTLMax
	}
	d := time.Duration(c.Session.TTLMS) * time.Millisecond
	if d > SessionTTLMax {
		return SessionTTLMax
	}
	return d
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
	if cfg.Providers == nil {
		cfg.Providers = map[string]Provider{}
	}
	cfg.ConfigPath = path
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	applySecrets(&cfg)
	return cfg, nil
}

func Validate(c Config) error {
	if strings.TrimSpace(c.Version) == "" {
		return fmt.Errorf("version is empty")
	}
	if strings.TrimSpace(c.HTTP.Addr) == "" {
		return fmt.Errorf("http.addr is empty")
	}
	if c.HTTP.TimeoutMS <= 0 {
		return fmt.Errorf("http.timeout_ms must be > 0")
	}
	if c.HTTP.Retries <= 0 {
		return fmt.Errorf("http.retries must be > 0")
	}
	if c.HTTP.ProviderTimeoutMS <= 0 {
		return fmt.Errorf("http.provider_timeout_ms must be > 0")
	}
	if err := validateExp("http.backoff", c.HTTP.Backoff); err != nil {
		return err
	}
	if c.Session.TTLMS <= 0 {
		return fmt.Errorf("session.ttl_ms must be > 0")
	}
	if c.Match.MinScore <= 0 {
		return fmt.Errorf("match.min_score must be > 0")
	}
	if c.Match.MinMargin < 0 {
		return fmt.Errorf("match.min_margin must be >= 0")
	}
	if c.Match.MinHits < 1 {
		return fmt.Errorf("match.min_hits must be >= 1")
	}
	if c.Match.Workers < 1 {
		return fmt.Errorf("match.workers must be >= 1")
	}
	if c.Match.CooldownFails < 0 {
		return fmt.Errorf("match.cooldown_fails must be >= 0")
	}
	if err := validateExp("match.cooldown", c.Match.Cooldown); err != nil {
		return err
	}
	if c.Group.SeqThreshold <= 0 || c.Group.SeqThreshold > 1 {
		return fmt.Errorf("group.seq_threshold must be in (0, 1]")
	}
	if len(wordList(c.Group.VideoExt)) == 0 {
		return fmt.Errorf("group.video_ext is empty")
	}
	if len(wordList(c.Group.Extras)) == 0 {
		return fmt.Errorf("group.extras is empty")
	}
	if len(wordList(c.Group.Release)) == 0 {
		return fmt.Errorf("group.release is empty")
	}
	if len(wordList(c.Group.Kinds)) == 0 {
		return fmt.Errorf("group.kinds is empty")
	}
	if c.Ingest.SampleRows < 1 {
		return fmt.Errorf("ingest.sample_rows must be >= 1")
	}
	return nil
}

func validateExp(name string, r ExpRange) error {
	if r.MinExp < 0 {
		return fmt.Errorf("%s.min_exp must be >= 0", name)
	}
	if r.MaxExp < r.MinExp+2 {
		return fmt.Errorf("%s.max_exp must be >= min_exp+2", name)
	}
	return nil
}

func (c Config) IngestSampleRows() int {
	return c.Ingest.SampleRows
}

func (c Config) SeqThreshold() float64 {
	return c.Group.SeqThreshold
}

func (c Config) GroupVideoExt() map[string]struct{} {
	return extSet(c.Group.VideoExt)
}

func (c Config) GroupExtras() map[string]struct{} {
	return wordSet(c.Group.Extras)
}

func (c Config) GroupRelease() map[string]struct{} {
	return wordSet(c.Group.Release)
}

func (c Config) GroupKinds() map[string]struct{} {
	return wordSet(c.Group.Kinds)
}

func wordList(list []string) []string {
	out := make([]string, 0, len(list))
	for _, w := range list {
		w = strings.ToLower(strings.TrimSpace(w))
		if w == "" {
			continue
		}
		out = append(out, w)
	}
	return out
}

func extSet(list []string) map[string]struct{} {
	out := make(map[string]struct{}, len(list))
	for _, e := range wordList(list) {
		e = strings.TrimPrefix(e, ".")
		if e == "" {
			continue
		}
		out["."+e] = struct{}{}
	}
	return out
}

func wordSet(list []string) map[string]struct{} {
	out := make(map[string]struct{}, len(list))
	for _, w := range wordList(list) {
		out[w] = struct{}{}
	}
	return out
}

func (c Config) MatchWorkers() int {
	return c.Match.Workers
}

func (c Config) MatchSoloScore() float64 {
	if c.Match.SoloMinScore <= 0 {
		return c.Match.MinScore
	}
	return c.Match.SoloMinScore
}

func (c Config) MatchMinHits() int {
	return c.Match.MinHits
}

func (c Config) MatchCooldownFails() int {
	return c.Match.CooldownFails
}

func (c Config) MatchCooldown() ExpRange {
	return c.Match.Cooldown
}

func (c Config) HTTPBackoff() ExpRange {
	return c.HTTP.Backoff
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
	return time.Duration(c.HTTP.TimeoutMS) * time.Millisecond
}

func (c Config) ProviderTimeout() time.Duration {
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

func validateOverlay(cfg Config, overlay map[string]any) error {
	base, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		return err
	}
	over, err := yaml.Marshal(overlay)
	if err != nil {
		return err
	}
	raw, err := merge(base, over)
	if err != nil {
		return err
	}
	decoded, err := decode(raw)
	if err != nil {
		return err
	}
	return Validate(decoded)
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
	if err := validateOverlay(*cfg, cur); err != nil {
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
