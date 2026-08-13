package devdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/sqlpublish"
)

// dbWorkflowChecks reports only checks required by an explicitly configured
// SQL Server workflow.
func (f *dbFeature) dbWorkflowChecks() []daemon.DoctorCheck {
	if !f.dbWorkflowConfigured() {
		return nil
	}

	_, rootCheck, _ := f.host.ResolveWorkspaceRoot()
	checks := make([]daemon.DoctorCheck, 0, 4)
	checks = append(checks, rootCheck)
	checks = append(checks, f.sqlProjectChecks()...)
	checks = append(checks, sqlServerReadinessChecks(f.host.Config())...)
	checks = append(checks, f.sqlImageChecks()...)
	checks = append(checks, publishToolchainChecks()...)
	return checks
}

// sqlServerReadinessChecks warns when the SQL Server target's probe cannot
// prove a login, and is the only doctor check whose remedy carries an ongoing
// cost: the exec probe it recommends keeps running as the liveness check, one
// authenticated login per interval.
//
// Every other doctor hint asks for a one-off action — install a tool, log in,
// add a PATH entry, read a log, restore Docker, set a workspace root, create
// an executable, run a setup command. Checked as of 2026-08 over every file
// producing a DoctorCheck:
//
//	git ls-files '*.go' | grep -v _test | xargs grep -l 'DoctorCheck{'
//
// That enumeration matters more than any count here. Three earlier passes
// were wrong because each searched a category it had named rather than one
// the repo defines: "Hint string literals" missed the ones built in
// variables, `Hint:` missed the files that assign `check.Hint`, and both
// missed files outside the two directories picked before searching.
//
// Anchoring on the type rather than on how a hint is written holds because a
// composite literal is the only way to construct a DoctorCheck — files that
// merely declare, pass or aggregate them add no hints, and the three files
// that set a Hint without constructing one (app/root.go, cli/json_contract.go,
// internal/sqlpublish/tools.go) are CLI error hints and a shared constant,
// not doctor remedies.
//
// A new hint recommending something continuous belongs in this minority and
// should say so in its own text, the way this one does.
func sqlServerReadinessChecks(cfg *config.Config) []daemon.DoctorCheck {
	section := SQLServerFrom(cfg)
	if section == nil {
		return nil
	}
	target, ok := cfg.Containers[section.Target]
	if !ok || target == nil {
		return nil
	}
	// The empty-type arm is defensive only: config validation rejects a
	// health_check without a type, before and after an extends merge, so an
	// empty Type cannot reach here. Kept because the checker would treat it
	// as tcp if it ever did.
	if target.HealthCheck != nil && target.HealthCheck.Type != "" && target.HealthCheck.Type != "tcp" {
		return nil
	}

	probe := "has no explicit health check"
	if target.HealthCheck != nil {
		probe = "uses a tcp health check"
	}
	// An exec probe keeps running after startup (MonitorHealthy), so a
	// long-lived environment pays one login per interval indefinitely.
	// Naming that here keeps the reader from adopting a background cost
	// they only meant to pay while waiting for the server to come up.
	hint := fmt.Sprintf(
		"set containers.%s.health_check to:\n  type: exec\n  command: [/bin/sh, -c, 'password=\"$(printenv \"$1\")\"; if [ -z \"$password\" ]; then echo \"$1 is empty in the configured SQL Server target\" >&2; exit 2; fi; export SQLCMDPASSWORD=\"$password\"; exec /opt/mssql-tools18/bin/sqlcmd -S localhost -U \"$2\" -C -I -Q \"SELECT 1\"', orbit-sqlserver-health, %s, %s]\n"+
			"this probe also runs while the environment is up, one login per health_check.interval (5s default) — raise interval for an environment you leave running",
		section.Target, strconv.Quote(section.PasswordEnv), strconv.Quote(section.Username),
	)
	return []daemon.DoctorCheck{{
		Name:    "SQL Server Readiness",
		Status:  daemon.CheckWarn,
		Message: fmt.Sprintf("sqlserver.target %q %s, which cannot prove SQL Server accepts logins", section.Target, probe),
		Hint:    hint,
	}}
}

func (f *dbFeature) sqlProjectChecks() []daemon.DoctorCheck {
	return sqlProjectChecks(f.host.Config(), f.workspaceRoot())
}

func sqlProjectChecks(cfg *config.Config, root string) []daemon.DoctorCheck {
	section := SQLServerFrom(cfg)
	if section == nil || root == "" {
		return nil
	}
	checks := make([]daemon.DoctorCheck, 0, len(section.Projects))
	for _, project := range section.Projects {
		path := filepath.Join(root, project.Path)
		check := daemon.DoctorCheck{Name: "SQL Project", Message: project.Path}
		info, err := os.Stat(path)
		switch {
		case err != nil:
			check.Status = daemon.CheckWarn
			check.Message += " (file not found)"
			check.Hint = "Restore the source checkout for builds, or use --dacpac-dir with prebuilt artifacts"
		case info.IsDir():
			check.Status = daemon.CheckFail
			check.Message += " (expected a .sqlproj file, found a directory)"
			check.Hint = "Point sqlserver.projects[].path at the project file"
		default:
			check.Status = daemon.CheckPass
			check.Message += " (found)"
		}
		checks = append(checks, check)
	}
	return checks
}

// sqlImageChecks reports whether the SQL Server image the env declares is
// present locally, plus registry pull access.
func (f *dbFeature) sqlImageChecks() []daemon.DoctorCheck {
	c, ok := f.sqlServerContainerConfig()
	if !ok {
		return nil
	}
	var checks []daemon.DoctorCheck
	if f.host.Containers().ImageExists(c.Image) {
		checks = append(checks, daemon.DoctorCheck{Name: "SQL Image", Status: daemon.CheckPass, Message: fmt.Sprintf("%s (cached)", c.Image)})
	} else {
		checks = append(checks, daemon.DoctorCheck{Name: "SQL Image", Status: daemon.CheckWarn, Message: fmt.Sprintf("%s (not cached — will pull on next start)", c.Image)})
	}
	return append(checks, f.checkRegistryAccess(c.Image))
}

// checkRegistryAccess probes the registry for pull permission on the given
// image. ECR (and other private registries) deny anonymous manifest reads, so
// auth failure here almost always means missing or expired credentials.
func (f *dbFeature) checkRegistryAccess(imageName string) daemon.DoctorCheck {
	registry := registryHost(imageName)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := f.host.Containers().CheckImagePull(ctx, imageName); err != nil {
		hint := loginHint(registry)
		return daemon.DoctorCheck{Name: "SQL Registry Auth", Status: daemon.CheckFail, Message: fmt.Sprintf("%s — %s", err, hint), Hint: hint}
	}
	return daemon.DoctorCheck{Name: "SQL Registry Auth", Status: daemon.CheckPass, Message: "can pull from " + registry}
}

// registryHost returns the registry portion of an image reference, or
// "docker.io" when the image uses a bare name.
func registryHost(imageName string) string {
	slash := strings.Index(imageName, "/")
	if slash == -1 {
		return "docker.io"
	}
	first := imageName[:slash]
	if strings.ContainsAny(first, ".:") || first == "localhost" {
		return first
	}
	return "docker.io"
}

// loginHint returns the command to run to (re-)authenticate to the registry.
// ECR gets a tailored command since it needs aws CLI; everything else gets
// the generic docker login form.
func loginHint(registry string) string {
	if strings.HasSuffix(registry, ".amazonaws.com") {
		region := "ap-east-2"
		parts := strings.Split(registry, ".")
		for i, p := range parts {
			if p == "ecr" && i+1 < len(parts) {
				region = parts[i+1]
				break
			}
		}
		return fmt.Sprintf("run: aws ecr get-login-password --region %s | docker login --username AWS --password-stdin %s", region, registry)
	}
	return "run: docker login " + registry
}

// publishToolchainChecks reports the host tools required by the configured
// SQL Server project workflow. Core doctor checks stay environment-specific,
// so an env without this optional workflow never sees .NET or sqlpackage.
func publishToolchainChecks() []daemon.DoctorCheck {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var checks []daemon.DoctorCheck
	if v, err := sqlpublish.DotnetVersion(ctx); err != nil {
		checks = append(checks, daemon.DoctorCheck{
			Name:    "SQL Server .NET SDK",
			Status:  daemon.CheckWarn,
			Message: "dotnet SDK not found — source builds unavailable",
			Hint:    "Install the .NET SDK, or use --dacpac-dir with prebuilt artifacts",
		})
	} else {
		checks = append(checks, daemon.DoctorCheck{
			Name:    "SQL Server .NET SDK",
			Status:  daemon.CheckPass,
			Message: v,
		})
	}

	v, err := sqlpublish.SqlpackageVersion(ctx)
	if err != nil {
		return append(checks, daemon.DoctorCheck{
			Name: "Publish Toolchain", Status: daemon.CheckFail,
			Message: "sqlpackage not found — `orbit sqlserver publish` unavailable",
			Hint:    sqlpublish.InstallHint,
		})
	}
	return append(checks, daemon.DoctorCheck{
		Name: "Publish Toolchain", Status: daemon.CheckPass,
		Message: "sqlpackage " + v,
	})
}
