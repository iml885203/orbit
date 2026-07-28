package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/dockerctx"
	"github.com/iml885203/orbit/internal/engine"
	"github.com/iml885203/orbit/internal/shellquote"
	"github.com/moby/moby/client"
)

// CoreHostToolChecks is the ordered list of host dependencies Orbit itself
// needs independently of the active environment.
var CoreHostToolChecks = []HostToolCheck{
	{
		Name:     "Git",
		Binary:   "git",
		Critical: false,
		Hint:     "Install git (brew install git or your distro's package manager)",
		Version:  versionFromCmd("--version"),
	},
}

// A cold tool process can exceed two seconds while the machine is compiling
// Orbit; five seconds still bounds doctor latency without dropping valid output.
const hostToolVersionTimeout = 5 * time.Second

// versionFromCmd returns a Version probe that runs `<path> <arg>` with a
// timeout and trims the first line of stdout.
func versionFromCmd(arg string) func(path string) (string, error) {
	return func(path string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), hostToolVersionTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, path, arg).Output()
		if err != nil {
			return "", err
		}
		line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
		return line, nil
	}
}

// dotnetSDKVersion reports the highest installed SDK version. Empty stdout
// (runtime-only install) is surfaced as an error so callers can warn.
func dotnetSDKVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), hostToolVersionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--list-sdks").Output()
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return "", fmt.Errorf("no SDKs installed (runtime-only)")
	}
	lines := strings.Split(text, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	return "SDK " + last, nil
}

// checkHostTool resolves a host CLI dependency on PATH and (optionally) probes
// its version. A missing critical tool fails; a missing non-critical tool
// warns. Version probe errors are tolerated — they do not flip a pass to fail.
func checkHostTool(c HostToolCheck) DoctorCheck {
	requiredBy := ""
	if len(c.RequiredBy) > 0 {
		requiredBy = " (required by " + strings.Join(c.RequiredBy, ", ") + ")"
	}
	path, err := exec.LookPath(c.Binary)
	if err != nil {
		status := CheckWarn
		if c.Critical {
			status = CheckFail
		}
		return DoctorCheck{
			Name:    c.Name,
			Status:  status,
			Message: c.Binary + " not found on PATH" + requiredBy,
			Hint:    missingRuntimeHint(c),
		}
	}
	msg := "found at " + path
	version := ""
	if c.Version != nil {
		v, err := c.Version(path)
		if err != nil {
			slog.Debug("version probe failed", "component", "doctor", "tool", c.Binary, "err", err)
		} else if v != "" {
			version = v
			msg = fmt.Sprintf("%s (%s)", msg, v)
		}
	}
	msg += requiredBy
	return evaluateRuntimeRequirements(c, path, version, msg)
}

// HostEnvironmentChecks reports only the tools the selected environment
// actually needs. Optional Orbit features contribute their own checks, so a
// Python-only project never has to understand Node, .NET, or SQL tooling.
func HostEnvironmentChecks(cfg *config.Config) []DoctorCheck {
	tools := requiredHostTools(cfg)
	results := make([]DoctorCheck, len(tools), len(tools)+1)

	var wg sync.WaitGroup
	for i, tool := range tools {
		wg.Add(1)
		go func(i int, tool HostToolCheck) {
			defer wg.Done()
			results[i] = checkHostTool(tool)
		}(i, tool)
	}
	wg.Wait()

	results = append(results, projectDependencyChecks(cfg)...)
	return results
}

func projectDependencyChecks(cfg *config.Config) []DoctorCheck {
	if cfg == nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Services))
	for name := range cfg.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	var checks []DoctorCheck
	for _, name := range names {
		service := cfg.Services[name]
		if service == nil || service.Type != "node" {
			continue
		}
		manager := commandBinary(service.Command)
		if !isNodePackageManager(manager) {
			continue
		}
		checks = append(checks, nodeProjectDependencyCheck(name, service.Path, manager))
	}
	return checks
}

func nodeProjectDependencyCheck(service, path, manager string) DoctorCheck {
	check := DoctorCheck{Name: "Packages (" + service + ")"}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		check.Status = CheckFail
		check.Message = "service path not found: " + path
		check.Hint = "Update services." + service + ".path to the project directory"
		return check
	}
	if _, err := os.Stat(filepath.Join(path, "package.json")); err != nil {
		check.Status = CheckFail
		check.Message = "package.json not found in " + path
		check.Hint = "Update services." + service + ".path to the Node project directory"
		return check
	}
	if nodePackagesInstalled(path, manager) {
		check.Status = CheckPass
		check.Message = "installed in " + path
		return check
	}
	command := nodeInstallCommand(manager, path)
	check.Status = CheckFail
	check.Message = "project packages are not installed"
	check.Hint = "run: " + command
	return check
}

func nodePackagesInstalled(path, manager string) bool {
	if info, err := os.Stat(filepath.Join(path, "node_modules")); err == nil && info.IsDir() {
		return true
	}
	if manager == "yarn" {
		for _, marker := range []string{".pnp.cjs", ".pnp.js"} {
			if _, err := os.Stat(filepath.Join(path, marker)); err == nil {
				return true
			}
		}
	}
	return false
}

func nodeInstallCommand(manager, path string) string {
	quotedPath := shellquote.Quote(path)
	switch manager {
	case "npm":
		return "npm --prefix " + quotedPath + " install"
	case "pnpm":
		return "pnpm --dir " + quotedPath + " install"
	case "yarn":
		return "yarn --cwd " + quotedPath + " install"
	case "bun":
		return "bun --cwd " + quotedPath + " install"
	default:
		return manager + " install"
	}
}

func requiredHostTools(cfg *config.Config) []HostToolCheck {
	requirements := map[string]map[string]bool{}
	add := func(binary, service string) {
		if requirements[binary] == nil {
			requirements[binary] = map[string]bool{}
		}
		requirements[binary][service] = true
	}

	versionRequirements := projectRuntimeVersionRequirements(cfg)
	if cfg != nil {
		names := make([]string, 0, len(cfg.Services))
		for name := range cfg.Services {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			service := cfg.Services[name]
			if service == nil {
				continue
			}
			binary := commandBinary(service.Command)
			switch service.Type {
			case "dotnet":
				add("dotnet", name)
			case "node":
				if binary == "bun" {
					add("bun", name)
				} else {
					add("node", name)
				}
				if isNodePackageManager(binary) && binary != "bun" {
					add(binary, name)
				}
			case "python":
				if binary == "" {
					binary = "python3"
				}
				add(binary, name)
			default:
				if isKnownRuntime(binary) {
					add(binary, name)
				}
			}
		}
	}

	tools := append([]HostToolCheck(nil), CoreHostToolChecks...)
	for _, binary := range []string{"dotnet", "node", "python", "python3", "uv", "poetry", "npm", "pnpm", "yarn", "bun"} {
		services := sortedRequirementNames(requirements[binary])
		if len(services) == 0 {
			continue
		}
		tool := hostToolDefinition(binary, services)
		tool.Requirements = versionRequirements[binary]
		tools = append(tools, tool)
	}
	return tools
}

func commandBinary(command string) string {
	fields := strings.Fields(command)
	for len(fields) > 0 && strings.Contains(fields[0], "=") {
		fields = fields[1:]
	}
	if len(fields) > 1 && fields[0] == "env" {
		fields = fields[1:]
		for len(fields) > 0 && strings.Contains(fields[0], "=") {
			fields = fields[1:]
		}
	}
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(fields[0])
}

func isNodePackageManager(binary string) bool {
	switch binary {
	case "npm", "pnpm", "yarn", "bun":
		return true
	default:
		return false
	}
}

func isKnownRuntime(binary string) bool {
	switch binary {
	case "dotnet", "python", "python3", "uv", "poetry", "node", "npm", "pnpm", "yarn", "bun":
		return true
	default:
		return false
	}
}

func sortedRequirementNames(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func hostToolDefinition(binary string, requiredBy []string) HostToolCheck {
	tool := HostToolCheck{
		Name:       binary,
		Binary:     binary,
		Critical:   true,
		RequiredBy: requiredBy,
		Version:    versionFromCmd("--version"),
	}
	switch binary {
	case "dotnet":
		tool.Name = ".NET SDK"
		tool.Hint = "Install the .NET SDK: https://dotnet.microsoft.com/download"
		tool.Version = dotnetSDKVersion
	case "node":
		tool.Name = "Node.js"
		tool.Hint = "Install Node.js: https://nodejs.org/"
	case "python", "python3":
		tool.Name = "Python"
		tool.Hint = "Install Python 3: https://www.python.org/downloads/"
	case "uv":
		tool.Name = "uv"
		tool.Hint = "Install uv: https://docs.astral.sh/uv/getting-started/installation/"
	case "poetry":
		tool.Name = "Poetry"
		tool.Hint = "Install Poetry: https://python-poetry.org/docs/#installation"
	case "pnpm":
		tool.Name = "pnpm"
		tool.Hint = "Install pnpm: https://pnpm.io/installation"
	case "npm":
		tool.Name = "npm"
		tool.Hint = "Install Node.js (includes npm): https://nodejs.org/"
	case "yarn":
		tool.Name = "Yarn"
		tool.Hint = "Install Yarn: https://yarnpkg.com/getting-started/install"
	case "bun":
		tool.Name = "Bun"
		tool.Hint = "Install Bun: https://bun.sh/docs/installation"
	}
	return tool
}

// DockerCheck resolves the docker CLI on PATH and, if present, pings the
// daemon. Emits a single DoctorCheck named "Docker" covering both concerns.
// Exported so `orbit doctor` can reuse it when the daemon itself is down.
func DockerCheck() DoctorCheck {
	path, err := exec.LookPath("docker")
	if err != nil {
		return DoctorCheck{
			Name:    "Docker",
			Status:  CheckFail,
			Message: "docker not found on PATH",
			Hint:    "Install Docker Desktop or OrbStack",
		}
	}
	ctxInfo := dockerctx.Active()
	cli, err := dockerctx.NewClient()
	if err != nil {
		msg, hint := formatDockerFailMessage(ctxInfo, err.Error())
		return DoctorCheck{Name: "Docker", Status: CheckFail, Message: msg, Hint: hint}
	}
	defer func() { _ = cli.Close() }()
	ping, err := cli.Ping(context.Background(), client.PingOptions{})
	if err != nil {
		msg, hint := formatDockerFailMessage(ctxInfo, "Docker daemon is not running")
		return DoctorCheck{Name: "Docker", Status: CheckFail, Message: msg, Hint: hint}
	}
	return DoctorCheck{
		Name:    "Docker",
		Status:  CheckPass,
		Message: formatDockerPassMessage(ctxInfo, path, ping.APIVersion),
	}
}

// formatDockerPassMessage leads with the context name (what the user set and
// recognises) when one is active, instead of the docker binary path.
func formatDockerPassMessage(ctxInfo dockerctx.ContextInfo, path, apiVersion string) string {
	if ctxInfo.Name != "" {
		return fmt.Sprintf("%s context (API v%s)", ctxInfo.Name, apiVersion)
	}
	return fmt.Sprintf("found at %s (API v%s)", path, apiVersion)
}

// formatDockerFailMessage points the hint at the most common fix when a named
// context is active — switching back to default — since "Docker is down" and
// "I'm pointed at the wrong endpoint" look identical at the SDK layer.
func formatDockerFailMessage(ctxInfo dockerctx.ContextInfo, fallbackMsg string) (string, string) {
	if ctxInfo.Name != "" {
		return fmt.Sprintf("cannot reach %s context", ctxInfo.Name),
			"Start Docker Desktop, or run 'docker context use default' to fall back"
	}
	return fallbackMsg, "Start Docker Desktop"
}

// checkOrbitOnPath verifies that a bare `orbit` command resolves to the
// running binary. Merely finding its directory later in PATH is insufficient:
// another installation may take precedence.
func checkOrbitOnPath() DoctorCheck {
	exe, err := os.Executable()
	if err != nil {
		return DoctorCheck{Name: "PATH", Status: CheckInfo, Message: "cannot resolve orbit binary: " + err.Error()}
	}
	winner, lookupErr := exec.LookPath("orbit")
	return checkOrbitPath(exe, winner, lookupErr)
}

func checkOrbitPath(executable, winner string, lookupErr error) DoctorCheck {
	executable = resolvedPath(executable)
	exeDir := filepath.Dir(executable)
	if lookupErr != nil {
		return DoctorCheck{
			Name:    "PATH",
			Status:  CheckWarn,
			Message: "orbit installed at " + exeDir + " but not on $PATH",
			Hint:    "Add " + exeDir + " to your shell's PATH",
		}
	}
	winner = resolvedPath(winner)
	if winner == executable {
		return DoctorCheck{Name: "PATH", Status: CheckPass, Message: "orbit resolves to " + executable}
	}
	return DoctorCheck{
		Name:    "PATH",
		Status:  CheckWarn,
		Message: "another Orbit installation takes precedence on PATH: " + winner,
		Hint:    "Move " + exeDir + " before " + filepath.Dir(winner) + " in your shell's PATH",
	}
}

func resolvedPath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodGet) {
		return
	}
	checks := s.runDoctorChecks()
	writeJSON(w, http.StatusOK, DoctorResponse{
		Checks: checks,
		RanAt:  time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) runDoctorChecks() []DoctorCheck {
	var checks []DoctorCheck

	configPath := s.ConfigPath()
	checks = append(checks, DoctorCheck{Name: "Config", Status: CheckInfo, Message: configPath})
	if len(s.Config().Containers) > 0 {
		checks = append(checks, DockerCheck())
	}

	services := s.app.Orchestrator.GetAllServices()
	checks = append(checks, serviceHealthCheck(services))

	checks = append(checks, HostEnvironmentChecks(s.Config())...)
	checks = append(checks, checkOrbitOnPath())

	// Feature-owned check groups (the DB workflow) come from registered
	// contributors.
	for _, contribute := range s.doctorContributors {
		checks = append(checks, contribute()...)
	}
	return checks
}

// ResolveWorkspaceRoot is the exported form for feature-owned doctor
// groups that gate on the workspace root.
func (s *Server) ResolveWorkspaceRoot() (string, DoctorCheck, bool) {
	return s.resolveWorkspaceRoot()
}

// resolveWorkspaceRoot returns the configured workspace root, a DoctorCheck
// describing its state, and whether downstream workspace-root-dependent
// checks should run. ok=false when the root is unset or missing on disk.
func (s *Server) resolveWorkspaceRoot() (string, DoctorCheck, bool) {
	root := WorkspaceRootFromEnv()
	if root == "" {
		root = s.settings.Get("workspace_root")
	}
	check, ok := WorkspaceRootCheck(root)
	return root, check, ok
}

func serviceHealthCheck(services []engine.ServiceInfo) DoctorCheck {
	var healthy, stopped int
	var degraded, degradedNames, changing []string
	for i := range services {
		service := &services[i]
		switch service.State {
		case engine.StateHealthy:
			healthy++
		case engine.StateStopped:
			stopped++
		case engine.StateDegraded:
			degradedNames = append(degradedNames, service.Name)
			detail := service.Name
			if service.StateReason != "" {
				detail += " — " + service.StateReason
			}
			degraded = append(degraded, detail)
		default:
			changing = append(changing, fmt.Sprintf("%s (%s)", service.Name, service.State))
		}
	}
	if len(degraded) > 0 {
		hint := "run: orbit status"
		if len(degraded) == 1 {
			hint = "run: orbit logs " + degradedNames[0]
		}
		return DoctorCheck{
			Name:    "Daemon",
			Status:  CheckFail,
			Message: fmt.Sprintf("%d degraded: %s", len(degraded), strings.Join(degraded, "; ")),
			Hint:    hint,
		}
	}
	if len(changing) > 0 {
		return DoctorCheck{
			Name:    "Daemon",
			Status:  CheckWarn,
			Message: fmt.Sprintf("%d still changing: %s", len(changing), strings.Join(changing, "; ")),
			Hint:    "run: orbit status",
		}
	}
	if healthy == len(services) {
		return DoctorCheck{Name: "Daemon", Status: CheckPass, Message: fmt.Sprintf("All healthy (%d/%d)", healthy, len(services))}
	}
	return DoctorCheck{
		Name:    "Daemon",
		Status:  CheckPass,
		Message: fmt.Sprintf("Daemon running; %d healthy, %d stopped", healthy, stopped),
	}
}
