//go:build e2e

package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2E_RelativeFileBindRecreatesFromConfigDirectory(t *testing.T) {
	env := setupE2E(t)
	configDir := filepath.Dir(env.envYaml)
	fixtureDir := filepath.Join(configDir, "fixtures")
	if err := os.MkdirAll(fixtureDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{"first.txt": "first", "second.txt": "second"} {
		if err := os.WriteFile(filepath.Join(fixtureDir, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeRelativeFileBindConfig(t, env.envYaml, "first.txt")
	env.run(t, "-c", env.envYaml, "up", "--json")
	containerName := "orbit-" + env.namespace + "-probe"
	firstID := dockerOutput(t, "inspect", "--format", "{{.Id}}", containerName)
	assertRelativeFileBind(t, containerName, filepath.Join(fixtureDir, "first.txt"), "first")

	writeRelativeFileBindConfig(t, env.envYaml, "second.txt")
	env.run(t, "-c", env.envYaml, "up", "--json")
	secondID := dockerOutput(t, "inspect", "--format", "{{.Id}}", containerName)
	if secondID == firstID {
		t.Fatal("bind source change did not recreate the container")
	}
	assertRelativeFileBind(t, containerName, filepath.Join(fixtureDir, "second.txt"), "second")
}

func writeRelativeFileBindConfig(t *testing.T, path, fixture string) {
	t.Helper()
	configYAML := fmt.Sprintf(`version: "3"
settings:
  health_check_interval: 500ms
  docker_poll_interval: 500ms
containers:
  probe:
    image: alpine:3.22
    command: ["sh", "-c", "sleep 300"]
    volumes:
      - ./fixtures/%s:/fixture/input.txt:ro
    health_check:
      type: exec
      command: ["test", "-f", "/fixture/input.txt"]
`, fixture)
	if err := os.WriteFile(path, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertRelativeFileBind(t *testing.T, containerName, wantSource, wantContents string) {
	t.Helper()
	gotMount := dockerOutput(t, "inspect", "--format", "{{range .Mounts}}{{if eq .Destination \"/fixture/input.txt\"}}{{.Type}}:{{.Source}}:{{.RW}}{{end}}{{end}}", containerName)
	if gotMount != "bind:"+wantSource+":false" {
		t.Fatalf("container bind mount = %q, want bind:%s:false", gotMount, wantSource)
	}
	if got := dockerOutput(t, "exec", containerName, "cat", "/fixture/input.txt"); got != wantContents {
		t.Fatalf("bound fixture = %q, want %q", got, wantContents)
	}
}

func TestE2E_NamedInstancesRunAndCleanIndependently(t *testing.T) {
	env := setupNamedInstanceE2E(t)
	first := "a"
	peerName := "a-ca978112-x"
	peer := assertInstancesUseIndependentEndpoints(t, env, first, peerName)
	assertOrdinaryDownCleansAnonymousAndPreservesNamedVolume(t, env, first)
	assertStaleReplacementCleansAnonymousAndPreservesConfiguredStorage(t, env, first)
	assertCleaningInstancePreservesPeer(t, env, first, peer)
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
	bindPath := filepath.Join(filepath.Dir(env.envYaml), "bind-proof")
	if err := os.MkdirAll(bindPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bindPath+"/sentinel", []byte("preserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	configYAML := fmt.Sprintf(`version: "3"
settings:
  health_check_interval: 500ms
  docker_poll_interval: 500ms
containers:
  redis:
    image: redis:7.4-alpine
    volumes:
      - data:/data
      - ./bind-proof:/proof
    ports:
      redis: "26379:6379"
    health_check:
      type: tcp
      port: 26379
  cache:
    image: redis:7.4-alpine
    environment:
      REPLACEMENT_PROOF: before
    volumes:
      - cache-data:/named
      - ./bind-proof:/proof
    ports:
      redis: "26380:6379"
    health_check:
      type: tcp
      port: 26380
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
		_, _ = env.runNoFail(t, "-c", env.envYaml, "instance", "clean", "a")
		_, _ = env.runNoFail(t, "-c", env.envYaml, "instance", "clean", "a-ca978112-x")
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
	for _, resource := range []string{"api", "cache", "redis"} {
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
	for _, resource := range []string{"api", "cache", "redis"} {
		if listData.Instances[0].Endpoints[resource] == listData.Instances[1].Endpoints[resource] {
			t.Fatalf("instances share %s endpoint: %+v", resource, listData.Instances)
		}
	}
	return listData.Instances[1]
}

func assertOrdinaryDownCleansAnonymousAndPreservesNamedVolume(t *testing.T, env *e2eEnv, name string) {
	t.Helper()
	namespace := instanceNamespaceFromList(t, env, name)
	redisContainer := "orbit-" + namespace + "-redis"
	cacheContainer := "orbit-" + namespace + "-cache"
	namedVolume := "orbit-" + namespace + "-data"
	bindPath := filepath.Join(filepath.Dir(env.envYaml), "bind-proof")
	anonymousVolume := dockerOutput(t, "inspect", "--format", "{{range .Mounts}}{{if eq .Destination \"/data\"}}{{.Name}}{{end}}{{end}}", cacheContainer)
	if anonymousVolume == "" {
		t.Fatal("cache container did not receive its image-declared anonymous volume")
	}
	assertContainerBindMount(t, redisContainer, bindPath)
	if got := dockerOutput(t, "exec", redisContainer, "redis-cli", "SET", "down-proof", "preserved"); got != "OK" {
		t.Fatalf("redis SET = %q", got)
	}
	runNamedInstance(t, env, "down", "--instance", name, "--json")
	if _, err := exec.Command("docker", "volume", "inspect", anonymousVolume).CombinedOutput(); err == nil {
		t.Fatalf("anonymous volume %s survived ordinary down", anonymousVolume)
	}
	dockerOutput(t, "volume", "inspect", namedVolume)
	upInstancePorts(t, env, name)
	assertContainerBindMount(t, redisContainer, bindPath)
	if contents, err := os.ReadFile(bindPath + "/sentinel"); err != nil || string(contents) != "preserved" {
		t.Fatalf("bind mount sentinel changed across down/up: contents=%q err=%v", contents, err)
	}
	if got := dockerOutput(t, "exec", redisContainer, "redis-cli", "GET", "down-proof"); got != "preserved" {
		t.Fatalf("named Redis data changed across down/up: %q", got)
	}
}

func assertContainerBindMount(t *testing.T, containerName, wantSource string) {
	t.Helper()
	got := dockerOutput(t, "inspect", "--format", "{{range .Mounts}}{{if eq .Destination \"/proof\"}}{{.Type}}:{{.Source}}{{end}}{{end}}", containerName)
	if got != "bind:"+wantSource {
		t.Fatalf("container bind mount = %q, want bind:%s", got, wantSource)
	}
}

func assertStaleReplacementCleansAnonymousAndPreservesConfiguredStorage(t *testing.T, env *e2eEnv, name string) {
	t.Helper()
	namespace := instanceNamespaceFromList(t, env, name)
	redisContainer := "orbit-" + namespace + "-redis"
	cacheContainer := "orbit-" + namespace + "-cache"
	bindPath := filepath.Join(filepath.Dir(env.envYaml), "bind-proof")
	oldAnonymousVolume := dockerOutput(t, "inspect", "--format", "{{range .Mounts}}{{if eq .Destination \"/data\"}}{{.Name}}{{end}}{{end}}", cacheContainer)
	cacheNamedVolume := "orbit-" + namespace + "-cache-data"
	namedVolumeID := dockerOutput(t, "volume", "inspect", "--format", "{{.Name}}", cacheNamedVolume)
	if got := dockerOutput(t, "exec", redisContainer, "redis-cli", "SET", "replacement-proof", "preserved"); got != "OK" {
		t.Fatalf("redis SET = %q", got)
	}
	if got := dockerOutput(t, "exec", cacheContainer, "sh", "-c", "printf preserved > /named/replacement-proof && printf preserved > /proof/cache-replacement-proof"); got != "" {
		t.Fatalf("cache sentinel command output = %q", got)
	}

	original, err := os.ReadFile(env.envYaml)
	if err != nil {
		t.Fatal(err)
	}
	updated := []byte(strings.Replace(string(original), "REPLACEMENT_PROOF: before", "REPLACEMENT_PROOF: after", 1))
	if string(updated) == string(original) {
		t.Fatal("named-instance fixture no longer contains the replacement marker")
	}
	if err := os.WriteFile(env.envYaml, updated, 0o600); err != nil {
		t.Fatal(err)
	}
	upInstancePorts(t, env, name)
	if _, err := exec.Command("docker", "volume", "inspect", oldAnonymousVolume).CombinedOutput(); err == nil {
		t.Fatalf("anonymous volume %s survived stale container replacement", oldAnonymousVolume)
	}
	newAnonymousVolume := dockerOutput(t, "inspect", "--format", "{{range .Mounts}}{{if eq .Destination \"/data\"}}{{.Name}}{{end}}{{end}}", cacheContainer)
	if newAnonymousVolume == "" || newAnonymousVolume == oldAnonymousVolume {
		t.Fatalf("replacement anonymous volume = %q, old=%q", newAnonymousVolume, oldAnonymousVolume)
	}
	if got := dockerOutput(t, "volume", "inspect", "--format", "{{.Name}}", cacheNamedVolume); got != namedVolumeID {
		t.Fatalf("named volume changed during replacement: before=%s after=%s", namedVolumeID, got)
	}
	if got := dockerOutput(t, "exec", cacheContainer, "cat", "/named/replacement-proof"); got != "preserved" {
		t.Fatalf("cache named-volume data changed during replacement: %q", got)
	}
	if got := dockerOutput(t, "exec", cacheContainer, "cat", "/proof/cache-replacement-proof"); got != "preserved" {
		t.Fatalf("cache bind-mount data changed during replacement: %q", got)
	}
	if got := dockerOutput(t, "exec", redisContainer, "redis-cli", "GET", "replacement-proof"); got != "preserved" {
		t.Fatalf("named Redis data changed during replacement: %q", got)
	}
	assertContainerBindMount(t, redisContainer, bindPath)
	if err := os.WriteFile(env.envYaml, original, 0o600); err != nil {
		t.Fatal(err)
	}
	upInstancePorts(t, env, name)
}

func instanceNamespaceFromList(t *testing.T, env *e2eEnv, name string) string {
	t.Helper()
	output := runNamedInstance(t, env, "instance", "list", "--json")
	envelope := parseE2EEnvelope(t, output)
	var data struct {
		Instances []namedInstanceSummary `json:"instances"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	for _, item := range data.Instances {
		if item.Name == name {
			return item.Namespace
		}
	}
	t.Fatalf("instance %q missing from list: %s", name, output)
	return ""
}

func assertCleaningInstancePreservesPeer(t *testing.T, env *e2eEnv, cleaned string, peer namedInstanceSummary) {
	t.Helper()
	cleanedNamespace := instanceNamespaceFromList(t, env, cleaned)
	cleanedNamedVolumes := []string{
		dockerOutput(t, "volume", "inspect", "--format", "{{.Name}}", "orbit-"+cleanedNamespace+"-data"),
		dockerOutput(t, "volume", "inspect", "--format", "{{.Name}}", "orbit-"+cleanedNamespace+"-cache-data"),
	}
	cleanedAnonymousVolume := dockerOutput(t, "inspect", "--format", "{{range .Mounts}}{{if eq .Destination \"/data\"}}{{.Name}}{{end}}{{end}}", "orbit-"+cleanedNamespace+"-cache")
	peerContainer := "orbit-" + peer.Namespace + "-redis"
	peerNamedVolume := "orbit-" + peer.Namespace + "-data"
	peerNamedVolumeID := dockerOutput(t, "volume", "inspect", "--format", "{{.Name}}", peerNamedVolume)
	if got := dockerOutput(t, "exec", peerContainer, "redis-cli", "SET", "instance-proof", "preserved"); got != "OK" {
		t.Fatalf("redis SET = %q", got)
	}
	runNamedInstance(t, env, "down", "--instance", peer.Name, "--json")
	runNamedInstance(t, env, "instance", "clean", cleaned, "--json")
	for _, removed := range append(cleanedNamedVolumes, cleanedAnonymousVolume) {
		if _, err := exec.Command("docker", "volume", "inspect", removed).CombinedOutput(); err == nil {
			t.Fatalf("cleaned instance volume %s survived instance clean", removed)
		}
	}
	if got := dockerOutput(t, "volume", "inspect", "--format", "{{.Name}}", peerNamedVolume); got != peerNamedVolumeID {
		t.Fatalf("peer named volume changed: before=%s after=%s", peerNamedVolumeID, got)
	}
	upInstancePorts(t, env, peer.Name)
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
	if len(statusData.Resources) != 3 {
		t.Fatalf("peer resources changed after cleanup: %+v", statusData.Resources)
	}
	for _, resource := range statusData.Resources {
		if resource.State != "healthy" {
			t.Fatalf("peer resource changed after cleanup: %+v", statusData.Resources)
		}
	}
	if got := dockerOutput(t, "exec", peerContainer, "redis-cli", "GET", "instance-proof"); got != "preserved" {
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
	if len(ports) != 3 || ports["api"] == 0 || ports["cache"] == 0 || ports["redis"] == 0 {
		t.Fatalf("%s did not report all resolved endpoints: %+v", name, ports)
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
