package ingest

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"github.com/alyshmahell/matchora/lib/config"
)

var fields = []string{"title", "year", "type", "season", "episode", "imdb"}

type Row struct {
	Title   string `json:"title"`
	Year    string `json:"year,omitempty"`
	Type    string `json:"type,omitempty"`
	Season  string `json:"season,omitempty"`
	Episode string `json:"episode,omitempty"`
	IMDB    string `json:"imdb,omitempty"`
}

func Parse(ctx context.Context, cfg config.Config, r io.Reader, name, contentType string) ([]Row, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty payload")
	}
	switch detect(raw, name, contentType) {
	case "json":
		return parseJSON(cfg, raw)
	default:
		return parseCSV(ctx, cfg, raw)
	}
}

func detect(raw []byte, name, contentType string) string {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "json") {
		return "json"
	}
	if strings.Contains(ct, "csv") {
		return "csv"
	}
	n := strings.ToLower(name)
	if strings.HasSuffix(n, ".json") {
		return "json"
	}
	if strings.HasSuffix(n, ".csv") {
		return "csv"
	}
	if raw[0] == '[' {
		return "json"
	}
	return "csv"
}

func parseJSON(cfg config.Config, raw []byte) ([]Row, error) {
	var rows []Row
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}
	return validate(cfg, rows)
}

func parseCSV(ctx context.Context, cfg config.Config, raw []byte) ([]Row, error) {
	cr := csv.NewReader(bytes.NewReader(raw))
	cr.TrimLeadingSpace = true
	recs, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv: %w", err)
	}
	if len(recs) < 2 {
		return nil, fmt.Errorf("csv: need a header and at least one row")
	}
	headers := recs[0]
	cmap := deterministicMap(headers, cfg.Ingest.Aliases)
	if _, ok := cmap["title"]; !ok {
		if mapped, err := askColumns(ctx, cfg, headers, recs[1:]); err == nil {
			mergeColumns(cmap, headers, mapped)
		}
	}
	if _, ok := cmap["title"]; !ok {
		return nil, fmt.Errorf("csv: missing title column")
	}
	var rows []Row
	for _, rec := range recs[1:] {
		rows = append(rows, Row{
			Title:   cell(rec, cmap, "title"),
			Year:    cell(rec, cmap, "year"),
			Type:    cell(rec, cmap, "type"),
			Season:  cell(rec, cmap, "season"),
			Episode: cell(rec, cmap, "episode"),
			IMDB:    cell(rec, cmap, "imdb"),
		})
	}
	return validate(cfg, rows)
}

func deterministicMap(headers []string, aliases map[string]string) map[string]int {
	idx := headerIndex(headers)
	out := map[string]int{}
	for _, f := range fields {
		if i, ok := idx[normHeader(f)]; ok {
			out[f] = i
		}
	}
	aliasToField := map[string]string{}
	for alias, field := range aliases {
		field = strings.ToLower(strings.TrimSpace(field))
		if !isField(field) {
			continue
		}
		aliasToField[normKey(alias)] = field
	}
	for i, h := range headers {
		field, ok := aliasToField[normKey(h)]
		if !ok {
			continue
		}
		if _, taken := out[field]; taken {
			continue
		}
		out[field] = i
	}
	return out
}

func headerIndex(headers []string) map[string]int {
	idx := map[string]int{}
	for i, h := range headers {
		idx[normHeader(h)] = i
	}
	return idx
}

func mergeColumns(cmap map[string]int, headers []string, mapped map[string]string) {
	idx := headerIndex(headers)
	byKey := map[string]int{}
	for i, h := range headers {
		byKey[normKey(h)] = i
	}
	for field, src := range mapped {
		field = strings.ToLower(strings.TrimSpace(field))
		src = strings.TrimSpace(src)
		if !isField(field) || src == "" {
			continue
		}
		i, ok := idx[normHeader(src)]
		if !ok {
			i, ok = byKey[normKey(src)]
		}
		if !ok {
			continue
		}
		cmap[field] = i
	}
}

func askColumns(ctx context.Context, cfg config.Config, headers []string, rows [][]string) (map[string]string, error) {
	base := strings.TrimSpace(cfg.ChatBaseURL())
	if base == "" {
		return nil, fmt.Errorf("no instruct")
	}
	n := cfg.IngestSampleRows()
	if n > len(rows) {
		n = len(rows)
	}
	system := strings.TrimSpace(cfg.IngestPrompt())
	if hints := aliasHints(cfg.Ingest.Aliases); hints != "" {
		system = strings.TrimSpace(system + "\n\n" + hints)
	}
	raw, err := chatJSON(ctx, cfg, system, columnUser(headers, rows[:n]))
	if err != nil {
		return nil, err
	}
	return parseColumns(raw)
}

func aliasHints(aliases map[string]string) string {
	if len(aliases) == 0 {
		return ""
	}
	parts := make([]string, 0, len(aliases))
	for alias, field := range aliases {
		alias = strings.TrimSpace(alias)
		field = strings.TrimSpace(field)
		if alias == "" || field == "" {
			continue
		}
		parts = append(parts, alias+" often means "+field)
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return "Hints: " + strings.Join(parts, "; ") + "."
}

func columnUser(headers []string, samples [][]string) string {
	var b strings.Builder
	b.WriteString("Header: ")
	b.WriteString(strings.Join(headers, ","))
	b.WriteByte('\n')
	if len(samples) == 0 {
		return b.String()
	}
	b.WriteString("Samples:\n")
	for _, rec := range samples {
		b.WriteString(strings.Join(rec, ","))
		b.WriteByte('\n')
	}
	return b.String()
}

func parseColumns(raw []byte) (map[string]string, error) {
	s := strings.TrimSpace(string(raw))
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	var wrap struct {
		Columns map[string]string `json:"columns"`
	}
	if err := json.Unmarshal([]byte(s), &wrap); err != nil {
		return nil, err
	}
	if len(wrap.Columns) == 0 {
		return nil, fmt.Errorf("empty columns")
	}
	return wrap.Columns, nil
}

func chatJSON(ctx context.Context, cfg config.Config, system, user string) ([]byte, error) {
	payload := map[string]any{
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature":     0,
		"max_tokens":      256,
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: cfg.HTTPTimeout()}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("empty chat")
	}
	return []byte(out.Choices[0].Message.Content), nil
}

func validate(cfg config.Config, rows []Row) ([]Row, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("no rows")
	}
	out := make([]Row, 0, len(rows))
	for i, row := range rows {
		row.Title = strings.TrimSpace(row.Title)
		row.Year = strings.TrimSpace(row.Year)
		row.Type = normalizeType(cfg, row.Type)
		row.Season = strings.TrimSpace(row.Season)
		row.Episode = strings.TrimSpace(row.Episode)
		row.IMDB = strings.TrimSpace(row.IMDB)
		if row.Title == "" {
			return nil, fmt.Errorf("row %d: title is required", i+1)
		}
		out = append(out, row)
	}
	return out, nil
}

func normalizeType(cfg config.Config, typ string) string {
	t := strings.ToLower(strings.TrimSpace(typ))
	if t == "" || len(cfg.Ingest.Types) == 0 {
		return t
	}
	if mapped, ok := cfg.Ingest.Types[t]; ok {
		return strings.ToLower(strings.TrimSpace(mapped))
	}
	key := normKey(t)
	for raw, mapped := range cfg.Ingest.Types {
		if normKey(raw) == key {
			return strings.ToLower(strings.TrimSpace(mapped))
		}
	}
	return t
}

func cell(rec []string, idx map[string]int, key string) string {
	i, ok := idx[key]
	if !ok || i >= len(rec) {
		return ""
	}
	return rec[i]
}

func isField(s string) bool {
	for _, f := range fields {
		if f == s {
			return true
		}
	}
	return false
}

func normHeader(h string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, h)
}

func normKey(h string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, h)
}
