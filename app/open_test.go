package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
)

func TestContainerURLNotConfiguredHintUsesValidSchema(t *testing.T) {
	err := resourceURLNotConfiguredError{name: "store-front", kind: daemon.ResourceKindContainer}
	hint := err.CLIJSONHint()

	if !strings.Contains(hint, "set 'url'") || !strings.Contains(hint, "'http' or 'https'") {
		t.Fatalf("hint = %q, want named application port guidance", hint)
	}
}

func TestRunOpenReportsContainerTarget(t *testing.T) {
	home, err := os.MkdirTemp(shortTestTempRoot(), "o131-open-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("ORBIT_HOME", home)
	if err := os.MkdirAll(daemon.OrbitDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	serveLifecycleProgressDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		case "/api/status":
			_ = json.NewEncoder(w).Encode(daemon.StatusResponse{Resources: []daemon.ResourceStatus{{
				Name: "store-front", Kind: daemon.ResourceKindContainer, State: "healthy",
				URL: "http://localhost:8080/admin",
			}}})
		default:
			http.NotFound(w, r)
		}
	})
	path := filepath.Join(home, "orbit.yaml")
	if err := os.WriteFile(path, []byte("version: \"3\"\ncontainers:\n  store-front:\n    image: nginx:alpine\n    url: http://localhost:8080/admin\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	previousJSON, previousConfig, previousOpenBrowser := cli.JSONOutput, configFile, openBrowser
	cli.JSONOutput = true
	configFile = path
	openedURL := ""
	openBrowser = func(url string) error {
		openedURL = url
		return nil
	}
	t.Cleanup(func() {
		cli.JSONOutput = previousJSON
		configFile = previousConfig
		openBrowser = previousOpenBrowser
	})
	finishCapture := captureLifecycleProcessStreams(t)

	if err := runOpen(nil, []string{"store-front"}); err != nil {
		t.Fatal(err)
	}
	output, diagnostics := finishCapture()
	if diagnostics != "" {
		t.Fatalf("stderr = %q", diagnostics)
	}
	var envelope cli.JSONEnvelope
	if err := json.Unmarshal(output, &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, output)
	}
	var data openJSONData
	encoded, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &data); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"service"`) {
		t.Fatalf("container open JSON = %s, must omit legacy service field", encoded)
	}
	if data.Target != "container" || data.Resource != "store-front" || data.Service != "" || data.URL != openedURL {
		t.Fatalf("open data = %+v, opened URL = %q", data, openedURL)
	}
}

func TestOpenJSONKeepsServiceCompatibilityAlias(t *testing.T) {
	previousJSON, previousOpenBrowser := cli.JSONOutput, openBrowser
	cli.JSONOutput = true
	openBrowser = func(string) error { return nil }
	t.Cleanup(func() {
		cli.JSONOutput = previousJSON
		openBrowser = previousOpenBrowser
	})
	finishCapture := captureLifecycleProcessStreams(t)

	if err := openURL("http://localhost:8080", "service", "api"); err != nil {
		t.Fatal(err)
	}
	output, diagnostics := finishCapture()
	if diagnostics != "" {
		t.Fatalf("stderr = %q", diagnostics)
	}
	var envelope cli.JSONEnvelope
	if err := json.Unmarshal(output, &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, output)
	}
	encoded, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	var data openJSONData
	if err := json.Unmarshal(encoded, &data); err != nil {
		t.Fatal(err)
	}
	if data.Target != "service" || data.Resource != "api" || data.Service != "api" {
		t.Fatalf("service open data = %+v, want resource and compatibility alias", data)
	}
}
