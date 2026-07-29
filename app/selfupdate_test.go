package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunSelfUpdateFailsWhenInstallScriptDownloadFails(t *testing.T) {
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
