package match

import (
	"strings"

	"github.com/alyshmahell/matchora/lib/config"
)

func rank(cfg config.Config, job Job, cands []Candidate) []Candidate {
	if len(cands) == 0 {
		return cands
	}
	year := strings.TrimSpace(job.Year)
	stop := cfg.PlotStop()
	titleSet := tokenSet(job.Title)
	qplot := plotQuery(job.Title, stop)
	var parentSet map[string]struct{}
	if parent := strings.TrimSpace(job.Parent); parent != "" && titleNorm(parent) != titleNorm(job.Title) {
		parentSet = contentSet(parent, stop)
	}
	out := append([]Candidate(nil), cands...)
	synSets := make([]map[string]struct{}, len(out))
	for i, c := range out {
		candSet := candidateTitleSet(c)
		out[i].Jaccard = bestTitleJaccard(titleSet, c)
		out[i].QueryCov = coverage(titleSet, candSet)
		synSets[i] = tokenSet(c.Synopsis)
	}
	df := plotDF(qplot, synSets)
	for i := range out {
		j := out[i].Jaccard
		plot := plotFunc(qplot, synSets[i], df)
		parentCov := coverage(parentSet, candidateTitleSet(out[i]))
		if parentCov > plot {
			plot = parentCov
		}
		out[i].Score = j + (1-j)*plot
	}
	sortByScore(out, job.Title, year, stop)
	return out
}

func plotQuery(title string, stop map[string]struct{}) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, t := range strings.Fields(titleNorm(title)) {
		if len(t) < 3 {
			continue
		}
		if _, ok := stop[t]; ok {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func plotDF(q []string, syns []map[string]struct{}) map[string]int {
	df := make(map[string]int, len(q))
	for _, t := range q {
		n := 0
		for _, syn := range syns {
			if _, ok := syn[t]; ok {
				n++
			}
		}
		df[t] = n
	}
	return df
}

func plotFunc(q []string, syn map[string]struct{}, df map[string]int) float64 {
	if len(q) == 0 || len(syn) == 0 {
		return 0
	}
	var num, den float64
	for _, t := range q {
		u := 1.0
		if df[t] >= 2 {
			u = 0.25
		}
		w := float64(len(t)) * u
		den += w
		if _, ok := syn[t]; ok {
			num += w
		}
	}
	if den == 0 {
		return 0
	}
	return num / den
}

func contentSet(s string, stop map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	for _, t := range plotQuery(s, stop) {
		out[t] = struct{}{}
	}
	return out
}

func bestTitleJaccard(q map[string]struct{}, c Candidate) float64 {
	j := jaccard(q, tokenSet(c.Title))
	for k, v := range c.Attrs {
		if !strings.HasPrefix(strings.ToLower(k), "title") {
			continue
		}
		if alt := jaccard(q, tokenSet(v)); alt > j {
			j = alt
		}
	}
	return j
}

func candidateTitleSet(c Candidate) map[string]struct{} {
	out := tokenSet(c.Title)
	for k, v := range c.Attrs {
		if strings.HasPrefix(strings.ToLower(k), "title") {
			out = unionSets(out, tokenSet(v))
		}
	}
	return out
}

func sharesContent(a, b map[string]struct{}) bool {
	for t := range a {
		if _, ok := b[t]; ok {
			return true
		}
	}
	return false
}

func contentOverlap(jobTitle string, c Candidate, stop map[string]struct{}) bool {
	return sharesContent(contentSet(jobTitle, stop), contentSetFromTokens(candidateTitleSet(c), stop))
}

func contentSetFromTokens(toks map[string]struct{}, stop map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	for t := range toks {
		if len(t) < 3 {
			continue
		}
		if _, ok := stop[t]; ok {
			continue
		}
		out[t] = struct{}{}
	}
	return out
}

func titleTokenPrefix(jobTitle, candTitle string) bool {
	q := strings.Fields(titleNorm(jobTitle))
	c := strings.Fields(titleNorm(candTitle))
	if len(q) == 0 || len(q) > len(c) {
		return false
	}
	for i := range q {
		if q[i] != c[i] {
			return false
		}
	}
	return true
}

func tokenSet(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, t := range strings.Fields(titleNorm(s)) {
		if t != "" {
			out[t] = struct{}{}
		}
	}
	return out
}

func unionSets(a, b map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(a)+len(b))
	for t := range a {
		out[t] = struct{}{}
	}
	for t := range b {
		out[t] = struct{}{}
	}
	return out
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	uni := len(a)
	for t := range b {
		if _, ok := a[t]; ok {
			inter++
		} else {
			uni++
		}
	}
	if uni == 0 {
		return 1
	}
	return float64(inter) / float64(uni)
}

func coverage(q, c map[string]struct{}) float64 {
	if len(q) == 0 {
		return 0
	}
	n := 0
	for t := range q {
		if _, ok := c[t]; ok {
			n++
		}
	}
	return float64(n) / float64(len(q))
}

func sortByScore(cands []Candidate, jobTitle, year string, stop map[string]struct{}) {
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0 && betterRank(cands[j], cands[j-1], jobTitle, year, stop); j-- {
			cands[j], cands[j-1] = cands[j-1], cands[j]
		}
	}
}

func betterRank(a, b Candidate, jobTitle, year string, stop map[string]struct{}) bool {
	ao, bo := contentOverlap(jobTitle, a, stop), contentOverlap(jobTitle, b, stop)
	if ao != bo {
		return ao
	}
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	ap, bp := titleTokenPrefix(jobTitle, a.Title), titleTokenPrefix(jobTitle, b.Title)
	if ap != bp {
		return ap
	}
	if a.Jaccard != b.Jaccard {
		return a.Jaccard > b.Jaccard
	}
	if year != "" && (a.Year == year) != (b.Year == year) {
		return a.Year == year
	}
	return false
}
