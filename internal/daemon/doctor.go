package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iml885203/orbit/dockerctx"
	"github.com/iml885203/orbit/internal/engine"
	"github.com/moby/moby/client"
)

// HostToolChecks is the ordered list of host CLI dependencies orbit expects on
// PATH. Critical tools (docker, dotnet) produce CheckFail when missing; the
// rest warn.
var HostToolChecks = []HostToolCheck{
	{
		Name:     "dotnet",
		Binary:   "dotnet",
		Critical: true,
		Hint:     "Install the .NET SDK: https://dotnet.microsoft.com/download",
		Version:  dotnetSDKVersion,
	},
	{
		Name:     "git",
		Binary:   "git",
		Critical: false,
		Hint:     "Install git (brew install git or your distro's package manager)",
		Version:  versionFromCmd("--version"),
	},
	{
		Name:     "pnpm",
		Binary:   "pnpm",
		Critical: false,
		Hint:     "Install pnpm: npm install -g pnpm (or brew install pnpm)",
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
	path, err := exec.LookPath(c.Binary)
	if err != nil {
		status := CheckWarn
		if c.Critical {
			status = CheckFail
		}
		return DoctorCheck{
			Name:    c.Name,
			Status:  status,
			Message: c.Binary + " not found on PATH",
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
	return DoctorCheck{Name: c.Name, Status: CheckPass, Message: msg}
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

// minNodeMajor is the lowest Node.js major version orbit-managed services
// are known to run on.
const minNodeMajor = 22

// checkNode probes for a node binary on PATH and its major version. Missing
// binary warns (non-critical); binary present but major < minNodeMajor also
// warns; otherwise passes with the reported version.
func checkNode() DoctorCheck {
	path, err := exec.LookPath("node")
	if err != nil {
		return DoctorCheck{
			Name:    "Node.js",
			Status:  CheckWarn,
			Message: "node not found on PATH",
			Hint:    "Install Node.js >=22 (https://nodejs.org or `brew install node`)",
		}
	}
	version, err := versionFromCmd("--version")(path)
	if err != nil {
		slog.Debug("version probe failed", "component", "doctor", "tool", "node", "err", err)
		return DoctorCheck{Name: "Node.js", Status: CheckPass, Message: "found at " + path}
	}
	major := parseNodeMajor(version)
	if major > 0 && major < minNodeMajor {
		return DoctorCheck{
			Name:    "Node.js",
			Status:  CheckWarn,
			Message: fmt.Sprintf("found at %s (%s) — orbit services expect >=%d", path, version, minNodeMajor),
			Hint:    fmt.Sprintf("Upgrade Node.js to >=%d", minNodeMajor),
		}
	}
	return DoctorCheck{Name: "Node.js", Status: CheckPass, Message: fmt.Sprintf("found at %s (%s)", path, version)}
}

// parseNodeMajor extracts the leading major number from "v22.3.1" style
// output. Returns 0 on unparseable input (caller treats as pass).
func parseNodeMajor(v string) int {
	v = strings.TrimPrefix(v, "v")
	dot := strings.Index(v, ".")
	if dot <= 0 {
		return 0
	}
	n, err := strconv.Atoi(v[:dot])
	if err != nil {
		return 0
	}
	return n
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

	checks = append(checks, DockerCheck())

	configPath := s.ConfigPath()
	checks = append(checks, DoctorCheck{Name: "Config", Status: CheckInfo, Message: configPath})

	services := s.app.Orchestrator.GetAllServices()
	healthy := 0
	for i := range services {
		if services[i].State == engine.StateHealthy {
			healthy++
		}
	}
	checks = append(checks, DoctorCheck{Name: "Daemon", Status: CheckPass, Message: formatDaemonMsg(healthy, len(services))})

	// Host CLI tools orbit shells out to. Probes run in parallel so a slow
	// or hung binary cannot stall the whole doctor response; ordering is
	// preserved via index-keyed result slots.
	hostResults := make([]DoctorCheck, len(HostToolChecks))
	var hostWG sync.WaitGroup
	for i, tool := range HostToolChecks {
		hostWG.Add(1)
		go func(i int, t HostToolCheck) {
			defer hostWG.Done()
			hostResults[i] = checkHostTool(t)
		}(i, tool)
	}
	hostWG.Wait()
	checks = append(checks, hostResults...)
	checks = append(checks, checkNode(), checkOrbitOnPath())

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
