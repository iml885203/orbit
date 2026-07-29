package app

import (
	"os"
	"path/filepath"
)

const projectConfigName = "orbit.yaml"

func resolveConfigFile() string {
	if path := discoverProjectConfig(); path != "" {
		return path
	}
	if path := readCurrentEnv(); path != "" {
		return path
	}
	if distribution.DefaultEnv == "" {
		return ""
	}
	return filepath.Join(envsDestDir(), distribution.DefaultEnv)
}

func discoverProjectConfig() string {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return ""
	}
	return findProjectConfig(workingDirectory)
}

func findProjectConfig(start string) string {
	current, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(current, projectConfigName)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func usesDiscoveredProjectConfig(path string) bool {
	discovered := discoverProjectConfig()
	return discovered != "" && sameFilePath(discovered, path)
}
