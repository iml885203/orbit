package devdb

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/extension"
	"github.com/iml885203/orbit/process"
)

// fakeDaemonHost satisfies daemonHost from public packages only — the
// overlay's tests cannot construct the core-internal daemon.Server, so
// the handler tests exercise the feature against the same capability
// surface the production host exposes (the wiring itself is covered by
// the daemon smoke path). Capabilities the DB handler tests never reach
// (Containers, Restarter, UpdateConfig) panic loudly instead of
// silently no-oping.
type fakeDaemonHost struct {
	holder     *config.Holder
	settings   *daemon.Settings
	pm         *process.Manager
	configPath string
	updateErr  error
}

func (h *fakeDaemonHost) Config() *config.Config { return h.holder.Load() }
func (h *fakeDaemonHost) UpdateConfig(func(tx extension.ConfigTx) error) error {
	if h.updateErr != nil {
		return h.updateErr
	}
	panic("not used in DB feature tests")
}
func (h *fakeDaemonHost) ProcessMgr() *process.Manager { return h.pm }
func (h *fakeDaemonHost) Settings() *daemon.Settings   { return h.settings }
func (h *fakeDaemonHost) Containers() daemon.ContainerOps {
	panic("not used in DB feature tests")
}
func (h *fakeDaemonHost) Restarter() daemon.ServiceRestarter {
	panic("not used in DB feature tests")
}
func (h *fakeDaemonHost) BaseContext() context.Context { return context.Background() }
func (h *fakeDaemonHost) ConfigPath() string           { return h.configPath }

func (h *fakeDaemonHost) ResolveWorkspaceRoot() (string, daemon.DoctorCheck, bool) {
	panic("not used in DB feature tests")
}

// newTestDBFeature builds a dbFeature over a fake daemonHost carrying a
// real Settings and config holder — the moved handler tests keep
// exercising the same host surface they consumed as Server methods.
func newTestDBFeature(t *testing.T, cfg *config.Config) *dbFeature {
	t.Helper()
	t.Setenv("ORBIT_HOME", t.TempDir())
	host := &fakeDaemonHost{
		holder:   config.NewHolder(cfg),
		settings: daemon.LoadSettings(filepath.Join(t.TempDir(), "settings.json")),
		pm:       process.NewManager(),
	}
	return &dbFeature{host: host, dbOps: newDBOpsManager(), drift: newDriftCache()}
}

func testConfig() *config.Config {
	return &config.Config{
		Containers: map[string]*config.Container{
			"redis": {Name: "redis", Image: "redis:7"},
			"db":    {Name: "db", Image: "postgres:15"},
		},
		Services: map[string]*config.Service{
			"api": {Name: "api", Type: "dotnet", DependsOn: []string{"db"}},
			"web": {Name: "web", Type: "node", DependsOn: []string{"api"}},
		},
	}
}
