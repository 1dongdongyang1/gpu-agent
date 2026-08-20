package idgen

import (
	"fmt"
	"sync"
)

type Generator interface {
	Next(prefix string) string
}

type Sequential struct {
	mu       sync.Mutex
	counters map[string]int
}

func NewSequential() *Sequential {
	return &Sequential{counters: make(map[string]int)}
}

func (g *Sequential) Next(prefix string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.counters[prefix]++
	return fmt.Sprintf("%s-%03d", prefix, g.counters[prefix])
}
