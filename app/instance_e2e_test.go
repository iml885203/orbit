//go:build e2e

package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestE2E_NamedInstancesRunAndCleanIndependently(t *testing.T) {
	env := setupNamedInstanceE2E(t)
	peer := assertInstancesUseIndependentEndpoints(t, env, "test-a", "test-b")
	assertCleaningInstancePreservesPeer(t, env, "test-a", peer)
}

func setupNamedInstanceE2E(t *testing.T) *e2eEnv {
	t.Helper()
	env := setupE2E(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	writeNamedInstanceAPIServer(t, env)
	writeNamedInstanceConfig(t, env)
	cleanupNamedInstances(t, env)
	return env
}

func writeNamedInstanceAPIServer(t *testing.T, env *e2eEnv) {
	t.Helper()
	serverSource := `import http.server
import os

server = http.server.ThreadingHTTPServer(("127.0.0.1", int(os.environ["PORT"])), http.server.SimpleHTTPRequestHandler)
server.serve_forever()
`
	if err := os.WriteFile(env.home+"/server.py", []byte(serverSource), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeNamedInstanceConfig(t *testing.T, env *e2eEnv) {
	t.Helper()
	configYAML := fmt.Sprintf(`version: "3"
settings:
  health_check_interval: 500ms
  docker_poll_interval: 500ms
containers:
  redis:
    image: redis:7.4-alpine
    ports:
      redis: "26379:6379"
    health_check:
      type: tcp
      port: 26379
services:
  api:
    type: python
    path: %q
    command: python3 server.py
    ports:
      http: 28080
    depends_on: [redis]
    health_check:
      type: http
      path: /
      port: 28080
`, env.home)
	if err := os.WriteFile(env.envYaml, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}
}

func cleanupNamedInstances(t *testing.T, env *e2eEnv) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = env.runNoFail(t, "-c", env.envYaml, "instance", "clean", "test-a")
		_, _ = env.runNoFail(t, "-c", env.envYaml, "instance", "clean", "test-b")
	})
}

type namedInstanceSummary struct {
	Name      string            `json:"name"`
	State     string            `json:"state"`
	Dashboard string            `json:"dashboard"`
	Namespace string            `json:"namespace"`
	Endpoints map[string]string `json:"endpoints"`
}

func assertInstancesUseIndependentEndpoints(t *testing.T, env *e2eEnv, first, second string) namedInstanceSummary {
	t.Helper()
	portsA := upInstancePorts(t, env, first)
	portsB := upInstancePorts(t, env, second)
	for _, resource := range []string{"api", "redis"} {
		if portsA[resource] == portsB[resource] {
			t.Fatalf("instances share %s port %d", resource, portsA[resource])
		}
	}

	listOutput := runNamedInstance(t, env, "instance", "list", "--json")
	listEnvelope := parseE2EEnvelope(t, string(listOutput))
	var listData struct {
		Instances []namedInstanceSummary `json:"instances"`
	}
	if err := json.Unmarshal(listEnvelope.Data, &listData); err != nil {
		t.Fatal(err)
	}
	if len(listData.Instances) != 2 || listData.Instances[0].State != "running" || listData.Instances[1].State != "running" {
		t.Fatalf("instance list = %+v\n%s", listData.Instances, listOutput)
	}
	if listData.Instances[0].Dashboard == listData.Instances[1].Dashboard {
		t.Fatalf("instances share dashboard %q", listData.Instances[0].Dashboard)
	}
	for _, resource := range []string{"api", "redis"} {
		if listData.Instances[0].Endpoints[resource] == listData.Instances[1].Endpoints[resource] {
			t.Fatalf("instances share %s endpoint: %+v", resource, listData.Instances)
		}
	}
	return listData.Instances[1]
}

func assertCleaningInstancePreservesPeer(t *testing.T, env *e2eEnv, cleaned string, peer namedInstanceSummary) {
	t.Helper()
	containerName := "orbit-" + peer.Namespace + "-redis"
	containerID := dockerOutput(t, "inspect", "--format", "{{.Id}}", containerName)
	if got := dockerOutput(t, "exec", containerName, "redis-cli", "SET", "instance-proof", "preserved"); got != "OK" {
		t.Fatalf("redis SET = %q", got)
	}
	runNamedInstance(t, env, "instance", "clean", cleaned, "--json")
	statusB := runNamedInstance(t, env, "status", "--instance", peer.Name, "--json")
	statusEnvelope := parseE2EEnvelope(t, string(statusB))
	if !statusEnvelope.OK {
		t.Fatalf("test-b status failed: %s", statusB)
	}
	var statusData struct {
		Resources []struct {
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(statusEnvelope.Data, &statusData); err != nil {
		t.Fatal(err)
	}
	if len(statusData.Resources) != 2 {
		t.Fatalf("peer resources changed after cleanup: %+v", statusData.Resources)
	}
	for _, resource := range statusData.Resources {
		if resource.State != "healthy" {
			t.Fatalf("peer resource changed after cleanup: %+v", statusData.Resources)
		}
	}
	if got := dockerOutput(t, "inspect", "--format", "{{.Id}}", containerName); got != containerID {
		t.Fatalf("peer container changed: before=%s after=%s", containerID, got)
	}
	if got := dockerOutput(t, "exec", containerName, "redis-cli", "GET", "instance-proof"); got != "preserved" {
		t.Fatalf("peer Redis data changed: %q", got)
	}
}

func upInstancePorts(t *testing.T, env *e2eEnv, name string) map[string]int {
	t.Helper()
	output := runNamedInstance(t, env, "up", "--instance", name, "--json")
	envelope := parseE2EEnvelope(t, string(output))
	if !envelope.OK {
		t.Fatalf("up %s failed: %s", name, output)
	}
	if envelope.Instance != name {
		t.Fatalf("up identified instance %q, want %q", envelope.Instance, name)
	}
	if len(envelope.RecommendedActions) == 0 || !strings.Contains(envelope.RecommendedActions[0].Command, "--instance "+name) {
		t.Fatalf("up recovery lost instance target: %+v", envelope.RecommendedActions)
	}
	var data struct {
		Resources []struct {
			Name  string         `json:"name"`
			State string         `json:"state"`
			Ports map[string]int `json:"ports"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	ports := make(map[string]int)
	for _, resource := range data.Resources {
		if resource.State != "healthy" {
			t.Fatalf("%s resources = %+v", name, data.Resources)
		}
		for _, port := range resource.Ports {
			ports[resource.Name] = port
		}
	}
	if len(ports) != 2 || ports["api"] == 0 || ports["redis"] == 0 {
		t.Fatalf("%s did not report both resolved endpoints: %+v", name, ports)
	}
	return ports
}

func runNamedInstance(t *testing.T, env *e2eEnv, args ...string) string {
	t.Helper()
	return env.run(t, append([]string{"-c", env.envYaml}, args...)...)
}

func dockerOutput(t *testing.T, args ...string) string {
	t.Helper()
	output, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
