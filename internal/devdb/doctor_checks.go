package devdb

// The DB-workflow doctor group — moved from the core doctor when the
// feature became extension-owned (spec B6); registered via
// daemon.DoctorRegistrar in daemonSetup.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/sqlpublish"
)

// dbWorkflowChecks is the DB-workflow group of doctor checks: workspace
// root, the optional db-root override, SQL image presence, and the
// publish toolchain. For an adopting team without a sql-server container
// all of these would be red noise, so the group collapses to a single
// informational skip.
func (f *dbFeature) dbWorkflowChecks() []daemon.DoctorCheck {
	if !f.dbWorkflowConfigured() {
		return nil
	}

	_, rootCheck, _ := f.host.ResolveWorkspaceRoot()
	checks := []daemon.DoctorCheck{rootCheck}

	// ORBIT_DB_ROOT — optional override. Only reported when configured.
	if dbCheck, ok := f.resolveDBRoot(); ok {
		checks = append(checks, dbCheck)
	}

	checks = append(checks, f.sqlImageChecks()...)
	checks = append(checks, publishToolchainChecks()...)
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

// resolveDBRoot returns a DoctorCheck describing the ORBIT_DB_ROOT
// setting (env var winning over settings, with the legacy env var as a
// fallback — see settings_legacy.go). The second return is false when
// the override is not configured — the caller should omit the check in
// that case since it's optional (devdb falls back to
// <workspace_root>[/dbprojects]).
func (f *dbFeature) resolveDBRoot() (daemon.DoctorCheck, bool) {
	root := resolveDBRootPath(f.host.Settings())
	if root == "" {
		return daemon.DoctorCheck{}, false
	}
	if _, err := os.Stat(root); err != nil {
		return daemon.DoctorCheck{
			Name:    "DB Root",
			Status:  daemon.CheckFail,
			Message: root + " (path not found)",
			Hint:    "Update ORBIT_DB_ROOT (env or settings 'db_root') to point at an existing directory",
		}, true
	}
	projects := findSQLProjectDirs(root)
	if len(projects) == 0 {
		return daemon.DoctorCheck{
			Name:    "DB Root",
			Status:  daemon.CheckWarn,
			Message: fmt.Sprintf("%s (no SQL projects found)", root),
			Hint:    "Expected a directory containing SQL project subdirectories (each with a <project>/*/*.sqlproj layout)",
		}, true
	}
	return daemon.DoctorCheck{
		Name:    "DB Root",
		Status:  daemon.CheckPass,
		Message: fmt.Sprintf("%s (%d SQL project(s))", root, len(projects)),
	}, true
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
			Status:  daemon.CheckFail,
			Message: "dotnet SDK not found — SQL project commands unavailable",
			Hint:    "Install the .NET SDK: https://dotnet.microsoft.com/download",
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
			Name: "Publish Toolchain", Status: daemon.CheckWarn,
			Message: "sqlpackage not found — `orbit db publish` unavailable",
			Hint:    sqlpublish.InstallHint,
		})
	}
	return append(checks, daemon.DoctorCheck{
		Name: "Publish Toolchain", Status: daemon.CheckPass,
		Message: "sqlpackage " + v,
	})
}
