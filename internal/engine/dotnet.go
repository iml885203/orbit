package engine

import (
	"bufio"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type dotnetBuildGate struct {
	slot chan struct{}
}

func newDotnetBuildGate() *dotnetBuildGate {
	return &dotnetBuildGate{slot: make(chan struct{}, 1)}
}

func (g *dotnetBuildGate) acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case g.slot <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-g.slot
			return nil, err
		}
		return func() { <-g.slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (a *App) buildDotnetProject(ctx context.Context, name, dir, project string, environment []string) error {
	release, err := a.dotnetBuilds.acquire(ctx)
	if err != nil {
		return fmt.Errorf("waiting to build %s: %w", project, err)
	}
	defer release()

	build := exec.CommandContext(ctx, "dotnet", "build", project, "-v", "minimal")
	build.Dir = dir
	build.Env = environment
	stdout, err := build.StdoutPipe()
	if err != nil {
		return fmt.Errorf("dotnet build pipe failed: %w", err)
	}
	stderr, err := build.StderrPipe()
	if err != nil {
		return fmt.Errorf("dotnet build pipe failed: %w", err)
	}
	if err := build.Start(); err != nil {
		return fmt.Errorf("dotnet build start failed: %w", err)
	}

	go func() {
		scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
		for scanner.Scan() {
			a.Logs.Write(name, scanner.Text())
		}
	}()
	if err := build.Wait(); err != nil {
		return fmt.Errorf("dotnet build failed: %w", err)
	}
	return nil
}

func dotnetBuildEnvironment(overrides map[string]string) []string {
	environment := append(os.Environ(), "DOTNET_CLI_UI_LANGUAGE=en")
	for key, value := range overrides {
		environment = append(environment, key+"="+value)
	}
	// NuGet audit contacts every configured feed on every build. Defaulting it
	// off keeps offline/private-feed development reliable while preserving an
	// explicit NuGetAudit override from either the host or build_env.
	if _, configured := overrides["NuGetAudit"]; os.Getenv("NuGetAudit") == "" && !configured {
		environment = append(environment, "NuGetAudit=false")
	}
	return environment
}

func quoteProcessArgument(value string) string {
	return strings.ReplaceAll(strconv.Quote(value), "$", `\$`)
}

type dotnetProject struct {
	PropertyGroups []dotnetPropertyGroup `xml:"PropertyGroup"`
}

type dotnetPropertyGroup struct {
	TargetFramework  string `xml:"TargetFramework"`
	TargetFrameworks string `xml:"TargetFrameworks"`
}

func resolveDotnetAssemblyPath(ctx context.Context, dir, proj string, environment []string) (string, error) {
	projectPath := filepath.Join(dir, proj)
	evaluate := exec.CommandContext(ctx, "dotnet", "msbuild", proj, "-getProperty:TargetPath", "-nologo")
	evaluate.Dir = dir
	evaluate.Env = environment
	output, evaluationErr := evaluate.CombinedOutput()
	if evaluationErr == nil {
		targetPath := strings.TrimSpace(string(output))
		if targetPath != "" {
			if !filepath.IsAbs(targetPath) {
				targetPath = filepath.Join(dir, targetPath)
			}
			if _, err := os.Stat(targetPath); err == nil {
				return targetPath, nil
			} else {
				evaluationErr = fmt.Errorf("evaluated .NET build output not found at %s: %w", targetPath, err)
			}
		} else {
			evaluationErr = fmt.Errorf("MSBuild returned an empty TargetPath")
		}
	} else {
		detail := strings.TrimSpace(string(output))
		if detail != "" {
			evaluationErr = fmt.Errorf("evaluating MSBuild TargetPath: %w: %s", evaluationErr, detail)
		} else {
			evaluationErr = fmt.Errorf("evaluating MSBuild TargetPath: %w", evaluationErr)
		}
	}

	assembly := strings.TrimSuffix(proj, filepath.Ext(proj)) + ".dll"

	if targetFramework, err := readDotnetTargetFramework(projectPath); err == nil && targetFramework != "" {
		candidate := filepath.Join(dir, "bin", "Debug", targetFramework, assembly)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "bin", "Debug", "*", assembly))
	if len(matches) == 0 {
		return "", fmt.Errorf("build output not found: %s; %w", assembly, evaluationErr)
	}

	return newestFile(matches), nil
}

func readDotnetTargetFramework(projectPath string) (string, error) {
	f, err := os.Open(projectPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var project dotnetProject
	if err := xml.NewDecoder(f).Decode(&project); err != nil {
		return "", err
	}

	for _, group := range project.PropertyGroups {
		if target := strings.TrimSpace(group.TargetFramework); target != "" {
			return target, nil
		}
		if targets := strings.TrimSpace(group.TargetFrameworks); targets != "" {
			parts := strings.Split(targets, ";")
			for _, part := range parts {
				if target := strings.TrimSpace(part); target != "" {
					return target, nil
				}
			}
		}
	}

	return "", nil
}

func newestFile(paths []string) string {
	newest := paths[0]
	newestInfo, _ := os.Stat(newest)
	for _, path := range paths[1:] {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if newestInfo == nil || info.ModTime().After(newestInfo.ModTime()) {
			newest = path
			newestInfo = info
		}
	}
	return newest
}
