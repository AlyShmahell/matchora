package match

import (
	"context"
	"sync"
	"time"
)

type Circuit struct {
	mu     sync.Mutex
	streak map[string]int
	until  map[string]time.Time
}

func NewCircuit() *Circuit {
	return &Circuit{
		streak: map[string]int{},
		until:  map[string]time.Time{},
	}
}

func (c *Circuit) Cooling(name string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	until, ok := c.until[name]
	if !ok {
		return false
	}
	if time.Now().Before(until) {
		return true
	}
	delete(c.until, name)
	return false
}

func (c *Circuit) Fail(name string, limit int, ttl time.Duration) {
	if c == nil || limit <= 0 || name == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streak[name]++
	if c.streak[name] >= limit {
		c.until[name] = time.Now().Add(ttl)
		c.streak[name] = 0
	}
}

func (c *Circuit) OK(name string) {
	if c == nil || name == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streak[name] = 0
}

type circuitKey struct{}

func WithCircuit(ctx context.Context, c *Circuit) context.Context {
	if c == nil {
		return ctx
	}
	return context.WithValue(ctx, circuitKey{}, c)
}

func circuitFrom(ctx context.Context) *Circuit {
	c, _ := ctx.Value(circuitKey{}).(*Circuit)
	return c
}
