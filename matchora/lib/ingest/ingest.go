package ingest

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
)

type Row struct {
	Title   string `json:"title"`
	Year    string `json:"year,omitempty"`
	Type    string `json:"type,omitempty"`
	Season  string `json:"season,omitempty"`
	Episode string `json:"episode,omitempty"`
	IMDB    string `json:"imdb,omitempty"`
}

func Parse(r io.Reader, name, contentType string) ([]Row, error) {
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
		return parseJSON(raw)
	default:
		return parseCSV(raw)
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

func parseJSON(raw []byte) ([]Row, error) {
	var rows []Row
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}
	return validate(rows)
}

func parseCSV(raw []byte) ([]Row, error) {
	cr := csv.NewReader(bytes.NewReader(raw))
	cr.TrimLeadingSpace = true
	recs, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv: %w", err)
	}
	if len(recs) < 2 {
		return nil, fmt.Errorf("csv: need a header and at least one row")
	}
	idx := map[string]int{}
	for i, h := range recs[0] {
		idx[normHeader(h)] = i
	}
	if _, ok := idx["title"]; !ok {
		return nil, fmt.Errorf("csv: missing title column")
	}
	var rows []Row
	for _, rec := range recs[1:] {
		rows = append(rows, Row{
			Title:   cell(rec, idx, "title"),
			Year:    cell(rec, idx, "year"),
			Type:    cell(rec, idx, "type"),
			Season:  cell(rec, idx, "season"),
			Episode: cell(rec, idx, "episode"),
			IMDB:    cell(rec, idx, "imdb"),
		})
	}
	return validate(rows)
}

func validate(rows []Row) ([]Row, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("no rows")
	}
	out := make([]Row, 0, len(rows))
	for i, row := range rows {
		row.Title = strings.TrimSpace(row.Title)
		row.Year = strings.TrimSpace(row.Year)
		row.Type = strings.ToLower(strings.TrimSpace(row.Type))
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

func cell(rec []string, idx map[string]int, key string) string {
	i, ok := idx[key]
	if !ok || i >= len(rec) {
		return ""
	}
	return rec[i]
}

func normHeader(h string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, h)
}
