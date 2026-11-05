//go:build e2e

package app

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// requireDotnet skips where the .NET SDK is absent, except in CI jobs that
// set ORBIT_E2E_REQUIRE_DOTNET: there a missing SDK must fail, or the .NET
// paths would go untested without anyone noticing.
func requireDotnet(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dotnet"); err == nil {
		return
	}
	if os.Getenv("ORBIT_E2E_REQUIRE_DOTNET") != "" {
		t.Fatal("ORBIT_E2E_REQUIRE_DOTNET is set but dotnet is not on PATH")
	}
	t.Skip("dotnet not available")
}

// copyDotnetFixture copies the fixture project so its bin/ and obj/ build
// output stays out of the repository.
func copyDotnetFixture(t *testing.T, destination string) string {
	t.Helper()
	source := findRepoFile(t, filepath.Join("app", "testdata", "dotnet-health"))
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"DotnetHealth.csproj", "Program.cs"} {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destination, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(destination, "DotnetHealth.csproj")
}

// A real .NET service through both start modes: the default build-then-run
// path (build gate, assembly lookup, `dotnet <assembly>`) and `watch: true`
// (`dotnet watch run`). Doctor must accept a `path` that names the .csproj.
func TestE2E_DotnetServiceStartsInBuildAndWatchModes(t *testing.T) {
	t.Parallel()
	requireDotnet(t)

	binary := findOrbitBinary(t)
	namespace := "e2e-dotnet-" + randHex(4)
	registerE2ENamespace(t, namespace)
	builtPort, watchedPort := reserveLocalPort(t), reserveLocalPort(t)
	dashboardPort := reserveLocalPort(t)
	home, err := os.MkdirTemp("/tmp", "orb-dotnet-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	workspace := t.TempDir()
	builtProject := copyDotnetFixture(t, filepath.Join(workspace, "built"))
	watchedProject := copyDotnetFixture(t, filepath.Join(workspace, "watched"))
	configPath := filepath.Join(workspace, "orbit.yaml")
	configYAML := fmt.Sprintf(`version: "3"
settings:
  health_check:
    retries: 60
services:
  built:
    type: dotnet
    path: %q
    ports:
      http: %d
    health_check:
      type: http
      path: /health
  watched:
    type: dotnet
    path: %q
    watch: true
    ports:
      http: %d
    health_check:
      type: http
      path: /health
`, builtProject, builtPort, watchedProject, watchedPort)
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	command := func(args ...string) *exec.Cmd {
		cmd := exec.Command(binary, append([]string{"-c", configPath}, args...)...)
		cmd.Env = append(os.Environ(),
			"ORBIT_HOME="+home,
			"ORBIT_NAMESPACE="+namespace,
			fmt.Sprintf("ORBIT_DASHBOARD_PORT=%d", dashboardPort),
			"DOTNET_CLI_TELEMETRY_OPTOUT=1",
			"DOTNET_NOLOGO=1",
			// No lingering MSBuild or compiler servers holding the temp
			// workspace open after the test.
			"DOTNET_CLI_USE_MSBUILD_SERVER=0",
			"MSBUILDDISABLENODEREUSE=1",
			"UseSharedCompilation=false",
		)
		return cmd
	}
	t.Cleanup(func() {
		if t.Failed() {
			if logData, err := os.ReadFile(filepath.Join(home, "daemon.log")); err == nil {
				t.Logf("daemon.log:\n%s", logData)
			}
		}
		_ = command("down").Run()
		_ = command("daemon", "stop").Run()
	})

	// The first `dotnet` command on a fresh machine runs one-time setup that
	// creates its named-mutex directories. Two services starting at once (the
	// build of `built` and `dotnet watch` of `watched`) race on that setup on
	// Linux and one fails with mkdirat ENOENT. Run it once, alone, first.
	warmUp := exec.Command("dotnet", "nuget", "locals", "http-cache", "--list")
	warmUp.Env = append(os.Environ(), "DOTNET_CLI_TELEMETRY_OPTOUT=1", "DOTNET_NOLOGO=1")
	if output, err := warmUp.CombinedOutput(); err != nil {
		t.Fatalf("dotnet first-use setup failed: %v\n%s", err, output)
	}

	doctorOutput, err := command("doctor", "--json").Output()
	if err != nil {
		t.Fatalf("doctor rejected the .NET services: %v\n%s", err, doctorOutput)
	}
	if envelope := parseE2EEnvelope(t, string(doctorOutput)); !envelope.OK {
		t.Fatalf("doctor envelope = %+v\n%s", envelope, doctorOutput)
	}

	upOutput, err := command("up", "--json").Output()
	if err != nil {
		t.Fatalf("up failed: %v\n%s", err, upOutput)
	}
	if envelope := parseE2EEnvelope(t, string(upOutput)); !envelope.OK {
		t.Fatalf("up envelope = %+v\n%s", envelope, upOutput)
	}
	client := http.Client{Timeout: 2 * time.Second}
	for name, port := range map[string]int{"built": builtPort, "watched": watchedPort} {
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
		if err != nil {
			t.Fatalf("%s is not answering on port %d: %v", name, port, err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s health = %s", name, response.Status)
		}
	}

	if output, err := command("down", "--json").CombinedOutput(); err != nil {
		t.Fatalf("down failed: %v\n%s", err, output)
	}
	for name, port := range map[string]int{"built": builtPort, "watched": watchedPort} {
		if response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port)); err == nil {
			_ = response.Body.Close()
			t.Fatalf("%s still answers on port %d after down", name, port)
		}
	}
}
