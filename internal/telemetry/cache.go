package telemetry

import (
	"noise-server/internal/model"
	"sync"
)

type Cache struct {
	mu    sync.RWMutex
	nodes map[string]model.Runtime
}

func New(nodes map[string]model.Runtime) *Cache { return &Cache{nodes: nodes} }
func (c *Cache) Get(id string) (model.Runtime, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.nodes[id]
	return r, ok
}
func (c *Cache) Set(id string, r model.Runtime) { c.mu.Lock(); defer c.mu.Unlock(); c.nodes[id] = r }
func (c *Cache) Delete(id string)               { c.mu.Lock(); defer c.mu.Unlock(); delete(c.nodes, id) }
