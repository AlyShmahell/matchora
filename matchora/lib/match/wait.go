package match

import (
	"context"
	"strconv"
	"sync"
	"time"
)

type Reporter interface {
	WaitStart(jobID, title, name string) string
	WaitEnd(id, err string)
}

type Wait struct {
	ID    string     `json:"id"`
	JobID string     `json:"job_id"`
	Title string     `json:"title"`
	Name  string     `json:"name"`
	Since time.Time  `json:"since"`
	Until *time.Time `json:"until,omitempty"`
	Error string     `json:"error,omitempty"`
}

type WaitLog struct {
	mu    sync.Mutex
	seq   int
	cap   int
	items []Wait
}

func NewWaitLog(cap int) *WaitLog {
	return &WaitLog{cap: cap}
}

func (l *WaitLog) WaitStart(jobID, title, name string) string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	id := strconv.Itoa(l.seq)
	w := Wait{
		ID:    id,
		JobID: jobID,
		Title: title,
		Name:  name,
		Since: time.Now().UTC(),
	}
	l.items = append([]Wait{w}, l.items...)
	if l.cap > 0 && len(l.items) > l.cap {
		l.items = l.items[:l.cap]
	}
	return id
}

func (l *WaitLog) WaitEnd(id, err string) {
	if l == nil || id == "" {
		return
	}
	now := time.Now().UTC()
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.items {
		if l.items[i].ID == id {
			l.items[i].Until = &now
			l.items[i].Error = err
			return
		}
	}
}

func (l *WaitLog) Snapshot() []Wait {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Wait, len(l.items))
	copy(out, l.items)
	return out
}

type reporterKey struct{}
type jobKey struct{}

func WithReporter(ctx context.Context, r Reporter) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, reporterKey{}, r)
}

func WithJob(ctx context.Context, job Job) context.Context {
	return context.WithValue(ctx, jobKey{}, job)
}

func jobFrom(ctx context.Context) Job {
	j, _ := ctx.Value(jobKey{}).(Job)
	return j
}

func waitStart(ctx context.Context, job Job, name string) func(error) {
	r, _ := ctx.Value(reporterKey{}).(Reporter)
	if r == nil {
		return func(error) {}
	}
	id := r.WaitStart(job.ID, job.Title, name)
	return func(err error) {
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		r.WaitEnd(id, msg)
	}
}
