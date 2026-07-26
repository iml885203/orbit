package daemon

import (
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/extension"
)

// UpdateConfig is the single entry point for publishing a new config: it
// runs fn while holding the writer lock, so the whole read-modify-write
// (Load → build → Store, plus any path/staleness metadata) is one
// serialized critical section. fn may mutate process env before a
// tx.Load — that coupling is why the lock exists (see the writer shapes
// on extension.ConfigTx). An error from fn aborts without publishing
// anything fn didn't already Store. Nested UpdateConfig deadlocks.
func (s *Server) UpdateConfig(fn func(tx extension.ConfigTx) error) error {
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()
	return fn(serverConfigTx{s: s})
}

type serverConfigTx struct{ s *Server }

func (tx serverConfigTx) Current() *config.Config {
	return tx.s.holder.Load()
}

func (tx serverConfigTx) Load(path string) (*config.Config, error) {
	return config.Load(path)
}

func (tx serverConfigTx) Store(cfg *config.Config) {
	tx.s.holder.Store(cfg)
}

func (tx serverConfigTx) SetConfigPath(path string) {
	tx.s.SetConfigPath(path)
}

func (tx serverConfigTx) MarkEngineStale() {
	tx.s.engineStale.Store(true)
}
