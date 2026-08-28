package match

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
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
	childDirLine   = regexp.MustCompile(`(?m)^  - (.+?)/\s*(?:\(|$)`)
	spacedDash     = regexp.MustCompile(`\s+[-–—]\s+`)
)

type Cleaned struct {
	Title   string `json:"title"`
	Year    string `json:"year"`
	Type    string `json:"type"`
	Season  string `json:"season"`
	Episode string `json:"episode"`
}

type Grouped struct {
	Cleaned
	Files []JobFile
}

func Cleanup(ctx context.Context, cfg config.Config, raw, parent string) Cleaned {
	fallback := Cleaned{Title: strings.TrimSpace(raw)}
	if fallback.Title == "" {
		fallback.Title = raw
	}
	base := cfg.ChatBaseURL()
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
	if id := cfg.InstructModel(); id != "" {
		payload["model"] = id
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

func Group(ctx context.Context, cfg config.Config, listing string, files []JobFile) []Grouped {
	if strings.TrimSpace(listing) == "" {
		return nil
	}
	base := cfg.ChatBaseURL()
	if base == "" {
		return hintShows(listing, files)
	}
	system := strings.TrimSpace(cfg.Prompt())
	if system == "" {
		system = "You group video library files into unique titles. Return JSON only."
	}
	limit := 256
	if len(files) > 0 {
		limit = 2048
	}
	raw, err := chatJSONLimit(ctx, cfg, system, groupUser(listing, files), limit)
	if err != nil {
		return hintShows(listing, files)
	}
	return parseShows(raw, listing, files)
}

func groupUser(listing string, files []JobFile) string {
	if len(files) == 0 {
		return listing
	}
	var b strings.Builder
	b.WriteString(listing)
	if listing != "" && !strings.HasSuffix(listing, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("Files:\n")
	for _, f := range files {
		if f.Path == "" {
			continue
		}
		b.WriteString("  - ")
		b.WriteString(f.Path)
		b.WriteByte('\n')
	}
	return b.String()
}

func chatJSON(ctx context.Context, cfg config.Config, system, user string) ([]byte, error) {
	return chatJSONLimit(ctx, cfg, system, user, 256)
}

func chatJSONLimit(ctx context.Context, cfg config.Config, system, user string, maxTokens int) ([]byte, error) {
	httpc := newHTTP(cfg)
	payload := map[string]any{
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature":     0,
		"max_tokens":      maxTokens,
		"response_format": map[string]string{"type": "json_object"},
	}
	if id := cfg.InstructModel(); id != "" {
		payload["model"] = id
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.JoinPath(strings.TrimRight(cfg.ChatBaseURL(), "/"), "chat/completions")
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

func stripJSON(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func parseShows(raw []byte, listing string, files []JobFile) []Grouped {
	s := stripJSON(raw)
	var wrap struct {
		Shows []showJSON `json:"shows"`
	}
	if err := json.Unmarshal([]byte(s), &wrap); err != nil {
		return hintShows(listing, files)
	}
	out := make([]Grouped, 0, len(wrap.Shows))
	seen := map[string]int{}
	for _, in := range wrap.Shows {
		got := sanitizeGrouped(in.cleaned(), listing)
		if got.Title == "" {
			continue
		}
		key := strings.ToLower(got.Title) + "\t" + got.Year
		labeled := listedFiles(in.Files, files)
		if i, ok := seen[key]; ok {
			out[i].Files = appendJobFiles(out[i].Files, labeled)
			continue
		}
		seen[key] = len(out)
		out = append(out, Grouped{Cleaned: got, Files: labeled})
	}
	if len(out) == 0 {
		return hintShows(listing, files)
	}
	return out
}

type showJSON struct {
	Title   string          `json:"title"`
	Year    json.RawMessage `json:"year"`
	Type    string          `json:"type"`
	Season  string          `json:"season"`
	Episode string          `json:"episode"`
	Files   []fileJSON      `json:"files"`
}

type fileJSON struct {
	Path    string `json:"path"`
	Season  string `json:"season"`
	Episode string `json:"episode"`
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

func hintShows(listing string, files []JobFile) []Grouped {
	hint := listingHint(listing)
	if hint == "" {
		return nil
	}
	got := sanitizeGrouped(Cleaned{Title: hint}, listing)
	if got.Title == "" {
		return nil
	}
	return []Grouped{{Cleaned: got, Files: blankFiles(files)}}
}

func listedFiles(in []fileJSON, allowed []JobFile) []JobFile {
	if len(in) == 0 || len(allowed) == 0 {
		return nil
	}
	byPath := map[string]string{}
	for _, f := range allowed {
		if f.Path == "" {
			continue
		}
		byPath[f.Path] = f.Path
		base := filepath.Base(f.Path)
		if _, ok := byPath[base]; !ok {
			byPath[base] = f.Path
		}
	}
	var out []JobFile
	seen := map[string]bool{}
	for _, row := range in {
		p := strings.TrimSpace(row.Path)
		canon, ok := byPath[p]
		if !ok {
			canon, ok = byPath[filepath.Base(p)]
		}
		if !ok || seen[canon] {
			continue
		}
		seen[canon] = true
		out = append(out, JobFile{
			Path:    canon,
			Season:  strings.TrimSpace(row.Season),
			Episode: strings.TrimSpace(row.Episode),
		})
	}
	return out
}

func blankFiles(files []JobFile) []JobFile {
	out := make([]JobFile, 0, len(files))
	for _, f := range files {
		if f.Path == "" {
			continue
		}
		out = append(out, JobFile{Path: f.Path})
	}
	return out
}

func appendJobFiles(dst, extra []JobFile) []JobFile {
	seen := map[string]bool{}
	for _, f := range dst {
		seen[f.Path] = true
	}
	for _, f := range extra {
		if f.Path == "" || seen[f.Path] {
			continue
		}
		seen[f.Path] = true
		dst = append(dst, f)
	}
	return dst
}

func MergeJobs(jobs []Job) []Job {
	if len(jobs) < 2 {
		return jobs
	}
	out := make([]Job, 0, len(jobs))
	idx := map[string]int{}
	for _, j := range jobs {
		key := foldTitle(j.Title) + "\t" + strings.TrimSpace(j.Year)
		if i, ok := idx[key]; ok {
			out[i].Files = appendJobFiles(out[i].Files, j.Files)
			out[i].Path = mergeJobPath(out[i].Path, j.Path)
			continue
		}
		idx[key] = len(out)
		out = append(out, j)
	}
	return out
}

func mergeJobPath(a, b string) string {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == "" {
		return b
	}
	if b == "" || a == b {
		return a
	}
	sep := string(filepath.Separator)
	if strings.HasPrefix(b, a+sep) {
		return a
	}
	if strings.HasPrefix(a, b+sep) {
		return b
	}
	da, db := filepath.Dir(a), filepath.Dir(b)
	if da == db {
		return da
	}
	return a
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
	if hint != "" && (isSeasonName(c.Title) || seasonOnlyTree(listing) && !sameTitle(c.Title, hint)) {
		c.Title = strings.TrimSpace(spacedDash.ReplaceAllString(hint, ": "))
	}
	return c
}

func yearInListing(listing, year string) bool {
	if listing == "" || !yearOnly.MatchString(year) {
		return false
	}
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(year) + `\b`).MatchString(listing)
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

func listingChildDirs(listing string) []string {
	matches := childDirLine.FindAllStringSubmatch(listing, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func seasonOnlyTree(listing string) bool {
	dirs := listingChildDirs(listing)
	if len(dirs) == 0 {
		return false
	}
	for _, d := range dirs {
		if !isSeasonName(d) && !isExtrasName(d) {
			return false
		}
	}
	return true
}

func isSeasonName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(strings.TrimRight(strings.TrimSpace(name), "/")))
	if strings.HasPrefix(n, "season") {
		rest := strings.TrimLeft(n[len("season"):], " ._-")
		return rest != "" && isDigits(rest)
	}
	if len(n) >= 2 && n[0] == 's' && isDigits(n[1:]) {
		return true
	}
	return false
}

func isExtrasName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(strings.TrimRight(strings.TrimSpace(name), "/")))
	switch n {
	case "behind the scenes", "deleted scenes", "trailers", "interviews",
		"scenes", "featurettes", "shorts", "other", "extras":
		return true
	default:
		return false
	}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func sameTitle(a, b string) bool {
	return foldTitle(a) == foldTitle(b)
}

func foldTitle(s string) string {
	return strings.ToLower(strings.TrimSpace(spacedDash.ReplaceAllString(s, ": ")))
}

func listingHint(listing string) string {
	folder, parent, file := "", "", ""
	if m := folderLine.FindStringSubmatch(listing); len(m) == 2 {
		folder = strings.TrimSpace(m[1])
	}
	if m := parentLine.FindStringSubmatch(listing); len(m) == 2 {
		parent = strings.TrimSpace(m[1])
	}
	if m := fileLine.FindStringSubmatch(listing); len(m) == 2 {
		file = strings.TrimSpace(m[1])
	}
	if folder != "" && isSeasonName(folder) && parent != "" {
		return parent
	}
	if folder != "" {
		return folder
	}
	if parent != "" {
		return parent
	}
	return file
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
