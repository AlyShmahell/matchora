package match

import (
	"context"
	"errors"
	"strings"

	"github.com/alyshmahell/matchora/lib/config"
)

var errCandidateNotFound = errors.New("candidate not found")

func Run(ctx context.Context, cfg config.Config, jobs []Job) []Job {
	return RunWith(ctx, cfg, jobs, nil)
}

func RunWith(ctx context.Context, cfg config.Config, jobs []Job, rep Reporter) []Job {
	ctx = WithReporter(ctx, rep)
	httpc := newHTTP(cfg)
	out := make([]Job, len(jobs))
	for i, job := range jobs {
		out[i] = runOne(ctx, cfg, httpc, job)
	}
	return out
}

func runOne(ctx context.Context, cfg config.Config, httpc *httpClient, job Job) Job {
	job.Error = ""
	fast, ferrs, fok := collectProviders(ctx, cfg, httpc, job, true, false)
	if job, done := rankPass(ctx, cfg, httpc, job, fast); done {
		return job
	}
	slow, serrs, sok := collectProviders(ctx, cfg, httpc, job, true, true)
	cands := append(append([]Candidate(nil), fast...), slow...)
	errs := append(append([]string(nil), ferrs...), serrs...)
	ok := fok + sok
	if len(cands) == 0 && job.Type == "" {
		restFast, rferrs, rfok := collectProviders(ctx, cfg, httpc, job, false, false)
		if job, done := rankPass(ctx, cfg, httpc, job, restFast); done {
			return job
		}
		restSlow, rserrs, rsok := collectProviders(ctx, cfg, httpc, job, false, true)
		cands = append(append([]Candidate(nil), restFast...), restSlow...)
		errs = append(append(errs, rferrs...), rserrs...)
		ok += rfok + rsok
	}
	if len(cands) == 0 && len(errs) > 0 && ok == 0 {
		job.Status = "error"
		job.Error = strings.Join(errs, "; ")
		return job
	}
	if len(cands) == 0 {
		job.Status = "unmatched"
		job.Candidates = []Candidate{}
		job.Match = nil
		return job
	}
	return finishRank(ctx, cfg, httpc, job, cands)
}

func rankPass(ctx context.Context, cfg config.Config, httpc *httpClient, job Job, cands []Candidate) (Job, bool) {
	if len(cands) == 0 {
		return job, false
	}
	job = finishRank(ctx, cfg, httpc, job, cands)
	if job.Status == "error" || job.Status == "matched" {
		return job, true
	}
	if skipDefer(cfg, job.Candidates) {
		return job, true
	}
	return job, false
}

func skipDefer(cfg config.Config, ranked []Candidate) bool {
	if len(ranked) < cfg.MatchMinHits() {
		return false
	}
	return ranked[0].Score >= cfg.Match.MinScore
}

func finishRank(ctx context.Context, cfg config.Config, httpc *httpClient, job Job, cands []Candidate) Job {
	done := waitStart(ctx, job, "seq")
	cands = preferCandidates(cfg, job.Type, cands)
	ranked := rank(job.QueryText(), cands)
	done(nil)
	job.Ranker = "seq"
	job.Candidates = ranked
	best := ranked[0]
	if !autoMatch(cfg, ranked) {
		job.Status = "manual"
		job.Match = nil
		return job
	}
	job.Match = &best
	job.Status = "matched"
	if sub := fetchEpisode(ctx, cfg, httpc, job, best); sub != nil {
		job.Sub = sub
	}
	fetchDetail(ctx, cfg, httpc, job, job.Match)
	return attachCatalog(ctx, cfg, httpc, job, *job.Match)
}

func autoMatch(cfg config.Config, ranked []Candidate) bool {
	if len(ranked) == 0 {
		return false
	}
	best := ranked[0]
	need := cfg.Match.MinScore
	if len(ranked) == 1 {
		need = cfg.MatchSoloScore()
	}
	if best.Score < need {
		return false
	}
	if len(ranked) > 1 && best.Score-ranked[1].Score < cfg.Match.MinMargin {
		return false
	}
	return true
}

func preferCandidates(cfg config.Config, jobType string, cands []Candidate) []Candidate {
	want := cfg.Match.Prefer[jobType]
	if len(want) == 0 || len(cands) == 0 {
		return cands
	}
	var keep []Candidate
	for _, c := range cands {
		if preferMatch(c, want) {
			keep = append(keep, c)
		}
	}
	if len(keep) == 0 {
		return cands
	}
	return keep
}

func preferMatch(c Candidate, want map[string]string) bool {
	for k, v := range want {
		got := ""
		if c.Attrs != nil {
			got = c.Attrs[k]
		}
		if !strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(v)) {
			return false
		}
	}
	return true
}

func ApplySelect(ctx context.Context, cfg config.Config, job Job, provider, id string) (Job, error) {
	var pick *Candidate
	for i := range job.Candidates {
		if job.Candidates[i].Provider == provider && job.Candidates[i].ID == id {
			pick = &job.Candidates[i]
			break
		}
	}
	if pick == nil {
		return job, errCandidateNotFound
	}
	job.Match = pick
	job.Status = "matched"
	job.Error = ""
	httpc := newHTTP(cfg)
	if sub := fetchEpisode(ctx, cfg, httpc, job, *pick); sub != nil {
		job.Sub = sub
	}
	fetchDetail(ctx, cfg, httpc, job, job.Match)
	return attachCatalog(ctx, cfg, httpc, job, *job.Match), nil
}
