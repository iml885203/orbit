package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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
			Hint:    c.Hint,
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
	results = append(results, configuredServiceCommandChecks(cfg)...)
	results = append(results, DependencyReadinessChecks(cfg)...)
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

// DependencyReadinessChecks remains non-blocking because some containers only
// need to be started, not ready, before their dependents launch.
func DependencyReadinessChecks(cfg *config.Config) []DoctorCheck {
	if cfg == nil {
		return nil
	}
	dependents := make(map[string][]string)
	for name, service := range cfg.Services {
		for _, dependency := range service.DependsOn {
			dependents[dependency] = append(dependents[dependency], name)
		}
	}
	for name, container := range cfg.Containers {
		for _, dependency := range container.DependsOn {
			dependents[dependency] = append(dependents[dependency], name)
		}
	}

	names := make([]string, 0, len(dependents))
	for name := range dependents {
		names = append(names, name)
	}
	sort.Strings(names)

	var checks []DoctorCheck
	for _, name := range names {
		var healthCheck *config.HealthCheckConfig
		var ports map[string]config.PortDef
		var configPath string
		if service, ok := cfg.Services[name]; ok {
			healthCheck = service.HealthCheck
			ports = service.Ports
			configPath = "services." + name + ".health_check"
			if healthCheck == nil && len(ports) == 0 {
				continue
			}
		} else if container, ok := cfg.Containers[name]; ok {
			healthCheck = container.HealthCheck
			ports = container.Ports
			configPath = "containers." + name + ".health_check"
		}
		if healthCheck != nil || readinessEndpointIsUnambiguous(ports) {
			continue
		}
		users := append([]string(nil), dependents[name]...)
		sort.Strings(users)
		checks = append(checks, DoctorCheck{
			Name:    "Readiness (" + name + ")",
			Status:  CheckWarn,
			Message: fmt.Sprintf("%s depends on %s, but Orbit cannot infer when %s is ready", strings.Join(users, ", "), name, name),
			Hint:    "Add " + configPath + " so dependents wait for a real readiness signal",
		})
	}
	return checks
}

func readinessEndpointIsUnambiguous(ports map[string]config.PortDef) bool {
	if len(ports) == 1 {
		return true
	}
	_, ok := ports["http"]
	return ok
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
			sourceName := os.Getenv("ORBIT_SOURCE_NAME")
			if sourceName == "" {
				sourceName = "<source>"
			}
			check.Hint = `run: orbit source set-workspace ` + sourceName + ` "$PWD"`
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
			tasks = append(tasks, func() (DoctorCheck, bool) {
				return nodeDependencyCheckForService(name, service)
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
	return coalesceProjectDependencyFailures(checks)
}

func coalesceProjectDependencyFailures(checks []DoctorCheck) []DoctorCheck {
	type group struct {
		first    int
		indices  []int
		services []string
		checks   []DoctorCheck
	}
	groups := make(map[string]*group)
	for i, check := range checks {
		service, packageCheck := packageCheckService(check.Name)
		if !packageCheck || check.Status != CheckFail || !strings.HasPrefix(check.Hint, "run: ") {
			continue
		}
		key := check.Hint
		current := groups[key]
		if current == nil {
			current = &group{first: i}
			groups[key] = current
		}
		current.indices = append(current.indices, i)
		current.services = append(current.services, service)
		current.checks = append(current.checks, check)
	}

	mergedAt := make(map[int]DoctorCheck)
	skipped := make(map[int]bool)
	for _, current := range groups {
		if len(current.checks) < 2 {
			continue
		}
		merged := current.checks[0]
		merged.Name = "Packages (" + strings.Join(current.services, ", ") + ")"
		merged.Message = sharedProjectDependencyMessage(current.checks, current.services)
		mergedAt[current.first] = merged
		for _, index := range current.indices[1:] {
			skipped[index] = true
		}
	}

	result := make([]DoctorCheck, 0, len(checks))
	for i, check := range checks {
		if merged, ok := mergedAt[i]; ok {
			result = append(result, merged)
			continue
		}
		if !skipped[i] {
			result = append(result, check)
		}
	}
	return result
}

func packageCheckService(name string) (string, bool) {
	service, ok := strings.CutPrefix(name, "Packages (")
	if !ok || !strings.HasSuffix(service, ")") {
		return "", false
	}
	return strings.TrimSuffix(service, ")"), true
}

func sharedProjectDependencyMessage(checks []DoctorCheck, services []string) string {
	message := checks[0].Message
	identical := true
	for _, check := range checks[1:] {
		if check.Message != message {
			identical = false
			break
		}
	}
	if identical {
		return message + " (required by " + strings.Join(services, ", ") + ")"
	}

	firstSuffix := " for " + services[0]
	base, ok := strings.CutSuffix(message, firstSuffix)
	if ok {
		for i := 1; i < len(checks); i++ {
			if checks[i].Message != base+" for "+services[i] {
				return "project packages need setup for " + strings.Join(services, ", ")
			}
		}
		return base + " for " + strings.Join(services, ", ")
	}
	return "project packages need setup for " + strings.Join(services, ", ")
}

func nodeDependencyCheckForService(serviceName string, service *config.Service) (DoctorCheck, bool) {
	if service == nil || service.Type != "node" {
		return DoctorCheck{}, false
	}
	manager := commandBinary(service.Command)
	installPath := service.Path
	inferredManager, inferredPath, supported := inferredNodePackageManager(service.Path)
	if isNodePackageManager(manager) {
		if supported && inferredManager == manager {
			installPath = inferredPath
		}
	} else {
		manager, installPath = inferredManager, inferredPath
		if !supported {
			return DoctorCheck{}, false
		}
	}
	managerExecutable := manager
	if executable := commandExecutable(service.Command); commandBinary(service.Command) == manager {
		managerExecutable = executable
	}
	if _, err := resolveProjectCommand(managerExecutable, service.Path); err != nil {
		return DoctorCheck{}, false
	}
	return nodeProjectDependencyCheck(serviceName, service.Path, installPath, manager), true
}

type nodePackageManifest struct {
	PackageManager  string                     `json:"packageManager"`
	Dependencies    map[string]json.RawMessage `json:"dependencies"`
	DevDependencies map[string]json.RawMessage `json:"devDependencies"`
}

func inferredNodePackageManager(path string) (string, string, bool) {
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil {
		return "", "", false
	}
	var manifest nodePackageManifest
	if json.Unmarshal(data, &manifest) != nil {
		return "", "", false
	}
	if len(manifest.Dependencies) == 0 && len(manifest.DevDependencies) == 0 {
		return "", "", false
	}

	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		if current != path {
			if data, err := os.ReadFile(filepath.Join(current, "package.json")); err == nil {
				var parent nodePackageManifest
				if json.Unmarshal(data, &parent) == nil {
					manifest.PackageManager = parent.PackageManager
				}
			}
		}
		if manager := strings.SplitN(manifest.PackageManager, "@", 2)[0]; isNodePackageManager(manager) {
			return manager, current, true
		}
		for _, candidate := range []struct {
			file    string
			manager string
		}{
			{"pnpm-lock.yaml", "pnpm"},
			{"yarn.lock", "yarn"},
			{"bun.lock", "bun"},
			{"bun.lockb", "bun"},
			{"package-lock.json", "npm"},
		} {
			if _, err := os.Stat(filepath.Join(current, candidate.file)); err == nil {
				return candidate.manager, current, true
			}
		}
		parent := filepath.Dir(current)
		_, gitBoundaryErr := os.Stat(filepath.Join(current, ".git"))
		if parent == current || gitBoundaryErr == nil {
			break
		}
	}
	return "npm", path, true
}

func nodeProjectDependencyCheck(service, path, installPath, manager string) DoctorCheck {
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
	if nodePackagesInstalled(path, installPath, manager) {
		check.Status = CheckPass
		check.Message = "installed in " + installPath
		return check
	}
	command := nodeInstallCommand(manager, installPath)
	check.Status = CheckFail
	check.Message = "project packages are not installed"
	check.Hint = "run: " + command
	return check
}

func nodePackagesInstalled(path, installPath, manager string) bool {
	for _, candidate := range []string{path, installPath} {
		if info, err := os.Stat(filepath.Join(candidate, "node_modules")); err == nil && info.IsDir() {
			return true
		}
	}
	if manager == "yarn" {
		for _, candidate := range []string{path, installPath} {
			for _, marker := range []string{".pnp.cjs", ".pnp.js"} {
				if _, err := os.Stat(filepath.Join(candidate, marker)); err == nil {
					return true
				}
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
					if isNodePackageManager(binary) {
						add(binary, name)
					} else if manager, _, ok := inferredNodePackageManager(service.Path); ok {
						add(manager, name)
					}
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
	for _, binary := range []string{"dotnet", "go", "node", "python", "python3", "uv", "poetry", "npm", "pnpm", "yarn", "bun"} {
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
	case "dotnet", "go", "python", "python3", "uv", "poetry", "node", "npm", "pnpm", "yarn", "bun":
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
	case "go":
		tool.Name = "Go"
		tool.Hint = "Install Go: https://go.dev/doc/install"
		tool.Version = versionFromCmd("version")
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
			startDockerRemedy() + ", or run 'docker context use default' to fall back"
	}
	return fallbackMsg, startDockerRemedy()
}

func startDockerRemedy() string {
	if runtime.GOOS == "linux" {
		return "Start Docker (e.g. 'sudo systemctl start docker') or Docker Desktop"
	}
	return "Start Docker Desktop"
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
// describing its state, and whether downstream source-workspace-dependent
// checks should run. ok=false when the root is unset or missing on disk.
func (s *Server) resolveWorkspaceRoot() (string, DoctorCheck, bool) {
	root := WorkspaceRootFromEnv()
	check, ok := WorkspaceRootCheck(root)
	return root, check, ok
}

func serviceHealthCheck(services []engine.ServiceInfo) DoctorCheck {
	var healthy, stopped int
	var degraded, degradedNames, changing []string
	dockerUnavailable := false
	for i := range services {
		service := &services[i]
		switch service.State {
		case engine.StateHealthy:
			healthy++
		case engine.StateStopped:
			stopped++
		case engine.StateDegraded:
			degradedNames = append(degradedNames, service.Name)
			if service.StateReason == engine.DockerObservationUnavailableReason {
				dockerUnavailable = true
			}
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
		if dockerUnavailable {
			hint = "Restore Docker; Orbit reconnects automatically"
		} else if len(degraded) == 1 {
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
