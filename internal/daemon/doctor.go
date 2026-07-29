package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/dockerctx"
	"github.com/iml885203/orbit/internal/engine"
	"github.com/iml885203/orbit/internal/shellquote"
	"github.com/iml885203/orbit/port"
	"github.com/moby/moby/client"
)

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
	results := ServiceWorkingDirectoryChecks(cfg, nil)
	results = append(results, configuredPythonInterpreterChecks(cfg)...)
	toolOffset := len(results)
	results = append(results, make([]DoctorCheck, len(tools))...)

	var wg sync.WaitGroup
	for i, tool := range tools {
		wg.Add(1)
		go func(i int, tool HostToolCheck) {
			defer wg.Done()
			results[toolOffset+i] = checkHostTool(tool)
		}(i, tool)
	}
	wg.Wait()

	results = append(results, projectDependencyChecks(cfg)...)
	return results
}

func ServiceWorkingDirectoryChecks(cfg *config.Config, selected []string) []DoctorCheck {
	if cfg == nil {
		return nil
	}
	filterEnabled := selected != nil
	filter := make(map[string]bool, len(selected))
	for _, name := range selected {
		filter[name] = true
	}
	names := make([]string, 0, len(cfg.Services))
	for name := range cfg.Services {
		if !filterEnabled || filter[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	var checks []DoctorCheck
	workspaceRoot := WorkspaceRootFromEnv()
	for _, name := range names {
		service := cfg.Services[name]
		if service == nil {
			continue
		}
		path := service.Path
		unresolved := strings.Contains(path, "${")
		pathVariable := unresolvedPathVariable(path)
		info, err := os.Stat(path)
		valid := err == nil
		if service.Type != "dotnet" {
			valid = valid && info.IsDir()
		}
		if valid && !unresolved {
			continue
		}
		check := DoctorCheck{
			Name:   "Working directory (" + name + ")",
			Status: CheckFail,
		}
		if unresolved {
			if pathVariable != "" {
				check.Message = "path variable " + pathVariable + " is unresolved in " + path
			} else {
				check.Message = "path variable is unresolved in " + path
			}
		} else if err != nil {
			check.Message = "working directory not found: " + path
		} else {
			check.Message = "working directory is not a directory: " + path
		}
		if pathVariable == "WORKSPACE_ROOT" || pathUsesWorkspaceRoot(path, workspaceRoot) {
			check.Hint = `run: orbit settings set workspace-root "$PWD"`
		} else if pathVariable != "" {
			check.Hint = `run: orbit settings set-env ` + pathVariable + ` "$PWD"`
		} else if unresolved {
			check.Hint = "Set the path variable or update services." + name + ".path"
		} else {
			check.Hint = "Update services." + name + ".path to an existing project directory"
		}
		checks = append(checks, check)
	}
	return checks
}

var pathVariablePattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)`)

func unresolvedPathVariable(path string) string {
	match := pathVariablePattern.FindStringSubmatch(path)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func pathUsesWorkspaceRoot(path, root string) bool {
	if root == "" {
		return false
	}
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	return cleanPath == cleanRoot || strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator))
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

	type checkTask func() (DoctorCheck, bool)
	var tasks []checkTask
	for _, name := range names {
		name := name
		service := cfg.Services[name]
		if service == nil {
			continue
		}
		switch service.Type {
		case "node":
			manager := commandBinary(service.Command)
			if !isNodePackageManager(manager) {
				continue
			}
			tasks = append(tasks, func() (DoctorCheck, bool) {
				return nodeProjectDependencyCheck(name, service.Path, manager), true
			})
		case "python":
			tasks = append(tasks, func() (DoctorCheck, bool) {
				return pythonProjectDependencyCheck(name, service)
			})
		}
	}

	results := make([]DoctorCheck, len(tasks))
	supported := make([]bool, len(tasks))
	var wg sync.WaitGroup
	for i, task := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], supported[i] = task()
		}()
	}
	wg.Wait()

	checks := make([]DoctorCheck, 0, len(results))
	for i := range results {
		if supported[i] {
			checks = append(checks, results[i])
		}
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
				if strings.ContainsAny(commandExecutable(service.Command), `/\`) {
					continue
				}
				add(binary, name)
			default:
				if isKnownRuntime(binary) {
					add(binary, name)
				}
			}
		}
	}

	var tools []HostToolCheck
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
	return filepath.Base(commandExecutable(command))
}

func commandExecutable(command string) string {
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
	return fields[0]
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
	checks = append(checks, liveServiceHealthChecks(services)...)

	checks = append(checks, HostEnvironmentChecks(s.Config())...)
	checks = append(checks, checkOrbitOnPath())

	// Feature-owned check groups (the DB workflow) come from registered
	// contributors.
	for _, contribute := range s.doctorContributors {
		checks = append(checks, contribute()...)
	}
	return checks
}

func liveServiceHealthChecks(services []engine.ServiceInfo) []DoctorCheck {
	current := append([]engine.ServiceInfo(nil), services...)
	var checks []DoctorCheck
	var retry []string
	var occupied bool
	for i := range current {
		service := &current[i]
		if service.State != engine.StateDegraded || service.PortConflict == nil {
			continue
		}
		portNumber := service.PortConflict.Port
		conflicts := port.CheckPorts(map[string][]int{service.Name: {portNumber}})
		service.State = engine.StateStopped
		service.StateReason = ""
		service.PortConflict = nil
		if len(conflicts) == 0 {
			checks = append(checks, DoctorCheck{
				Name:    fmt.Sprintf("Port %d", portNumber),
				Status:  CheckPass,
				Message: "available (" + service.Name + "); previous conflict resolved",
			})
			retry = append(retry, service.Name)
			continue
		}
		occupied = true
		conflict := port.NewConflictError(conflicts[0])
		checks = append(checks, DoctorCheck{
			Name:    fmt.Sprintf("Port %d", portNumber),
			Status:  CheckFail,
			Message: conflict.Error(),
			Hint:    "run: " + conflict.InspectCommand,
		})
	}

	health := serviceHealthCheck(current)
	if len(retry) > 0 && !occupied && health.Status == CheckPass {
		sort.Strings(retry)
		command := "orbit up " + strings.Join(retry, " ")
		health.Message = strings.Join(retry, ", ") + " ready to retry; previous port conflict resolved"
		health.Hint = "run: " + command
	}
	return append(checks, health)
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
			if service.FailureEvidence != "" && service.FailureEvidence != service.StateReason {
				detail += " — " + service.FailureEvidence
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
	message := fmt.Sprintf("%d healthy, %d stopped", healthy, stopped)
	if healthy == 0 && stopped == len(services) {
		message = fmt.Sprintf("All stopped (0/%d)", len(services))
	}
	check := DoctorCheck{Name: "Daemon", Status: CheckPass, Message: message}
	if healthy == 0 && stopped == len(services) {
		check.Hint = "run: orbit up"
	}
	return check
}
