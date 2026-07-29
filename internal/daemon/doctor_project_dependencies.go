package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/internal/shellquote"
)

const projectDependencyProbeTimeout = 10 * time.Second

func pythonProjectDependencyCheck(serviceName string, service *config.Service) (DoctorCheck, bool) {
	requirementsPath, interpreter, ok := pythonRequirementsManifest(service)
	if !ok {
		return DoctorCheck{}, false
	}
	check := DoctorCheck{Name: "Packages (" + serviceName + ")"}
	if _, err := os.Stat(requirementsPath); err != nil {
		return DoctorCheck{}, false
	}

	interpreterPath, err := resolveProjectCommand(interpreter, service.Path)
	if err != nil {
		check.Status = CheckFail
		check.Message = interpreter + " is unavailable, so Orbit cannot check " + filepath.Base(requirementsPath)
		check.Hint = "Install the required Python runtime before project packages"
		return check, true
	}
	setupCommand := pythonRequirementsSetupCommand(
		interpreterPath,
		requirementsPath,
		pythonInterpreterMode(interpreterPath, service.Path),
	)

	ctx, cancel := context.WithTimeout(context.Background(), projectDependencyProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		interpreterPath,
		"-m", "pip", "install",
		"--dry-run",
		"--no-index",
		"--disable-pip-version-check",
		"-r", requirementsPath,
	)
	cmd.Dir = service.Path
	cmd.Env = append(os.Environ(), "PIP_BREAK_SYSTEM_PACKAGES=1", "PIP_NO_INPUT=1")
	if _, err := cmd.CombinedOutput(); err == nil {
		check.Status = CheckPass
		check.Message = filepath.Base(requirementsPath) + " is satisfied for " + serviceName
		return check, true
	} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		check.Status = CheckFail
		check.Message = "timed out checking " + filepath.Base(requirementsPath) + " for " + serviceName
		check.Hint = "run: " + setupCommand
		return check, true
	}

	check.Status = CheckFail
	check.Message = filepath.Base(requirementsPath) + " is not satisfied for " + serviceName
	check.Hint = "run: " + setupCommand
	return check, true
}

// ProjectDependencySetupCommand reports the explicit setup action for a
// service whose supported dependency manifest is currently unsatisfied.
// Orbit only probes here; running the returned command remains the user's
// choice.
func ProjectDependencySetupCommand(cfg *config.Config, serviceName string) (string, bool) {
	if cfg == nil {
		return "", false
	}
	service := cfg.Services[serviceName]
	if service == nil {
		return "", false
	}
	var check DoctorCheck
	var supported bool
	switch service.Type {
	case "node":
		check, supported = nodeDependencyCheckForService(serviceName, service)
	case "python":
		check, supported = pythonProjectDependencyCheck(serviceName, service)
	}
	if !supported || check.Status != CheckFail {
		return "", false
	}
	command, ok := strings.CutPrefix(check.Hint, "run: ")
	return strings.TrimSpace(command), ok && strings.TrimSpace(command) != ""
}

func pythonRequirementsManifest(service *config.Service) (requirementsPath, interpreter string, ok bool) {
	if service == nil || service.Type != "python" {
		return "", "", false
	}
	interpreter = commandExecutable(service.Command)
	if interpreter == "" {
		interpreter = "python3"
	}
	if !isPythonInterpreter(interpreter) {
		return "", "", false
	}
	requirementsPath = filepath.Join(service.Path, "requirements.txt")
	return requirementsPath, interpreter, true
}

func pythonRequirementsSetupCommand(interpreter, requirementsPath, mode string) string {
	parts := []string{
		shellquote.Quote(interpreter),
		"-m pip install",
	}
	switch mode {
	case "managed":
		parts = append(parts, "--user", "--break-system-packages")
	case "system":
		parts = append(parts, "--user")
	}
	parts = append(parts, "-r", shellquote.Quote(requirementsPath))
	return strings.Join(parts, " ")
}

func pythonInterpreterMode(interpreter, projectPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), hostToolVersionTimeout)
	defer cancel()
	script := `import pathlib,sys,sysconfig; print("venv" if sys.prefix != sys.base_prefix else ("managed" if (pathlib.Path(sysconfig.get_path("stdlib")) / "EXTERNALLY-MANAGED").exists() else "system"))`
	cmd := exec.CommandContext(ctx, interpreter, "-c", script)
	cmd.Dir = projectPath
	output, err := cmd.Output()
	if err != nil {
		return "system"
	}
	switch strings.TrimSpace(string(output)) {
	case "venv", "managed", "system":
		return strings.TrimSpace(string(output))
	default:
		return "system"
	}
}

func isPythonInterpreter(command string) bool {
	name := strings.ToLower(filepath.Base(command))
	return name == "python" || name == "python3" || strings.HasPrefix(name, "python3.")
}

func resolveProjectCommand(command, projectPath string) (string, error) {
	if filepath.IsAbs(command) {
		_, err := os.Stat(command)
		return command, err
	}
	if strings.ContainsAny(command, `/\`) {
		path := filepath.Join(projectPath, command)
		_, err := os.Stat(path)
		return path, err
	}
	return exec.LookPath(command)
}
