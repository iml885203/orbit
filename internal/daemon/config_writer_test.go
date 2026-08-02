package daemon

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/extension"
)

// The two production writers (restartSQLServer's splice, handleEnvSwitch's
// full publish) run their whole Load→build→Store inside UpdateConfig.
// This test pins the contract the handlers rely on: a splice must never
// resurrect the config generation an env switch just replaced (the
// lost-update scenario from the config-holder spec). Without the writer
// lock the splice goroutine can read A, lose the race to the switch's
// Store(B), and then publish A+splice — rolling the switch back.
func TestConfigWriters_SpliceCannotRollBackEnvSwitch(t *testing.T) {
	for i := 0; i < 200; i++ {
		cfgA := &config.Config{Containers: map[string]*config.Container{
			"sql-server": {Name: "sql-server", Image: "A"},
		}}
		cfgB := &config.Config{Containers: map[string]*config.Container{
			"sql-server": {Name: "sql-server", Image: "B"},
			"redis":      {Name: "redis", Image: "redis:7.4"},
		}}
		s := &Server{holder: config.NewHolder(cfgA)}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { // handleEnvSwitch's critical-section shape
			defer wg.Done()
			_ = s.UpdateConfig(func(tx extension.ConfigTx) error {
				tx.Store(cfgB)
				return nil
			})
		}()
		go func() { // restartSQLServer's critical-section shape
			defer wg.Done()
			_ = s.UpdateConfig(func(tx extension.ConfigTx) error {
				cur := tx.Current()
				spliced := cur.Containers["sql-server"]
				tx.Store(cur.WithContainer("sql-server",
					&config.Container{Name: spliced.Name, Image: "spliced"}))
				return nil
			})
		}()
		wg.Wait()

		final := s.holder.Load()
		// Whichever writer ran last, the switch must never be rolled back:
		// if the splice won the second slot it must have built on B (redis
		// present); a final config without redis AND with the splice means
		// the splice resurrected A over the switch.
		if _, hasRedis := final.Containers["redis"]; !hasRedis {
			if final.Containers["sql-server"].Image == "spliced" {
				t.Fatalf("iteration %d: splice rolled back the env switch (A+splice published)", i)
			}
		}
	}
}

// SetConfigPath must land inside the same critical section as Store:
// restartSQLServer reads the config path inside its own UpdateConfig, so
// a switch that Stores the new config but publishes the path outside the
// lock lets a racing splice re-Load the OLD env file against the NEW
// config. That atomicity is pinned by the race test above plus
// UpdateConfig's structure; this test pins the delegation — the tx
// surface routes all three effects (publish, path+baseline, staleness)
// to the server.
func TestConfigWriters_PathAndStalenessCommitWithStore(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	dir := t.TempDir()
	envPath := filepath.Join(dir, "next.yaml")
	if err := os.WriteFile(envPath, []byte("version: \"3\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Server{holder: config.NewHolder(&config.Config{})}
	next := &config.Config{Version: "3"}
	err := s.UpdateConfig(func(tx extension.ConfigTx) error {
		tx.Store(next)
		tx.SetConfigPath(envPath)
		tx.MarkEngineStale()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := s.holder.Load(); got != next {
		t.Fatalf("published config = %p, want %p", got, next)
	}
	if got := s.ConfigPath(); got != envPath {
		t.Fatalf("config path = %q, want %q", got, envPath)
	}
	if !s.engineStale.Load() {
		t.Fatal("engine staleness not marked")
	}
}
