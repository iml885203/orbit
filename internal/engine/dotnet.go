package engine

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type dotnetProject struct {
	PropertyGroups []dotnetPropertyGroup `xml:"PropertyGroup"`
}

type dotnetPropertyGroup struct {
	TargetFramework  string `xml:"TargetFramework"`
	TargetFrameworks string `xml:"TargetFrameworks"`
}

// resolveDotnetAssemblyPath locates the built .dll for a .NET project. It
// prefers the framework declared in the .csproj so multi-TFM projects resolve
// the correct build output, falling back to the newest .dll under bin/Debug/*
// when the project cannot be read or the expected path is absent.
func resolveDotnetAssemblyPath(dir, proj string) (string, error) {
	assembly := strings.TrimSuffix(proj, filepath.Ext(proj)) + ".dll"

	if targetFramework, err := readDotnetTargetFramework(filepath.Join(dir, proj)); err == nil && targetFramework != "" {
		candidate := filepath.Join(dir, "bin", "Debug", targetFramework, assembly)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "bin", "Debug", "*", assembly))
	if len(matches) == 0 {
		return "", fmt.Errorf("build output not found: %s", assembly)
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
