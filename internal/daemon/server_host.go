package daemon

import (
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/process"
)

// Server implements extension.Host — the surface DaemonSetup receives.
// UpdateConfig lives in config_write.go.

func (s *Server) Config() *config.Config {
	return s.holder.Load()
}

func (s *Server) ProcessMgr() *process.Manager {
	return s.app.ProcessMgr
}

// AddResourceContributor registers fn. Called during DaemonSetup only —
// before any listener serves — so the slice is read-only once serving
// (same discipline as extHooks).
func (s *Server) AddResourceContributor(fn ResourceContributor) {
	s.resourceContributors = append(s.resourceContributors, fn)
}

// AddSettingsPUTHook registers fn — DaemonSetup-time only, read-only
// once serving (same discipline as extHooks).
func (s *Server) AddSettingsPUTHook(fn SettingsPUTHook) {
	s.settingsPUTHooks = append(s.settingsPUTHooks, fn)
}

// Settings exposes the daemon's settings store to feature setups.
func (s *Server) Settings() *Settings {
	return s.settings
}

func (s *Server) Containers() ContainerOps {
	return s.app.ContainerMgr
}

func (s *Server) Restarter() ServiceRestarter {
	return s.app.Orchestrator
}

// AddDoctorChecks registers fn — DaemonSetup-time only, read-only once
// serving.
func (s *Server) AddDoctorChecks(fn func() []DoctorCheck) {
	s.doctorContributors = append(s.doctorContributors, fn)
}
