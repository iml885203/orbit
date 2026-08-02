package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
)

func TestPackageManagerForBinary(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		goos    string
		manager string
		command string
	}{
		{name: "Apple Silicon Homebrew", path: "/opt/homebrew/Cellar/orbit/0.8.0/bin/orbit", goos: "darwin", manager: "Homebrew", command: "brew upgrade orbit"},
		{name: "Intel Homebrew", path: "/usr/local/Cellar/orbit/0.8.0/bin/orbit", goos: "darwin", manager: "Homebrew", command: "brew upgrade orbit"},
		{name: "Linuxbrew", path: "/home/linuxbrew/.linuxbrew/Cellar/orbit/0.8.0/bin/orbit", goos: "linux", manager: "Homebrew", command: "brew upgrade orbit"},
		{name: "Scoop user install", path: `C:\Users\dev\scoop\apps\orbit\current\orbit-windows-amd64.exe`, goos: "windows", manager: "Scoop", command: "scoop update orbit"},
		{name: "Scoop custom root", path: `D:\tools\apps\orbit\0.8.0\orbit-windows-arm64.exe`, goos: "windows", manager: "Scoop", command: "scoop update orbit"},
		{name: "direct Unix install", path: "/Users/dev/.local/bin/orbit", goos: "darwin"},
		{name: "direct Windows install", path: `C:\Users\dev\AppData\Local\Programs\Orbit\orbit.exe`, goos: "windows"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := packageManagerForBinary(tt.path, tt.goos)
			if tt.manager == "" {
				if got != nil {
					t.Fatalf("managed install = %+v, want direct", got)
				}
				return
			}
			if got == nil || got.manager != tt.manager || got.command != tt.command {
				t.Fatalf("managed install = %+v, want %s / %s", got, tt.manager, tt.command)
			}
		})
	}
}

func TestManagedInstallErrorJSONAction(t *testing.T) {
	var output bytes.Buffer
	err := managedInstallError{manager: "Homebrew", command: "brew upgrade orbit"}
	if writeErr := cli.WriteJSONError(&output, "orbit update --json", err); writeErr != nil {
		t.Fatal(writeErr)
	}
	var envelope cli.JSONEnvelope
	if decodeErr := json.Unmarshal(output.Bytes(), &envelope); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if envelope.Error == nil || envelope.Error.Code != "package_managed_install" {
		t.Fatalf("error = %+v", envelope.Error)
	}
	if len(envelope.RecommendedActions) != 1 || envelope.RecommendedActions[0].Command != "brew upgrade orbit" {
		t.Fatalf("actions = %+v", envelope.RecommendedActions)
	}
}

func TestRunSelfUpdateFailsWhenInstallScriptDownloadFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows Beta updates use install.ps1")
	}
	t.Setenv("ORBIT_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	t.Setenv("ORBIT_INSTALL_URL", server.URL+"/install.sh")

	err := runSelfUpdate(context.Background())
	if err == nil {
		t.Fatal("runSelfUpdate returned success after the install script download failed")
	}
	if !strings.Contains(err.Error(), "download install script") {
		t.Fatalf("error = %q, want download context", err)
	}
}

func TestRunSelfUpdateTargetsTheInvokedBinaryInstallation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows Beta updates use install.ps1")
	}
	t.Setenv("ORBIT_HOME", t.TempDir())
	marker := filepath.Join(t.TempDir(), "install-dir")
	binDir := t.TempDir()
	fakeBash := filepath.Join(binDir, "bash")
	script := "#!/bin/sh\nprintf '%s' \"$ORBIT_INSTALL_DIR\" > \"$ORBIT_UPDATE_TEST_MARKER\"\n"
	if err := os.WriteFile(fakeBash, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ORBIT_UPDATE_TEST_MARKER", marker)
	t.Setenv("ORBIT_INSTALL_DIR", filepath.Join(t.TempDir(), "wrong-install"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("# fake installer; interpreted by the test bash stub\n"))
	}))
	t.Cleanup(server.Close)
	t.Setenv("ORBIT_INSTALL_URL", server.URL+"/install.sh")

	if err := runSelfUpdate(context.Background()); err != nil {
		t.Fatalf("runSelfUpdate: %v", err)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := currentBinaryPath()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != filepath.Dir(exe) {
		t.Fatalf("installer target = %q, want invoked binary directory %q", got, filepath.Dir(exe))
	}
}

func TestRunSelfUpdateExplainsWindowsBetaLimitation(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only contract")
	}
	err := runSelfUpdate(context.Background())
	if err == nil {
		t.Fatal("runSelfUpdate returned success on Windows Beta")
	}
	for _, want := range []string{"Windows Beta limitation", "install.ps1"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want %q", err, want)
		}
	}
}
