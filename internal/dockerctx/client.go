// Package dockerctx builds Docker SDK clients that honor the active
// `docker context` the same way the docker CLI does.
//
// The upstream github.com/moby/moby/client.FromEnv only reads DOCKER_HOST
// (and DOCKER_TLS_*) env vars — it does NOT read ~/.docker/config.json's
// currentContext or the meta files under ~/.docker/contexts/meta/. So a user
// who runs `docker context use wsl-docker` sees their CLI honour it but any
// SDK-based tool (including orbit) silently falls back to the platform default
// endpoint. This package closes that gap.
package dockerctx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/moby/moby/client"
)

// ContextInfo describes the active Docker context selection. Name is empty
// when no named context is in effect (DOCKER_HOST, config.json=default, or
// nothing set); Host is "" when the caller should fall back to SDK defaults.
type ContextInfo struct {
	Name string
	Host string
}

// Active reports how orbit will connect to Docker, without actually opening a
// connection. Useful for diagnostics (`orbit doctor`) that want to tell the
// user which context they're on before a connection attempt succeeds or fails.
func Active() ContextInfo {
	if os.Getenv("DOCKER_HOST") != "" {
		return ContextInfo{}
	}
	name := os.Getenv("DOCKER_CONTEXT")
	if name == "" {
		name = configCurrentContext()
	}
	if name == "" || name == "default" {
		return ContextInfo{}
	}
	host, err := hostFromContext(name)
	if err != nil || host == "" {
		// Context is selected but unresolvable — treat as default so callers
		// don't claim a name we can't actually route to.
		return ContextInfo{}
	}
	return ContextInfo{Name: name, Host: host}
}

// NewClient returns a Docker SDK client configured from, in order:
//  1. DOCKER_HOST env var (via client.FromEnv), if set
//  2. DOCKER_CONTEXT env var, if set
//  3. ~/.docker/config.json's currentContext field
//  4. Platform default (unix socket / named pipe)
//
// API version negotiation is always enabled. Errors resolving the context are
// non-fatal: we log nothing and fall back to FromEnv so a misconfigured
// ~/.docker/ directory never breaks orbit for a user who doesn't use contexts.
func NewClient() (*client.Client, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if info := Active(); info.Host != "" {
		opts = append(opts, client.WithHost(info.Host))
	} else {
		opts = append(opts, client.FromEnv)
	}
	return client.NewClientWithOpts(opts...)
}

func configCurrentContext() string {
	path := filepath.Join(dockerConfigDir(), "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg struct {
		CurrentContext string `json:"currentContext"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return cfg.CurrentContext
}

// hostFromContext reads the docker endpoint host for the named context from
// ~/.docker/contexts/meta/<sha256(name)>/meta.json.
func hostFromContext(name string) (string, error) {
	sum := sha256.Sum256([]byte(name))
	id := hex.EncodeToString(sum[:])
	path := filepath.Join(dockerConfigDir(), "contexts", "meta", id, "meta.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var meta struct {
		Name      string `json:"Name"`
		Endpoints map[string]struct {
			Host string `json:"Host"`
		} `json:"Endpoints"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", err
	}
	ep, ok := meta.Endpoints["docker"]
	if !ok {
		return "", fmt.Errorf("context %q has no docker endpoint", name)
	}
	return ep.Host, nil
}

// dockerConfigDir mirrors Docker CLI resolution: DOCKER_CONFIG env var,
// falling back to ~/.docker.
func dockerConfigDir() string {
	if v := os.Getenv("DOCKER_CONFIG"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".docker"
	}
	return filepath.Join(home, ".docker")
}
