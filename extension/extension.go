// Package extension defines the compile-time contract between the orbit
// core and team-specific feature sets. The core never imports an
// extension; the binary's wiring (cmd/orbit for this repo, an overlay
// repo's main once the core is split out) constructs the registrations
// and hands them to the core at startup. Registration is an explicit
// slice — no global init() registry — so wiring order is visible and
// testable.
//
// The contract grew batch by batch with the extension-interface
// extraction; fields are added when the seam they serve is registered,
// not before.
package extension

import (
	"net/http"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"

	"github.com/spf13/cobra"
)

type Extension struct {
	// Name keys the extension in daemon registration and diagnostics
	// once B3 wires DaemonSetup; until then it only labels the wiring.
	Name string

	// Commands returns CLI commands appended to the root command. The
	// commands emit the shared output contract via internal/cli.
	Commands func() []*cobra.Command

	// CommandVisibility returns whether each extension command belongs in
	// root help for the selected environment. Hidden commands remain
	// callable, so explicit discovery and automation do not depend on help.
	CommandVisibility func(cfg *config.Config) map[string]bool

	// DaemonSetup is invoked once from the daemon's route setup (before
	// any listener serves). It constructs the extension's daemon state,
	// registers routes on mux, and returns the hooks the daemon honours.
	DaemonSetup func(host Host, mux *http.ServeMux) DaemonHooks

	// CLIDoctor contributes to `orbit doctor`'s OFFLINE path (daemon not
	// running — the daemon path uses DoctorRegistrar via DaemonSetup).
	// Two hooks because the human rendering has feature-specific prose
	// (clone hints, mode lines) that structured checks can't carry
	// without changing the printed output.
	CLIDoctor *CLIDoctor

	// Distribution brands the built binary with its distribution
	// endpoints. Nil leaves those commands requiring explicit
	// configuration (flag, setting, or env var). When several
	// extensions set it, the first non-nil wins.
	Distribution *Distribution

	// CLIInit contributes to `orbit init`'s interactive wizard.
	CLIInit *CLIInit
}

// CLIInit is the init-wizard contribution: workspace-root detection
// hints and feature-specific settings steps. All fields are optional.
type CLIInit struct {
	// WorkspaceCandidates returns auto-detect candidates for the
	// workspace root, tried in order when neither settings nor env
	// provide one.
	WorkspaceCandidates func(home string) []string
	// WorkspaceMarkers reports known repo directories under root. At least
	// one marker proves an auto-detected candidate is a real workspace.
	WorkspaceMarkers func(root string) []string
	// Steps runs feature-specific settings prompts after core environment
	// and contextual workspace setup. yes mirrors --yes (accept defaults,
	// no prompting); prompt reads one trimmed line from the user. quiet
	// asks the extension to keep stdout machine-readable for --json.
	Steps func(settings *daemon.Settings, yes bool, prompt func(label string) string, quiet bool) error
}

// Distribution names where a distribution of the binary gets its
// updates and defaults from — properties of the built artifact, not of
// any daemon feature.
type Distribution struct {
	// EnvRepoURL is the default git repo for `orbit env sync` (and the
	// suggested default in `orbit init`), used when no --url flag,
	// env_repo_url setting, or ORBIT_ENV_REPO_URL env var overrides it.
	EnvRepoURL string
	// EnvRepoRef pins the default repository to the demo revision compatible
	// with this binary release. Custom repositories remain unpinned unless
	// their owner selects a ref explicitly.
	EnvRepoRef string
	// InstallURL is the install script `orbit update` pipes to
	// bash, unless ORBIT_INSTALL_URL overrides it.
	InstallURL string
	// DefaultEnv is the env file preferred when none is selected: the
	// fallback config path when no current env is set, and the init
	// wizard's pick when several envs are available. Empty means no
	// preference (first available wins; no fallback path).
	DefaultEnv string
}

// CLIDoctor is the offline-doctor contribution. cfg is the loaded and
// validated config; callers never invoke these with a nil cfg (a failed
// config load is reported by the core Config check instead).
type CLIDoctor struct {
	// Checks returns structured checks for the --json local response.
	Checks func(cfg *config.Config) []daemon.DoctorCheck
	// PrintHuman renders the feature's section of the human doctor
	// output to stdout.
	PrintHuman func(cfg *config.Config)
}
