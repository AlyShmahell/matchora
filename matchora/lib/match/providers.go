package match

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/alyshmahell/matchora/lib/config"
)

var (
	paceMu     sync.Mutex
	paceLast   = map[string]time.Time{}
	htmlTag    = regexp.MustCompile(`(?s)<[^>]*>`)
	whitespace = regexp.MustCompile(`\s+`)
)

const synopsisLimit = 400

func searchProviders(ctx context.Context, cfg config.Config, httpc *httpClient, job Job) ([]Candidate, error) {
	return searchProvidersDefer(ctx, cfg, httpc, job, false)
}

func searchProvidersDefer(ctx context.Context, cfg config.Config, httpc *httpClient, job Job, deferred bool) ([]Candidate, error) {
	out, errs, ok := collectProviders(ctx, cfg, httpc, job, true, deferred)
	if len(out) == 0 {
		more, moreErrs, moreOK := collectProviders(ctx, cfg, httpc, job, false, deferred)
		out = append(out, more...)
		errs = append(errs, moreErrs...)
		ok += moreOK
	}
	if len(out) == 0 && len(errs) > 0 && ok == 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return out, nil
}

func collectProviders(ctx context.Context, cfg config.Config, httpc *httpClient, job Job, wantedOnly, deferred bool) ([]Candidate, []string, int) {
	type task struct {
		name string
		spec config.Provider
	}
	var tasks []task
	for name, spec := range cfg.Providers {
		if spec.Defer != deferred {
			continue
		}
		wanted := providerWanted(spec, job.Type)
		if wantedOnly && !wanted {
			continue
		}
		if !wantedOnly && wanted {
			continue
		}
		if spec.Require == "api_key" && spec.APIKey == "" {
			continue
		}
		tasks = append(tasks, task{name: name, spec: spec})
	}
	if len(tasks) == 0 {
		return nil, nil, 0
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		out  []Candidate
		errs []string
		ok   int
	)
	cool := circuitFrom(ctx)
	fails := cfg.MatchCooldownFails()
	ttl := cfg.MatchCooldown()
	wg.Add(len(tasks))
	for _, t := range tasks {
		go func(name string, spec config.Provider) {
			defer wg.Done()
			done := waitStart(ctx, job, name)
			if cool.Cooling(name) {
				err := fmt.Errorf("cooldown")
				done(err)
				mu.Lock()
				defer mu.Unlock()
				errs = append(errs, name+": "+err.Error())
				return
			}
			cs, err := searchProvider(ctx, httpc, name, spec, job)
			done(err)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if ctx.Err() == nil {
					cool.Fail(name, fails, ttl)
				}
				errs = append(errs, name+": "+err.Error())
				return
			}
			cool.OK(name)
			ok++
			out = append(out, cs...)
		}(t.name, t.spec)
	}
	wg.Wait()
	return out, errs, ok
}

func providerWanted(spec config.Provider, jobType string) bool {
	if jobType == "" || len(spec.Types) == 0 {
		return true
	}
	for _, t := range spec.Types {
		if t == jobType {
			return true
		}
	}
	return false
}

func searchProvider(ctx context.Context, httpc *httpClient, name string, spec config.Provider, job Job) ([]Candidate, error) {
	raw, err := providerGET(ctx, httpc, spec.URL, spec.Query, varsFor(spec, job, ""), func(ctx context.Context) error {
		return paceProvider(ctx, name, spec.MinIntervalMS)
	})
	if err != nil {
		return nil, err
	}
	items, err := extractItems(raw, spec.Items)
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(items))
	for _, item := range items {
		c, ok := candidateFrom(name, spec, item)
		if ok {
			out = append(out, c)
		}
	}
	return out, nil
}

func fetchEpisode(ctx context.Context, cfg config.Config, httpc *httpClient, job Job, cand Candidate) *Candidate {
	spec, ok := cfg.Providers[cand.Provider]
	if !ok || spec.Episode == nil || job.Season == "" || job.Episode == "" {
		return nil
	}
	done := waitStart(ctx, job, cand.Provider+"/episode")
	if err := paceProvider(ctx, cand.Provider, spec.MinIntervalMS); err != nil {
		done(err)
		return nil
	}
	v := varsFor(spec, job, cand.ID)
	raw, err := providerGET(ctx, httpc, spec.Episode.URL, spec.Episode.Query, v, nil)
	if err != nil {
		done(err)
		return nil
	}
	var obj any
	if err := json.Unmarshal(raw, &obj); err != nil {
		done(err)
		return nil
	}
	c, ok := candidateFrom(cand.Provider, config.Provider{
		Fields:    spec.Episode.Fields,
		Year:      spec.Episode.Year,
		URLPrefix: spec.URLPrefix,
	}, obj)
	if !ok {
		done(nil)
		return nil
	}
	done(nil)
	return &c
}

func providerGET(ctx context.Context, httpc *httpClient, rawURL string, query map[string]string, vars map[string]string, pace func(context.Context) error) ([]byte, error) {
	u, err := url.Parse(expand(rawURL, vars))
	if err != nil {
		return nil, err
	}
	qs := u.Query()
	for k, val := range query {
		exp := expand(val, vars)
		if exp != "" {
			qs.Set(k, exp)
		}
	}
	u.RawQuery = qs.Encode()
	b, code, err := httpc.get(ctx, u.String(), pace)
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("status %d", code)
	}
	return b, nil
}

func varsFor(spec config.Provider, job Job, id string) map[string]string {
	typeParam := ""
	if spec.TypeParams != nil {
		typeParam = spec.TypeParams[job.Type]
	}
	return map[string]string{
		"base":       spec.Base,
		"title":      job.Title,
		"year":       job.Year,
		"type":       job.Type,
		"season":     job.Season,
		"episode":    job.Episode,
		"id":         id,
		"api_key":    spec.APIKey,
		"type_param": typeParam,
	}
}

func expand(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

func extractItems(raw []byte, path string) ([]any, error) {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	v := dig(root, path)
	switch t := v.(type) {
	case []any:
		return t, nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("items %q is not an array", path)
	}
}

func candidateFrom(name string, spec config.Provider, item any) (Candidate, bool) {
	id := asString(dig(item, spec.Fields["id"]))
	title := asString(dig(item, spec.Fields["title"]))
	if id == "" || title == "" {
		return Candidate{}, false
	}
	year := asString(dig(item, spec.Fields["year"]))
	if spec.Year == "prefix4" && len(year) >= 4 {
		year = year[:4]
	}
	href := asString(dig(item, spec.Fields["url"]))
	if spec.URLPrefix != "" {
		href = spec.URLPrefix + id
	}
	synopsis := ""
	if p := spec.Fields["synopsis"]; p != "" {
		synopsis = clipText(stripHTML(asString(dig(item, p))), synopsisLimit)
	}
	poster := ""
	if p := spec.Fields["poster"]; p != "" {
		poster = asString(dig(item, p))
	}
	return Candidate{
		Provider: name,
		ID:       id,
		Title:    title,
		Year:     year,
		URL:      href,
		Synopsis: synopsis,
		Poster:   poster,
	}, true
}

func dig(v any, path string) any {
	if path == "" || path == "$" {
		return v
	}
	cur := v
	for _, p := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprint(t)
	}
}

func stripHTML(s string) string {
	s = htmlTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(whitespace.ReplaceAllString(s, " "))
}

func clipText(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n]))
}

func paceProvider(ctx context.Context, name string, minMS int) error {
	if minMS <= 0 {
		return nil
	}
	gap := time.Duration(minMS) * time.Millisecond
	paceMu.Lock()
	defer paceMu.Unlock()
	if last, ok := paceLast[name]; ok {
		if wait := gap - time.Since(last); wait > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	paceLast[name] = time.Now()
	return nil
}
