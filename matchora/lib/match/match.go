package match

import (
	"context"
	"errors"
	"strings"

	"github.com/alyshmahell/matchora/lib/config"
)

var ErrNotImplemented = errors.New("not_implemented")

func Match(query string, candidates []Candidate) ([]Candidate, error) {
	_, _ = query, candidates
	return nil, ErrNotImplemented
}

func Run(ctx context.Context, cfg config.Config, jobs []Job) []Job {
	return RunWith(ctx, cfg, jobs, nil)
}

func RunWith(ctx context.Context, cfg config.Config, jobs []Job, rep Reporter) []Job {
	ctx = withReporter(ctx, rep)
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
	if len(cands) == 0 {
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
	if job.Status == "error" {
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
	name := cfg.Ranker
	if name == "" {
		name = "embed"
	}
	done := waitStart(ctx, job, name)
	ranker, ranked, err := rank(ctx, cfg, httpc, job.QueryText(), cands)
	done(err)
	if err != nil {
		job.Status = "error"
		job.Error = err.Error()
		job.Candidates = cands
		return job
	}
	job.Ranker = ranker
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
	return job
}

func autoMatch(cfg config.Config, ranked []Candidate) bool {
	if len(ranked) == 0 {
		return false
	}
	best := ranked[0]
	if best.Score < cfg.Match.MinScore {
		return false
	}
	if len(ranked) > 1 && best.Score-ranked[1].Score < cfg.Match.MinMargin {
		return false
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
		return job, errors.New("candidate not found")
	}
	job.Match = pick
	job.Status = "matched"
	job.Error = ""
	httpc := newHTTP(cfg)
	if sub := fetchEpisode(ctx, cfg, httpc, job, *pick); sub != nil {
		job.Sub = sub
	}
	return job, nil
}
