// Package instance owns named Orbit runtime identity and filesystem isolation.
package instance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	EnvName     = "ORBIT_INSTANCE"
	EnvBaseHome = "ORBIT_INSTANCE_BASE_HOME"
)

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)

type Runtime struct {
	Name      string
	Home      string
	Namespace string
}

func ValidateName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid instance name %q: use 1-63 letters, numbers, dots, underscores, or hyphens, starting with a letter or number", name)
	}
	return nil
}

func BaseHome() string {
	if base := os.Getenv(EnvBaseHome); base != "" {
		return base
	}
	if home := os.Getenv("ORBIT_HOME"); home != "" {
		return home
	}
	home, _ := os.UserHomeDir()
	if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
		return filepath.Join(localApp, "orbit")
	}
	return filepath.Join(home, ".orbit")
}

func Resolve(baseHome, name string) (Runtime, error) {
	if err := ValidateName(name); err != nil {
		return Runtime{}, err
	}
	if strings.TrimSpace(baseHome) == "" {
		return Runtime{}, errors.New("instance base home is empty")
	}
	sum := sha256.Sum256([]byte(name))
	namespace := "instance-" + sanitizeNamespace(name) + "-" + hex.EncodeToString(sum[:4])
	return Runtime{
		Name:      name,
		Home:      filepath.Join(baseHome, "instances", name),
		Namespace: namespace,
	}, nil
}

func Activate(name string) (Runtime, error) {
	base := BaseHome()
	runtime, err := Resolve(base, name)
	if err != nil {
		return Runtime{}, err
	}
	if err := os.MkdirAll(runtime.Home, 0o755); err != nil {
		return Runtime{}, fmt.Errorf("creating instance home: %w", err)
	}
	for key, value := range map[string]string{
		EnvName:           runtime.Name,
		EnvBaseHome:       base,
		"ORBIT_HOME":      runtime.Home,
		"ORBIT_NAMESPACE": runtime.Namespace,
	} {
		if err := os.Setenv(key, value); err != nil {
			return Runtime{}, fmt.Errorf("activating instance: %w", err)
		}
	}
	return runtime, nil
}

func CurrentName() string { return os.Getenv(EnvName) }

func sanitizeNamespace(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
