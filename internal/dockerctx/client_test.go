package dockerctx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestActive(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".docker")
	t.Setenv("DOCKER_CONFIG", cfgDir)
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")

	writeConfig := func(t *testing.T, currentContext string) {
		t.Helper()
		if err := os.MkdirAll(cfgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(map[string]string{"currentContext": currentContext})
		if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeContext := func(t *testing.T, name, host string) {
		t.Helper()
		sum := sha256.Sum256([]byte(name))
		id := hex.EncodeToString(sum[:])
		dir := filepath.Join(cfgDir, "contexts", "meta", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		meta := map[string]any{
			"Name": name,
			"Endpoints": map[string]any{
				"docker": map[string]any{"Host": host},
			},
		}
		data, _ := json.Marshal(meta)
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("no config -> empty", func(t *testing.T) {
		_ = os.RemoveAll(cfgDir)
		got := Active()
		if got.Name != "" || got.Host != "" {
			t.Fatalf("got %+v, want empty", got)
		}
	})

	t.Run("DOCKER_HOST -> empty (caller uses FromEnv)", func(t *testing.T) {
		t.Setenv("DOCKER_HOST", "tcp://override:2375")
		got := Active()
		if got.Name != "" || got.Host != "" {
			t.Fatalf("got %+v, want empty so FromEnv picks up DOCKER_HOST", got)
		}
	})

	t.Run("DOCKER_CONTEXT resolves to host", func(t *testing.T) {
		t.Setenv("DOCKER_HOST", "")
		t.Setenv("DOCKER_CONTEXT", "wsl-docker")
		writeContext(t, "wsl-docker", "npipe:////./pipe/docker_wsl")
		got := Active()
		if got.Name != "wsl-docker" || got.Host != "npipe:////./pipe/docker_wsl" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("DOCKER_CONTEXT wins over config.json", func(t *testing.T) {
		t.Setenv("DOCKER_HOST", "")
		writeConfig(t, "wsl-docker")
		writeContext(t, "other", "tcp://1.2.3.4:2375")
		t.Setenv("DOCKER_CONTEXT", "other")
		got := Active()
		if got.Name != "other" || got.Host != "tcp://1.2.3.4:2375" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("config currentContext resolves to host", func(t *testing.T) {
		t.Setenv("DOCKER_HOST", "")
		t.Setenv("DOCKER_CONTEXT", "")
		writeConfig(t, "wsl-docker")
		writeContext(t, "wsl-docker", "npipe:////./pipe/docker_wsl")
		got := Active()
		if got.Name != "wsl-docker" || got.Host != "npipe:////./pipe/docker_wsl" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("currentContext=default -> empty", func(t *testing.T) {
		t.Setenv("DOCKER_HOST", "")
		t.Setenv("DOCKER_CONTEXT", "")
		writeConfig(t, "default")
		got := Active()
		if got.Name != "" {
			t.Fatalf("got %+v, want empty", got)
		}
	})

	t.Run("context selected but meta missing -> empty", func(t *testing.T) {
		t.Setenv("DOCKER_HOST", "")
		t.Setenv("DOCKER_CONTEXT", "ghost")
		got := Active()
		if got.Name != "" || got.Host != "" {
			t.Fatalf("got %+v, want empty when context is unresolvable", got)
		}
	})
}
