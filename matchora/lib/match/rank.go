package match

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"unicode"

	"github.com/alyshmahell/matchora/lib/config"
)

func rank(ctx context.Context, cfg config.Config, httpc *httpClient, query string, cands []Candidate) (string, []Candidate, error) {
	if len(cands) == 0 {
		return "", cands, nil
	}
	switch cfg.Ranker {
	case "llm":
		ranked, err := rankLLM(ctx, cfg, httpc, query, cands)
		if err != nil {
			ranked = rankLexical(query, yearOf(query), cands)
			return "lexical", ranked, nil
		}
		return "llm", ranked, nil
	default:
		ranked, err := rankEmbed(ctx, cfg, httpc, query, cands)
		if err != nil {
			ranked = rankLexical(query, yearOf(query), cands)
			return "lexical", ranked, nil
		}
		return "embed", ranked, nil
	}
}

func rankEmbed(ctx context.Context, cfg config.Config, httpc *httpClient, query string, cands []Candidate) ([]Candidate, error) {
	if cfg.Llama.BaseURL == "" {
		return nil, fmt.Errorf("llama.base_url is empty")
	}
	qvec, err := embed(ctx, httpc, cfg, query)
	if err != nil {
		return nil, err
	}
	out := append([]Candidate(nil), cands...)
	for i, c := range out {
		vec, err := embed(ctx, httpc, cfg, strings.TrimSpace(c.Title+" "+c.Year))
		if err != nil {
			return nil, err
		}
		score := cosine(qvec, vec)
		if syn := strings.TrimSpace(c.Synopsis); syn != "" {
			svec, err := embed(ctx, httpc, cfg, syn)
			if err != nil {
				return nil, err
			}
			if s := cosine(qvec, svec); s > score {
				score = s
			}
		}
		out[i].Score = score
	}
	sortByScore(out)
	return out, nil
}

func embed(ctx context.Context, httpc *httpClient, cfg config.Config, text string) ([]float64, error) {
	payload := map[string]any{"input": text}
	if cfg.Llama.Model != "" {
		payload["model"] = cfg.Llama.Model
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.JoinPath(strings.TrimRight(cfg.Llama.BaseURL, "/"), "embeddings")
	if err != nil {
		return nil, err
	}
	b, code, err := httpc.post(ctx, endpoint, "application/json", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("embeddings status %d: %s", code, truncate(b, 200))
	}
	var resp struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 || len(resp.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding")
	}
	return resp.Data[0].Embedding, nil
}

func rankLLM(ctx context.Context, cfg config.Config, httpc *httpClient, query string, cands []Candidate) ([]Candidate, error) {
	base := cfg.Llama.LLMBaseURL
	if base == "" {
		base = cfg.Llama.BaseURL
	}
	if base == "" {
		return nil, fmt.Errorf("llama.llm_base_url is empty")
	}
	var bld strings.Builder
	bld.WriteString("Pick the best match for: ")
	bld.WriteString(query)
	bld.WriteString("\nCandidates:\n")
	for _, c := range cands {
		fmt.Fprintf(&bld, "- id=%s title=%s year=%s\n", c.Key(), c.Title, c.Year)
	}
	bld.WriteString("Reply with JSON {\"id\":\"provider:id\",\"score\":0.0-1.0} only.")
	payload := map[string]any{
		"messages": []map[string]string{
			{"role": "system", "content": "You rank media titles. Return JSON only."},
			{"role": "user", "content": bld.String()},
		},
		"temperature": 0,
		"response_format": map[string]string{"type": "json_object"},
	}
	if cfg.Llama.Model != "" {
		payload["model"] = cfg.Llama.Model
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.JoinPath(strings.TrimRight(base, "/"), "chat/completions")
	if err != nil {
		return nil, err
	}
	b, code, err := httpc.post(ctx, endpoint, "application/json", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("chat status %d: %s", code, truncate(b, 200))
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty chat response")
	}
	var pick struct {
		ID    string  `json:"id"`
		Score float64 `json:"score"`
	}
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &pick); err != nil {
		return nil, fmt.Errorf("chat json: %w", err)
	}
	out := append([]Candidate(nil), cands...)
	for i := range out {
		if out[i].Key() == pick.ID {
			out[i].Score = pick.Score
			if out[i].Score == 0 {
				out[i].Score = 1
			}
		}
	}
	sortByScore(out)
	return out, nil
}

func rankLexical(query, year string, cands []Candidate) []Candidate {
	qtoks := tokens(query)
	out := append([]Candidate(nil), cands...)
	for i, c := range out {
		ct := tokens(c.Title + " " + c.Year)
		overlap := 0
		for t := range qtoks {
			if ct[t] {
				overlap++
			}
		}
		score := 0.0
		if len(qtoks) > 0 {
			score = float64(overlap) / float64(len(qtoks))
		}
		if year != "" && c.Year == year {
			score += 0.15
		}
		out[i].Score = score
	}
	sortByScore(out)
	return out
}

func tokens(s string) map[string]bool {
	s = strings.ToLower(s)
	out := map[string]bool{}
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out[b.String()] = true
		b.Reset()
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func yearOf(query string) string {
	i := strings.LastIndex(query, "(")
	j := strings.LastIndex(query, ")")
	if i >= 0 && j > i {
		return strings.TrimSpace(query[i+1 : j])
	}
	return ""
}

func cosine(a, b []float64) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func sortByScore(cands []Candidate) {
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0 && cands[j].Score > cands[j-1].Score; j-- {
			cands[j], cands[j-1] = cands[j-1], cands[j]
		}
	}
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n])
}
