package match

import (
	"context"
	"sync"
	"time"

	"github.com/alyshmahell/matchora/lib/config"
)

type Circuit struct {
	mu     sync.Mutex
	streak map[string]int
	until  map[string]time.Time
	exp    map[string]int
}

func NewCircuit() *Circuit {
	return &Circuit{
		streak: map[string]int{},
		until:  map[string]time.Time{},
		exp:    map[string]int{},
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

func (c *Circuit) Fail(name string, limit, minExp, maxExp int) {
	if c == nil || limit <= 0 || name == "" {
		return
	}
	if minExp < 0 {
		minExp = 0
	}
	if maxExp < minExp+2 {
		maxExp = minExp + 2
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streak[name]++
	if c.streak[name] < limit {
		return
	}
	e := c.exp[name]
	if e == 0 {
		e = minExp + 1
	} else {
		e++
		if e > maxExp {
			e = maxExp
		}
	}
	c.exp[name] = e
	c.until[name] = time.Now().Add(config.JitterExp(e))
	c.streak[name] = 0
}

func (c *Circuit) OK(name string) {
	if c == nil || name == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streak[name] = 0
	delete(c.exp, name)
	delete(c.until, name)
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
