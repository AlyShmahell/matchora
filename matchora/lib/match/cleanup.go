package match

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/alyshmahell/matchora/lib/config"
)

var (
	yearSuffix     = regexp.MustCompile(`\s*\((\d{4})\)\s*$`)
	yearOnly       = regexp.MustCompile(`^\d{4}$`)
	groupTag       = regexp.MustCompile(`^\[[^\]]+\]\s*`)
	bracketTag     = regexp.MustCompile(`\[([^\]]+)\]`)
	episodeInTitle = regexp.MustCompile(`(?i)s\d{1,2}e\d{1,3}`)
	videoExt       = regexp.MustCompile(`(?i)\.(mkv|mp4|avi|m4v|ts|m2ts)$`)
	trailingType   = regexp.MustCompile(`(?i)[\s,;:/\-|]*\(?\b(?:anime|tv|movie)\)?\s*$`)
	folderLine     = regexp.MustCompile(`(?m)^Folder:\s*(.+)$`)
	fileLine       = regexp.MustCompile(`(?m)^File:\s*(.+)$`)
	parentLine     = regexp.MustCompile(`(?m)^Parent:\s*(.+)$`)
	spacedDash     = regexp.MustCompile(`\s+[-–—]\s+`)
)

type Cleaned struct {
	Title   string `json:"title"`
	Year    string `json:"year"`
	Type    string `json:"type"`
	Season  string `json:"season"`
	Episode string `json:"episode"`
}

func Cleanup(ctx context.Context, cfg config.Config, raw, parent string) Cleaned {
	fallback := Cleaned{Title: strings.TrimSpace(raw)}
	if fallback.Title == "" {
		fallback.Title = raw
	}
	base := cfg.Llama.LLMBaseURL
	if base == "" {
		return fallback
	}
	httpc := newHTTP(cfg)
	var bld strings.Builder
	if parent != "" {
		bld.WriteString("Parent folder: ")
		bld.WriteString(parent)
		bld.WriteByte('\n')
	}
	bld.WriteString("Name: ")
	bld.WriteString(raw)
	bld.WriteString("\nReturn JSON {\"title\":\"\",\"year\":\"\",\"season\":\"\",\"episode\":\"\"} only.")
	payload := map[string]any{
		"messages": []map[string]string{
			{"role": "system", "content": "You extract media titles from file and folder names. Return JSON only."},
			{"role": "user", "content": bld.String()},
		},
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
	}
	if cfg.Llama.Model != "" {
		payload["model"] = cfg.Llama.Model
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fallback
	}
	endpoint, err := url.JoinPath(strings.TrimRight(base, "/"), "chat/completions")
	if err != nil {
		return fallback
	}
	b, code, err := httpc.post(ctx, endpoint, "application/json", bytes.NewReader(body))
	if err != nil || code >= 400 {
		return fallback
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &resp); err != nil || len(resp.Choices) == 0 {
		return fallback
	}
	var got Cleaned
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &got); err != nil {
		return fallback
	}
	got.Title = strings.TrimSpace(got.Title)
	got.Year = strings.TrimSpace(got.Year)
	got.Season = strings.TrimSpace(got.Season)
	got.Episode = strings.TrimSpace(got.Episode)
	if got.Title == "" {
		return fallback
	}
	got = normalize(got)
	got.Type = ""
	return got
}

func Group(ctx context.Context, cfg config.Config, listing string) []Cleaned {
	if strings.TrimSpace(listing) == "" {
		return nil
	}
	base := cfg.Llama.LLMBaseURL
	if base == "" {
		return nil
	}
	system := strings.TrimSpace(cfg.Prompt())
	if system == "" {
		system = "You group video library files into unique titles. Return JSON only."
	}
	raw, err := chatJSON(ctx, cfg, system, listing)
	if err != nil {
		return hintShows(listing)
	}
	return parseShows(raw, listing)
}

func chatJSON(ctx context.Context, cfg config.Config, system, user string) ([]byte, error) {
	httpc := newHTTP(cfg)
	payload := map[string]any{
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature":     0,
		"max_tokens":      256,
		"response_format": map[string]string{"type": "json_object"},
	}
	if cfg.Llama.Model != "" {
		payload["model"] = cfg.Llama.Model
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.JoinPath(strings.TrimRight(cfg.Llama.LLMBaseURL, "/"), "chat/completions")
	if err != nil {
		return nil, err
	}
	b, code, err := httpc.post(ctx, endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("status %d", code)
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &resp); err != nil || len(resp.Choices) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("empty chat")
	}
	return []byte(resp.Choices[0].Message.Content), nil
}

func parseShows(raw []byte, listing string) []Cleaned {
	s := strings.TrimSpace(string(raw))
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	var wrap struct {
		Shows []showJSON `json:"shows"`
	}
	if err := json.Unmarshal([]byte(s), &wrap); err != nil {
		return hintShows(listing)
	}
	out := make([]Cleaned, 0, len(wrap.Shows))
	seen := map[string]bool{}
	for _, in := range wrap.Shows {
		got := sanitizeGrouped(in.cleaned(), listing)
		if got.Title == "" {
			continue
		}
		key := strings.ToLower(got.Title) + "\t" + got.Year
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, got)
	}
	if len(out) == 0 {
		return hintShows(listing)
	}
	return out
}

type showJSON struct {
	Title   string          `json:"title"`
	Year    json.RawMessage `json:"year"`
	Type    string          `json:"type"`
	Season  string          `json:"season"`
	Episode string          `json:"episode"`
}

func (s showJSON) cleaned() Cleaned {
	return Cleaned{
		Title:   s.Title,
		Year:    decodeYear(s.Year),
		Type:    s.Type,
		Season:  s.Season,
		Episode: s.Episode,
	}
}

func decodeYear(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	return ""
}

func hintShows(listing string) []Cleaned {
	hint := listingHint(listing)
	if hint == "" {
		return nil
	}
	got := sanitizeGrouped(Cleaned{Title: hint}, listing)
	if got.Title == "" {
		return nil
	}
	return []Cleaned{got}
}

func sanitizeGrouped(c Cleaned, listing string) Cleaned {
	c = normalize(c)
	c.Type = ""
	c.Season = ""
	c.Episode = ""
	c.Title = strings.TrimSpace(groupTag.ReplaceAllString(c.Title, ""))
	c.Title = strings.TrimSpace(trailingType.ReplaceAllString(c.Title, ""))
	if !yearOnly.MatchString(c.Year) {
		c.Year = ""
	}
	hint := listingHint(listing)
	lower := strings.ToLower(c.Title)
	for _, tag := range listingTags(listing) {
		tl := strings.ToLower(tag)
		if lower == tl {
			c.Title = hint
			lower = strings.ToLower(c.Title)
			break
		}
		if strings.HasPrefix(lower, tl+" ") {
			c.Title = strings.TrimSpace(c.Title[len(tag):])
			lower = strings.ToLower(c.Title)
		}
	}
	c.Title = strings.TrimSpace(c.Title)
	if c.Title == "" || videoExt.MatchString(c.Title) || episodeInTitle.MatchString(c.Title) {
		if hint != "" && !videoExt.MatchString(hint) && !episodeInTitle.MatchString(hint) {
			c.Title = hint
		} else {
			c.Title = ""
		}
	}
	c.Title = strings.TrimSpace(spacedDash.ReplaceAllString(c.Title, ": "))
	if c.Year != "" && !yearInListing(listing, c.Year) {
		c.Year = ""
	}
	return c
}

func yearInListing(listing, year string) bool {
	if listing == "" || !yearOnly.MatchString(year) {
		return false
	}
	return regexp.MustCompile(`\b`+regexp.QuoteMeta(year)+`\b`).MatchString(listing)
}

func listingTags(listing string) []string {
	matches := bracketTag.FindAllStringSubmatch(listing, -1)
	var tags []string
	seen := map[string]bool{}
	for _, m := range matches {
		t := strings.TrimSpace(m[1])
		if t == "" {
			continue
		}
		k := strings.ToLower(t)
		if seen[k] {
			continue
		}
		seen[k] = true
		tags = append(tags, t)
	}
	return tags
}

func listingHint(listing string) string {
	if m := folderLine.FindStringSubmatch(listing); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	if m := parentLine.FindStringSubmatch(listing); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	if m := fileLine.FindStringSubmatch(listing); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func normalize(c Cleaned) Cleaned {
	c.Title = strings.TrimSpace(c.Title)
	c.Year = strings.TrimSpace(c.Year)
	c.Type = strings.ToLower(strings.TrimSpace(c.Type))
	c.Season = strings.TrimSpace(c.Season)
	c.Episode = strings.TrimSpace(c.Episode)
	if m := yearSuffix.FindStringSubmatch(c.Title); len(m) == 2 {
		if c.Year == "" || c.Year == m[1] {
			c.Year = m[1]
			c.Title = strings.TrimSpace(yearSuffix.ReplaceAllString(c.Title, ""))
		}
	}
	c.Type = normalizeType(c.Type, c.Title)
	return c
}

func normalizeType(t, title string) string {
	switch t {
	case "anime", "tv", "movie":
		return t
	}
	var valid []string
	seen := map[string]bool{}
	for _, p := range strings.FieldsFunc(t, func(r rune) bool {
		return r == '|' || r == '/' || r == ',' || r == ' '
	}) {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "anime" && p != "tv" && p != "movie" {
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		valid = append(valid, p)
	}
	if len(valid) == 1 {
		return valid[0]
	}
	if looksLikeFilm(title) {
		return "movie"
	}
	return ""
}

func looksLikeFilm(title string) bool {
	n := strings.ToLower(title)
	return strings.Contains(n, "movie") || strings.Contains(n, "film") || strings.Contains(n, "gekijou")
}
