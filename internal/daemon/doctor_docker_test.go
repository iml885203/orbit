package daemon

import (
	"strings"
	"testing"

	"github.com/iml885203/orbit/dockerctx"
)

func TestFormatDockerPassMessage(t *testing.T) {
	tests := []struct {
		name    string
		ctxInfo dockerctx.ContextInfo
		path    string
		api     string
		want    string
	}{
		{
			name:    "no named context (default or DOCKER_HOST) keeps path + api",
			ctxInfo: dockerctx.ContextInfo{},
			path:    "/usr/local/bin/docker",
			api:     "1.45",
			want:    "found at /usr/local/bin/docker (API v1.45)",
		},
		{
			name:    "named context leads with context name",
			ctxInfo: dockerctx.ContextInfo{Name: "wsl-docker"},
			path:    "/usr/local/bin/docker",
			api:     "1.45",
			want:    "wsl-docker context (API v1.45)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDockerPassMessage(tt.ctxInfo, tt.path, tt.api)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatDockerFailMessage(t *testing.T) {
	t.Run("default context keeps generic message and hint", func(t *testing.T) {
		msg, hint := formatDockerFailMessage(dockerctx.ContextInfo{}, "Docker daemon is not running")
		if msg != "Docker daemon is not running" {
			t.Errorf("msg: got %q", msg)
		}
		if hint != "Start Docker Desktop" {
			t.Errorf("hint: got %q", hint)
		}
	})

	t.Run("named context names the context and suggests fallback", func(t *testing.T) {
		msg, hint := formatDockerFailMessage(dockerctx.ContextInfo{Name: "wsl-docker"}, "Docker daemon is not running")
		if !strings.Contains(msg, "wsl-docker") {
			t.Errorf("msg should name the context, got %q", msg)
		}
		if !strings.Contains(hint, "docker context use default") {
			t.Errorf("hint should point at the context-switch fix, got %q", hint)
		}
	})
}
