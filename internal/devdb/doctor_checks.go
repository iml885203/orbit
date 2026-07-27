package devdb

// The DB-workflow doctor group — moved from the core doctor when the
// feature became extension-owned (spec B6); registered via
// daemon.DoctorRegistrar in daemonSetup.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	checks := []daemon.DoctorCheck{rootCheck}
	checks = append(checks, f.sqlProjectChecks()...)
	checks = append(checks, f.sqlImageChecks()...)
	checks = append(checks, publishToolchainChecks()...)
	return checks
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
			check.Status = daemon.CheckFail
			check.Message += " (file not found)"
			check.Hint = "Fix sqlserver.projects in the active environment"
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
