package daemonsrv

import (
	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/extension"
)

// UpdateConfig is the single entry point for publishing a new config: it
// runs fn while holding the writer lock, so the whole read-modify-write
// (Load → build → Store, plus any path/staleness metadata) is one
// serialized critical section. fn may mutate process env before a
// tx.Load — that coupling is why the lock exists (see the writer shapes
// on extension.ConfigTx). An error from fn aborts without publishing
// staged Store/path effects. Nested UpdateConfig deadlocks.
func (s *Server) UpdateConfig(fn func(tx extension.ConfigTx) error) error {
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()
	tx := &serverConfigTx{current: s.holder.Load()}
	if err := fn(tx); err != nil {
		return err
	}
	var publication environmentPathPublication
	if tx.path != nil {
		publication = prepareEnvironmentPath(*tx.path, "", false)
	}
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	if tx.stored {
		s.holder.Store(tx.current)
	}
	if tx.path != nil {
		s.publishEnvironmentPath(publication)
	}
	if tx.stale {
		s.engineStale.Store(true)
	}
	return nil
}

type serverConfigTx struct {
	current *config.Config
	stored  bool
	path    *string
	stale   bool
}

func (tx *serverConfigTx) Current() *config.Config {
	return tx.current
}

func (tx *serverConfigTx) Load(path string) (*config.Config, error) {
	return config.Load(path)
}

func (tx *serverConfigTx) Store(cfg *config.Config) {
	tx.current = cfg
	tx.stored = true
}

func (tx *serverConfigTx) SetConfigPath(path string) {
	tx.path = &path
}

func (tx *serverConfigTx) MarkEngineStale() {
	tx.stale = true
}
