package config

import (
	"fmt"
	"sync"
	"testing"
)

func TestHolder_LoadStoreRoundTrip(t *testing.T) {
	a := &Config{Version: "3"}
	h := NewHolder(a)
	if h.Load() != a {
		t.Fatal("Load must return the stored snapshot")
	}
	b := &Config{Version: "4"}
	h.Store(b)
	if h.Load() != b {
		t.Fatal("Store must publish the new snapshot")
	}
}

// Concurrent readers against a swapping writer must always observe a
// complete snapshot — this is the race class the Holder exists to kill
// (run with -race).
func TestHolder_ConcurrentLoadStore(t *testing.T) {
	h := NewHolder(&Config{Containers: map[string]*Container{
		"sql-server": {Name: "sql-server", Image: "gen-0"},
	}})

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				cfg := h.Load()
				if c := cfg.Containers["sql-server"]; c == nil || c.Image == "" {
					t.Error("observed a torn snapshot")
					return
				}
			}
		}()
	}
	for gen := 1; gen <= 500; gen++ {
		cur := h.Load()
		h.Store(cur.WithContainer("sql-server", &Container{Name: "sql-server", Image: fmt.Sprintf("gen-%d", gen)}))
	}
	close(stop)
	wg.Wait()
}

func TestWithContainer_SplicesWithoutMutatingOriginal(t *testing.T) {
	orig := &Config{
		Version: "3",
		Containers: map[string]*Container{
			"sql-server": {Name: "sql-server", Image: "old"},
			"redis":      {Name: "redis", Image: "redis:7.4"},
		},
		Services: map[string]*Service{"api": {Name: "api"}},
	}
	next := orig.WithContainer("sql-server", &Container{Name: "sql-server", Image: "new"})

	if orig.Containers["sql-server"].Image != "old" {
		t.Fatal("WithContainer mutated the original map")
	}
	if next.Containers["sql-server"].Image != "new" {
		t.Fatal("replacement entry missing")
	}
	if next.Containers["redis"] != orig.Containers["redis"] {
		t.Fatal("untouched entries must be carried over")
	}
	if len(next.Containers) != 2 {
		t.Fatalf("unexpected container count %d", len(next.Containers))
	}
	// Shallow elsewhere by design: published configs are immutable.
	if &next.Services == &orig.Services {
		t.Fatal("expected distinct struct copies")
	}
}
