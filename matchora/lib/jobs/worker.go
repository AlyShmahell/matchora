package jobs

import (
	"context"
	"log"
	"sync"

	"github.com/alyshmahell/matchora/lib/config"
	"github.com/alyshmahell/matchora/lib/match"
)

type Worker struct {
	cfg   config.Config
	store *Store
	waits *match.WaitLog
	cool  *match.Circuit
	mu    sync.Mutex
	busy  bool
}

func NewWorker(cfg config.Config, store *Store) *Worker {
	return &Worker{cfg: cfg, store: store, waits: &match.WaitLog{}, cool: match.NewCircuit()}
}

func (w *Worker) Waits() []match.Wait {
	return w.waits.Snapshot()
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
		list, err := w.store.List()
		if err != nil {
			return
		}
		for _, j := range list {
			if j.Status == "pending" {
				w.Kick()
				return
			}
		}
	}()
	for {
		list, err := w.store.List()
		if err != nil {
			log.Printf("match worker: list: %v", err)
			return
		}
		n := w.cfg.MatchWorkers()
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
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), w.cfg.HTTPTimeout())
		var wg sync.WaitGroup
		wg.Add(len(batch))
		for _, job := range batch {
			go func(job match.Job) {
				defer wg.Done()
				out := match.RunWith(match.WithCircuit(ctx, w.cool), w.cfg, []match.Job{job}, w.waits)[0]
				if err := w.store.Update(map[string]match.Job{out.ID: out}); err != nil {
					log.Printf("match worker: update: %v", err)
				}
			}(job)
		}
		wg.Wait()
		cancel()
	}
}
