package jobs

import (
	"context"
	"log"
	"sync"

	"github.com/alyshmahell/matchora/lib/config"
	"github.com/alyshmahell/matchora/lib/library"
	"github.com/alyshmahell/matchora/lib/match"
)

type Worker struct {
	cfg                *config.Config
	store              *Store
	waits              *match.WaitLog
	cool               *match.Circuit
	mu                 sync.Mutex
	busy               bool
	skipEpisodePosters bool
}

func NewWorker(cfg *config.Config, store *Store) *Worker {
	return &Worker{cfg: cfg, store: store, waits: match.NewWaitLog(cfg.WaitCap()), cool: match.NewCircuit()}
}

func (w *Worker) Waits() []match.Wait {
	return w.waits.Snapshot()
}

func (w *Worker) Reporter() match.Reporter {
	return w.waits
}

func (w *Worker) SetSkipEpisodePosters(v bool) {
	w.mu.Lock()
	w.skipEpisodePosters = v
	w.mu.Unlock()
}

func (w *Worker) skipEpisodePostersFlag() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.skipEpisodePosters
}

func (w *Worker) Kick() {
	w.mu.Lock()
	if w.busy {
		w.mu.Unlock()
		return
	}
	w.busy = true
	w.mu.Unlock()
	go w.loop()
}

func (w *Worker) loop() {
	defer func() {
		w.mu.Lock()
		w.busy = false
		w.mu.Unlock()
		list, err := w.store.ListAll(w.cfg.SessionTTL())
		if err != nil {
			return
		}
		for _, j := range list {
			if j.Status == "pending" || match.NeedsCatalog(*w.cfg, j) {
				w.Kick()
				return
			}
		}
	}()
	for {
		list, err := w.store.ListAll(w.cfg.SessionTTL())
		if err != nil {
			log.Printf("match worker: list: %v", err)
			return
		}
		n := w.cfg.MatchWorkers()
		batch := pendingBatch(list, n)
		if len(batch) > 0 {
			w.runBatch(batch, func(ctx context.Context, job match.Job) match.Job {
				return match.RunWith(ctx, *w.cfg, []match.Job{job}, w.waits)[0]
			})
			continue
		}
		batch = catalogBatch(*w.cfg, list, n)
		if len(batch) == 0 {
			return
		}
		w.runBatch(batch, func(ctx context.Context, job match.Job) match.Job {
			return match.FillCatalog(ctx, *w.cfg, job)
		})
	}
}

func (w *Worker) runBatch(batch []match.Job, fn func(context.Context, match.Job) match.Job) {
	var wg sync.WaitGroup
	wg.Add(len(batch))
	for _, job := range batch {
		go func(job match.Job) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), w.cfg.HTTPTimeout())
			defer cancel()
			ctx = match.WithCircuit(ctx, w.cool)
			ctx = match.WithReporter(ctx, w.waits)
			ctx = match.WithJob(ctx, job)
			out := fn(ctx, job)
			if err := w.store.UpdateAny(w.cfg.SessionTTL(), map[string]match.Job{out.ID: out}); err != nil {
				log.Printf("match worker: update: %v", err)
			}
			if out.Status == "matched" && out.Match != nil {
				if err := library.Save(ctx, *w.cfg, out, *out.Match, w.skipEpisodePostersFlag()); err != nil {
					log.Printf("match worker: library: %v", err)
				}
			}
		}(job)
	}
	wg.Wait()
}

func pendingBatch(list []match.Job, n int) []match.Job {
	batch := make([]match.Job, 0, n)
	for i := range list {
		if list[i].Status != "pending" {
			continue
		}
		batch = append(batch, list[i])
		if len(batch) >= n {
			break
		}
	}
	return batch
}

func catalogBatch(cfg config.Config, list []match.Job, n int) []match.Job {
	batch := make([]match.Job, 0, n)
	for i := range list {
		if !match.NeedsCatalog(cfg, list[i]) {
			continue
		}
		batch = append(batch, list[i])
		if len(batch) >= n {
			break
		}
	}
	return batch
}
