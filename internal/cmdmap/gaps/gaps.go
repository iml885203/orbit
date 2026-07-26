package gaps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Gap is one UI action that has no CLI equivalent.
type Gap struct {
	Method      string    `json:"method"`
	PathPattern string    `json:"pathPattern"`
	Summary     string    `json:"summary"`
	FirstSeen   time.Time `json:"firstSeen"`
	LastSeen    time.Time `json:"lastSeen"`
	Count       int       `json:"count"`
}

// Collector accumulates gaps, deduplicated by method and path pattern.
type Collector struct {
	mu    sync.Mutex
	path  string
	items map[string]*Gap
}

func New(path string) *Collector {
	c := &Collector{path: path, items: map[string]*Gap{}}
	c.load()
	return c
}

func (c *Collector) Track(method, pathPattern, summary string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := method + " " + pathPattern
	now := time.Now()
	if g, ok := c.items[key]; ok {
		g.LastSeen = now
		g.Count++
		if summary != "" {
			g.Summary = summary
		}
		return
	}
	c.items[key] = &Gap{Method: method, PathPattern: pathPattern, Summary: summary, FirstSeen: now, LastSeen: now, Count: 1}
}

func (c *Collector) List() []Gap {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Gap, 0, len(c.items))
	for _, g := range c.items {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out
}

func (c *Collector) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.flushLocked()
}

func (c *Collector) load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	var items []Gap
	if err := json.Unmarshal(data, &items); err != nil {
		return
	}
	for i := range items {
		g := &items[i]
		c.items[g.Method+" "+g.PathPattern] = g
	}
}

func (c *Collector) flushLocked() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		return err
	}
	out := make([]Gap, 0, len(c.items))
	for _, g := range c.items {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}
