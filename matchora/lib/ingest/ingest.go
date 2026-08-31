package ingest

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"github.com/alyshmahell/matchora/lib/config"
	"github.com/alyshmahell/matchora/lib/match"
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
		return parseCSV(cfg, raw)
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

func parseCSV(cfg config.Config, raw []byte) ([]Row, error) {
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
		mergeColumns(cmap, headers, seqMapHeaders(headers, cmap, cfg))
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

type seqCand struct {
	field string
	src   string
	score float64
}

func seqMapHeaders(headers []string, cmap map[string]int, cfg config.Config) map[string]string {
	takenField := map[string]bool{}
	takenIdx := map[int]bool{}
	for f, i := range cmap {
		takenField[f] = true
		takenIdx[i] = true
	}
	type target struct{ name, field string }
	var targets []target
	for _, f := range fields {
		targets = append(targets, target{name: f, field: f})
	}
	for alias, field := range cfg.Ingest.Aliases {
		field = strings.ToLower(strings.TrimSpace(field))
		if !isField(field) {
			continue
		}
		targets = append(targets, target{name: alias, field: field})
	}
	thresh := cfg.SeqThreshold()
	var cands []seqCand
	for i, h := range headers {
		if takenIdx[i] {
			continue
		}
		hn := strings.ToLower(strings.TrimSpace(h))
		if hn == "" {
			continue
		}
		for _, t := range targets {
			if takenField[t.field] {
				continue
			}
			r := match.SeqRatio(hn, strings.ToLower(strings.TrimSpace(t.name)))
			if r >= thresh {
				cands = append(cands, seqCand{field: t.field, src: h, score: r})
			}
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		if cands[i].field != cands[j].field {
			return cands[i].field < cands[j].field
		}
		return cands[i].src < cands[j].src
	})
	mapped := map[string]string{}
	usedSrc := map[string]bool{}
	for _, c := range cands {
		if takenField[c.field] || usedSrc[c.src] {
			continue
		}
		mapped[c.field] = c.src
		takenField[c.field] = true
		usedSrc[c.src] = true
	}
	return mapped
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
