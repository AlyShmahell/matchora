package match

import (
	"strings"
)

func rank(query string, cands []Candidate) []Candidate {
	if len(cands) == 0 {
		return cands
	}
	year := yearOf(query)
	qnorm := titleNorm(queryTitle(query, year))
	out := append([]Candidate(nil), cands...)
	for i, c := range out {
		cn := titleNorm(c.Title)
		score := 0.0
		if qnorm != "" && qnorm == cn {
			score = 1
		} else {
			score = SeqRatio(qnorm, cn)
			if y := strings.TrimSpace(c.Year); y != "" {
				if s := SeqRatio(qnorm, titleNorm(strings.TrimSpace(c.Title+" "+y))); s > score {
					score = s
				}
			}
		}
		if year != "" && c.Year == year {
			score += 0.15
		}
		out[i].Score = score
	}
	sortByScore(out)
	return out
}

func queryTitle(query, year string) string {
	if year == "" {
		return query
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(query), "("+year+")"))
}

func yearOf(query string) string {
	i := strings.LastIndex(query, "(")
	j := strings.LastIndex(query, ")")
	if i >= 0 && j > i {
		return strings.TrimSpace(query[i+1 : j])
	}
	return ""
}

func sortByScore(cands []Candidate) {
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0 && cands[j].Score > cands[j-1].Score; j-- {
			cands[j], cands[j-1] = cands[j-1], cands[j]
		}
	}
}
