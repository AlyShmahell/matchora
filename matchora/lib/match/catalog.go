package match

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/alyshmahell/matchora/lib/config"
)

func fetchCatalog(ctx context.Context, cfg config.Config, httpc *httpClient, job Job, cand Candidate) ([]CatalogSeason, string) {
	spec, ok := cfg.Providers[cand.Provider]
	if !ok || spec.Catalog == nil {
		return nil, ""
	}
	forKey := cand.Key()
	seasons, err := loadCatalog(ctx, httpc, cand.Provider, spec, job, cand.ID)
	if err != nil || seasons == nil {
		return []CatalogSeason{}, forKey
	}
	return seasons, forKey
}

func FillCatalog(ctx context.Context, cfg config.Config, job Job) Job {
	if job.Match == nil {
		job.Catalog = []CatalogSeason{}
		return job
	}
	httpc := newHTTP(cfg)
	cat, forKey := fetchCatalog(ctx, cfg, httpc, job, *job.Match)
	if cat == nil {
		job.Catalog = []CatalogSeason{}
		job.CatalogFor = ""
		return job
	}
	job.Catalog = cat
	job.CatalogFor = forKey
	return job
}

func ApplyCatalog(ctx context.Context, cfg config.Config, job Job, provider, id string) (Job, error) {
	cand := findCandidate(job, provider, id)
	if cand == nil {
		return job, errCandidateNotFound
	}
	httpc := newHTTP(cfg)
	cat, forKey := fetchCatalog(ctx, cfg, httpc, job, *cand)
	if cat == nil {
		job.Catalog = []CatalogSeason{}
		job.CatalogFor = cand.Key()
		return job, nil
	}
	job.Catalog = cat
	job.CatalogFor = forKey
	return job, nil
}

func NeedsCatalog(cfg config.Config, job Job) bool {
	if job.Status != "matched" || job.Catalog != nil || job.Match == nil {
		return false
	}
	spec, ok := cfg.Providers[job.Match.Provider]
	return ok && spec.Catalog != nil
}

func findCandidate(job Job, provider, id string) *Candidate {
	if job.Match != nil && job.Match.Provider == provider && job.Match.ID == id {
		return job.Match
	}
	for i := range job.Candidates {
		if job.Candidates[i].Provider == provider && job.Candidates[i].ID == id {
			return &job.Candidates[i]
		}
	}
	return nil
}

func LookupCandidate(job Job, provider, id string) (Candidate, bool) {
	c := findCandidate(job, provider, id)
	if c == nil {
		return Candidate{}, false
	}
	return *c, true
}

func attachCatalog(ctx context.Context, cfg config.Config, httpc *httpClient, job Job, cand Candidate) Job {
	cat, forKey := fetchCatalog(ctx, cfg, httpc, job, cand)
	if cat != nil {
		job.Catalog = cat
		job.CatalogFor = forKey
	}
	return job
}

func loadCatalog(ctx context.Context, httpc *httpClient, name string, spec config.Provider, job Job, showID string) ([]CatalogSeason, error) {
	cat := spec.Catalog
	seasons := make([]CatalogSeason, 0)
	if cat.Seasons != nil && cat.Seasons.URL != "" {
		items, err := fetchCatalogList(ctx, httpc, name, spec, job, cat.Seasons, showID, "", "")
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			s, ok := seasonFrom(spec, cat.Seasons, item)
			if ok {
				seasons = append(seasons, s)
			}
		}
		sortCatalogSeasons(seasons)
	}
	if cat.Episodes == nil || cat.Episodes.URL == "" {
		return seasons, nil
	}
	if perSeasonURL(cat.Episodes.URL) {
		for i := range seasons {
			if strings.Contains(cat.Episodes.URL, "{season}") && seasons[i].Number == "" {
				continue
			}
			if strings.Contains(cat.Episodes.URL, "{season_id}") && seasons[i].ID == "" {
				continue
			}
			items, err := fetchCatalogList(ctx, httpc, name, spec, job, cat.Episodes, showID, seasons[i].Number, seasons[i].ID)
			if err != nil {
				if ctx.Err() != nil {
					return nil, err
				}
				continue
			}
			seasons[i].Episodes = episodesFrom(spec, cat.Episodes, items)
		}
		return seasons, nil
	}
	items, err := fetchCatalogList(ctx, httpc, name, spec, job, cat.Episodes, showID, "", "")
	if err != nil {
		return seasons, nil
	}
	grouped := map[string][]CatalogEpisode{}
	for _, item := range items {
		ep, ok := episodeFrom(spec, cat.Episodes, item)
		if !ok {
			continue
		}
		key := episodeSeasonKey(cat.Episodes, item)
		grouped[key] = append(grouped[key], ep)
	}
	return attachGroupedEpisodes(seasons, grouped), nil
}

func fetchCatalogList(ctx context.Context, httpc *httpClient, name string, spec config.Provider, job Job, list *config.CatalogList, showID, season, seasonID string) ([]any, error) {
	done := waitStart(ctx, job, name+"/catalog")
	if err := paceProvider(ctx, name, spec.MinIntervalMS); err != nil {
		done(err)
		return nil, err
	}
	v := varsFor(spec, job, showID)
	v["season"] = season
	v["season_id"] = seasonID
	raw, err := providerGET(ctx, httpc.forSpec(spec), list.URL, list.Query, v, nil)
	if err != nil {
		done(err)
		return nil, err
	}
	items, err := extractItems(raw, list.Items)
	done(err)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func perSeasonURL(u string) bool {
	return strings.Contains(u, "{season}") || strings.Contains(u, "{season_id}")
}

func seasonFrom(spec config.Provider, list *config.CatalogList, item any) (CatalogSeason, bool) {
	id, number, title, synopsis, poster, href, year, _ := catalogFields(spec, list, item, "Season ")
	if title == "" {
		return CatalogSeason{}, false
	}
	return CatalogSeason{
		ID:       id,
		Number:   number,
		Title:    title,
		Synopsis: synopsis,
		Poster:   poster,
		URL:      href,
		Year:     year,
	}, true
}

func episodeFrom(spec config.Provider, list *config.CatalogList, item any) (CatalogEpisode, bool) {
	id, number, title, synopsis, poster, href, year, _ := catalogFields(spec, list, item, "Episode ")
	if title == "" {
		return CatalogEpisode{}, false
	}
	return CatalogEpisode{
		ID:       id,
		Number:   number,
		Title:    title,
		Synopsis: synopsis,
		Poster:   poster,
		URL:      href,
		Year:     year,
	}, true
}

func episodesFrom(spec config.Provider, list *config.CatalogList, items []any) []CatalogEpisode {
	out := make([]CatalogEpisode, 0, len(items))
	for _, item := range items {
		ep, ok := episodeFrom(spec, list, item)
		if ok {
			out = append(out, ep)
		}
	}
	sortCatalogEpisodes(out)
	return out
}

func catalogFields(spec config.Provider, list *config.CatalogList, item any, emptyPrefix string) (id, number, title, synopsis, poster, href, year, season string) {
	fields := list.Fields
	id = asString(dig(item, fields["id"]))
	number = asString(dig(item, fields["number"]))
	title = strings.TrimSpace(asString(dig(item, fields["title"])))
	if title == "" && number != "" {
		title = emptyPrefix + number
	}
	if id == "" {
		id = number
	}
	if p := fields["synopsis"]; p != "" {
		synopsis = clipText(stripHTML(asString(dig(item, p))), synopsisLimit)
	}
	if p := fields["poster"]; p != "" {
		poster = asString(dig(item, p))
	}
	prefix := list.PosterPrefix
	if prefix == "" {
		prefix = spec.PosterPrefix
	}
	if prefix != "" && poster != "" {
		poster = prefix + poster
	}
	if p := fields["url"]; p != "" {
		href = asString(dig(item, p))
	}
	year = asString(dig(item, fields["year"]))
	if list.Year == "prefix4" && len(year) >= 4 {
		year = year[:4]
	}
	season = episodeSeasonKey(list, item)
	return
}

func episodeSeasonKey(list *config.CatalogList, item any) string {
	path := ""
	if list != nil && list.Fields != nil {
		path = list.Fields["season"]
	}
	if path == "" {
		path = "season"
	}
	return asString(dig(item, path))
}

func attachGroupedEpisodes(seasons []CatalogSeason, grouped map[string][]CatalogEpisode) []CatalogSeason {
	used := map[string]bool{}
	for i := range seasons {
		key := seasons[i].Number
		if eps, ok := grouped[key]; ok {
			sortCatalogEpisodes(eps)
			seasons[i].Episodes = eps
			used[key] = true
		}
	}
	extras := make([]string, 0)
	for k := range grouped {
		if !used[k] {
			extras = append(extras, k)
		}
	}
	sort.Slice(extras, func(i, j int) bool {
		return catalogNumLess(extras[i], extras[j])
	})
	for _, k := range extras {
		eps := grouped[k]
		sortCatalogEpisodes(eps)
		title := "Season " + k
		if k == "" {
			title = "Season"
		}
		seasons = append(seasons, CatalogSeason{
			Number:   k,
			Title:    title,
			Episodes: eps,
		})
	}
	sortCatalogSeasons(seasons)
	return seasons
}

func sortCatalogSeasons(seasons []CatalogSeason) {
	sort.SliceStable(seasons, func(i, j int) bool {
		return catalogNumLess(seasons[i].Number, seasons[j].Number)
	})
}

func sortCatalogEpisodes(eps []CatalogEpisode) {
	sort.SliceStable(eps, func(i, j int) bool {
		return catalogNumLess(eps[i].Number, eps[j].Number)
	})
}

func catalogNumLess(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	if aerr == nil {
		return true
	}
	if berr == nil {
		return false
	}
	return a < b
}
