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
			Hint:    c.Hint,
		}
	}
	msg := "found at " + path
	if c.Version != nil {
		v, err := c.Version(path)
		if err != nil {
			slog.Debug("version probe failed", "component", "doctor", "tool", c.Binary, "err", err)
		} else if v != "" {
			msg = fmt.Sprintf("%s (%s)", msg, v)
		}
	}
	msg += requiredBy
	return DoctorCheck{Name: c.Name, Status: CheckPass, Message: msg}
}

// HostEnvironmentChecks reports only the tools the selected environment
// actually needs. Optional Orbit features contribute their own checks, so a
// Python-only project never has to understand Node, .NET, or SQL tooling.
func HostEnvironmentChecks(cfg *config.Config) []DoctorCheck {
	tools, nodeServices := requiredHostTools(cfg)
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

	if len(nodeServices) > 0 {
		results = append(results, checkNode(nodeServices))
	}
	return results
}

func requiredHostTools(cfg *config.Config) ([]HostToolCheck, []string) {
	requirements := map[string]map[string]bool{}
	add := func(binary, service string) {
		if requirements[binary] == nil {
			requirements[binary] = map[string]bool{}
		}
		requirements[binary][service] = true
	}

	var nodeServices []string
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
				nodeServices = append(nodeServices, name)
				if isNodePackageManager(binary) {
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
	for _, binary := range []string{"dotnet", "python", "python3", "uv", "poetry", "npm", "pnpm", "yarn", "bun"} {
		services := sortedRequirementNames(requirements[binary])
		if len(services) == 0 {
			continue
		}
		tools = append(tools, hostToolDefinition(binary, services))
	}
	return tools, nodeServices
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

// checkOrbitOnPath verifies the directory holding the running orbit binary
// is present in $PATH. Both sides are resolved via EvalSymlinks so a
// symlinked install dir (e.g. /usr/local/bin → /opt/homebrew/bin) doesn't
// cause a false negative.
func checkOrbitOnPath() DoctorCheck {
	exe, err := os.Executable()
	if err != nil {
		return DoctorCheck{Name: "PATH", Status: CheckInfo, Message: "cannot resolve orbit binary: " + err.Error()}
	}
	exeResolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		exeResolved = exe
	}
	exeDir := filepath.Dir(exeResolved)

	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if entry == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(entry)
		if err != nil {
			resolved = entry
		}
		if resolved == exeDir {
			return DoctorCheck{Name: "PATH", Status: CheckPass, Message: "orbit dir " + exeDir + " is on PATH"}
		}
	}
	return DoctorCheck{
		Name:    "PATH",
		Status:  CheckWarn,
		Message: "orbit installed at " + exeDir + " but not on $PATH",
		Hint:    "Add " + exeDir + " to your shell's PATH",
	}
}

// checkNode probes the runtime only when the selected environment has a Node
// service. Project-specific version files are the authority for acceptable
// versions; Orbit does not impose its own Node release on user services.
func checkNode(requiredBy []string) DoctorCheck {
	suffix := " (required by " + strings.Join(requiredBy, ", ") + ")"
	path, err := exec.LookPath("node")
	if err != nil {
		return DoctorCheck{
			Name:    "Node.js",
			Status:  CheckFail,
			Message: "node not found on PATH" + suffix,
			Hint:    "Install the version required by your project: https://nodejs.org/",
		}
	}
	version, err := versionFromCmd("--version")(path)
	if err != nil {
		slog.Debug("version probe failed", "component", "doctor", "tool", "node", "err", err)
		return DoctorCheck{Name: "Node.js", Status: CheckPass, Message: "found at " + path + suffix}
	}
	return DoctorCheck{Name: "Node.js", Status: CheckPass, Message: fmt.Sprintf("found at %s (%s)%s", path, version, suffix)}
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
	healthy := 0
	for i := range services {
		if services[i].State == engine.StateHealthy {
			healthy++
		}
	}
	checks = append(checks, DoctorCheck{Name: "Daemon", Status: CheckPass, Message: formatDaemonMsg(healthy, len(services))})

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

func formatDaemonMsg(healthy, total int) string {
	if healthy == total {
		return fmt.Sprintf("All healthy (%d/%d)", healthy, total)
	}
	return fmt.Sprintf("%d/%d healthy", healthy, total)
}
