//go:build e2e && !windows

package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/autoupdate"
)

func TestE2E_MultipleResourceLogs(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 unavailable")
	}
	binary := findOrbitBinary(t)
	parentUpdateHome := t.TempDir()
	t.Setenv("ORBIT_UPDATE_HOME", parentUpdateHome)
	t.Setenv(autoupdate.EnvLaunchPath, filepath.Join(t.TempDir(), "unrelated-orbit"))
	t.Setenv(backgroundUpdateEnv, "0")
	updateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("logs fixture invoked the update endpoint")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer updateServer.Close()
	t.Setenv("ORBIT_RELEASE_API_URL", updateServer.URL)
	home, err := os.MkdirTemp(os.TempDir(), "orb-logs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	namespace := "e2e-logs-" + filepath.Base(home)
	registerE2ENamespace(t, namespace)
	t.Logf("ORBIT_NAMESPACE=%s", namespace)
	dashboardPort := reserveLocalPort(t)
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "orbit.yaml")
	configYAML := fmt.Sprintf(`version: "3"
services:
  api:
    type: python
    path: %q
    command: %q
  web:
    type: python
    path: %q
    command: %q
`, workspace, `python3 -u -c "import time; print('api-ready'); time.sleep(120)"`, workspace, `python3 -u -c "import time; print('web-ready'); time.sleep(120)"`)
	if err := os.WriteFile(configPath, []byte(configYAML), 0600); err != nil {
		t.Fatal(err)
	}
	command := func(ctx context.Context, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, binary, append([]string{"-c", configPath}, args...)...)
		cmd.Env = append(os.Environ(), "ORBIT_HOME="+home, "ORBIT_INSTANCE_BASE_HOME="+home,
			"ORBIT_UPDATE_HOME="+filepath.Join(home, "updates"), "ORBIT_UPDATE_BACKGROUND=1",
			autoupdate.EnvLaunchPath+"="+binary, "ORBIT_NAMESPACE="+namespace, fmt.Sprintf("ORBIT_DASHBOARD_PORT=%d", dashboardPort))
		return cmd
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = command(ctx, "down", "--json").Run()
		_ = command(ctx, "daemon", "stop", "--json").Run()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := command(ctx, "up", "--json").CombinedOutput(); err != nil {
		t.Fatalf("up: %v\n%s", err, out)
	}
	var snapshot multiLogsJSONData
	for {
		out, err := command(ctx, "logs", "api", "web", "api", "--json").Output()
		if err != nil {
			t.Fatalf("logs: %v\n%s", err, out)
		}
		envelope := parseE2EEnvelope(t, string(out))
		if !envelope.OK {
			t.Fatalf("envelope=%+v", envelope)
		}
		if err := json.Unmarshal(envelope.Data, &snapshot); err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Resources) == 2 && len(snapshot.Resources[0].Lines) > 0 && len(snapshot.Resources[1].Lines) > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("resources produced no logs")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if snapshot.Resources[0].Resource != "api" || snapshot.Resources[1].Resource != "web" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	follow := command(ctx, "logs", "api", "web", "--follow", "--lines", "1", "--json")
	stdout, err := follow.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	follow.Stderr = &diagnostics
	releaseConsole, err := configureSignalTestProcess(follow)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseConsole()
	if err := follow.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if follow.ProcessState == nil {
			_ = follow.Process.Kill()
			_ = follow.Wait()
		}
	})
	scanner := bufio.NewScanner(stdout)
	resources := map[string]bool{}
	for len(resources) < 2 && scanner.Scan() {
		var event logJSONEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		resources[event.Resource] = true
	}
	if len(resources) != 2 {
		t.Fatalf("follow resources=%v stderr=%s", resources, diagnostics.String())
	}
	if err := signalTestProcess(follow); err != nil {
		t.Fatal(err)
	}
	err = follow.Wait()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.Sys().(syscall.WaitStatus).Signal() != syscall.SIGINT {
		t.Fatalf("Ctrl-C: %v\n%s", err, diagnostics.String())
	}
	out, err := command(ctx, "logs", "api", "missing", "--json").Output()
	if err == nil {
		t.Fatalf("unknown succeeded: %s", out)
	}
	if envelope := parseE2EEnvelope(t, string(out)); envelope.Error == nil || envelope.Error.Code != "unknown_resource" {
		t.Fatalf("unknown: %s", out)
	}
	entries, err := os.ReadDir(parentUpdateHome)
	if err != nil || len(entries) != 0 {
		t.Fatalf("logs fixture modified inherited update home: entries=%d, err=%v", len(entries), err)
	}
	updateData, err := os.ReadFile(filepath.Join(home, "updates", "update-registry.json"))
	if err != nil {
		t.Fatalf("read fixture update registry: %v", err)
	}
	var registry map[string]map[string]autoupdate.State
	if err := json.Unmarshal(updateData, &registry); err != nil {
		t.Fatal(err)
	}
	installationID, launchPath, err := autoupdate.InstallationID(binary)
	if err != nil {
		t.Fatal(err)
	}
	installations := registry["installations"]
	if len(installations) != 1 || installations[installationID].LaunchPath != launchPath {
		t.Fatal("logs fixture registered an unrelated installation")
	}
	if state := installations[installationID]; state.NextCheckAt != nil || state.LastCheckedAt != nil {
		t.Fatal("logs fixture scheduled an update check")
	}

}
