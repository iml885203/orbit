package bigquery

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/instance"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const validYAML = `version: "3"
bigquery:
  identity: orders-dev
  profiles:
    shared:
      mode: shared
      job_project: orbit-tests
      data_project: orbit-data
      location: asia-east1
      dataset: readonly_orders
    integration:
      mode: sandbox
      job_project: orbit-tests
      data_project: orbit-data
      location: asia-east1
      tables:
        orders:
          schema: orders.json
          fixture: orders.ndjson
      app_mapping:
        dataset: ORDERS_DATASET
`

func writeConfig(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "orbit.yaml")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestOptionalConfigStrictValidation(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, "version: \"3\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if From(cfg) != nil {
		t.Fatal("unconfigured feature enabled")
	}
	cfg, err = config.Load(writeConfig(t, validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if From(cfg).Profiles["integration"].ID != "integration" {
		t.Fatal("profile identity missing")
	}
	for _, source := range []string{strings.Replace(validYAML, "data_project:", "data_projet:", 1), strings.Replace(validYAML, "mode: shared", "mode: unknown", 1), strings.Replace(validYAML, "dataset: readonly_orders", "dataset: invalid.dataset", 1), strings.Replace(validYAML, "identity: orders-dev", "identity: ''", 1)} {
		if _, err := config.Load(writeConfig(t, source)); err == nil {
			t.Fatalf("accepted malformed config %s", source)
		}
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte("profiles:\n  integration:\n    mode: sandbox\n"), &node); err != nil {
		t.Fatal(err)
	}
}
func TestLocalCLIJSONDoesNotNeedCredentials(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/must-not-read")
	t.Setenv("ORBIT_HOME", t.TempDir())
	t.Setenv(instance.EnvBaseHome, t.TempDir())
	path := writeConfig(t, validYAML)
	old := cli.JSONOutput
	cli.JSONOutput = true
	t.Cleanup(func() { cli.JSONOutput = old })
	root := &cobra.Command{Use: "orbit"}
	root.PersistentFlags().String("config", path, "")
	root.AddCommand(Command())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"bq", "sandbox", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var envelope cli.JSONEnvelope
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || !envelope.OK || envelope.SchemaVersion != "orbit.cli.v1" {
		t.Fatalf("bad JSON %s %v", out.String(), err)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("ORBIT_HOME"), "bq-generation.json")); !os.IsNotExist(err) {
		t.Fatal("local inspection created runtime state")
	}
	out.Reset()
	root.SetArgs([]string{"bq", "sandbox", "prepare", "shared", "--run", "r"})
	if err := root.Execute(); err == nil {
		t.Fatal("shared mutated")
	}
	if !strings.Contains(out.String(), "sandbox_profile_and_run_required") {
		t.Fatal(out.String())
	}
}
func TestMalformedCanonicalEvidenceCannotMutate(t *testing.T) {
	for _, mutate := range []func(*Receipt){func(r *Receipt) { r.FixtureHash = "" }, func(r *Receipt) { r.Jobs = nil }, func(r *Receipt) { delete(r.JobStates, "orders") }, func(r *Receipt) { r.Target.DataProject = "another-project" }, func(r *Receipt) { r.Generation = "bad" }, func(r *Receipt) { delete(r.TableGenerations, "orders") }, func(r *Receipt) { r.TableGenerations["orders"] = 0 }, func(r *Receipt) { r.TableGenerations["unknown"] = 3 }, func(r *Receipt) { r.Phase = "other" }} {
		fake := testFake(t)
		r := testRunner(t, fake, t.TempDir())
		rec := ready(t, r)
		mutate(rec)
		if err := r.store.save(rec); err != nil {
			t.Fatal(err)
		}
		before := fake.requests
		_, err := r.clean(context.Background(), testProfile(), "run-1", true)
		requireCode(t, err, "receipt_invalid")
		if before != fake.requests {
			t.Fatal("corrupt evidence contacted cloud")
		}
	}
}
func TestSchemaValidationBeforeCloud(t *testing.T) {
	for _, raw := range []string{`{"fields":[null]}`, `{"fields":[{"name":"amount"}]}`, `{"fields":[{"name":"amount","type":"INVALID"}]}`, `{"fields":[{"name":"amount","type":"STRING","policyTags":{"names":["unsupported"]}}]}`} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "orders.json"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "orders.ndjson"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readInputs(root, testProfile()); err == nil {
			t.Fatalf("accepted schema %s", raw)
		}
	}
}
func TestEmptyFixtureProducesSchemaOnlySandbox(t *testing.T) {
	fake := testFake(t)
	r := testRunner(t, fake, t.TempDir())
	input := testInputs()
	input.Tables[0].Fixture = nil
	rec, err := r.prepare(context.Background(), testProfile(), "empty", input)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.DataReady || rec.JobStates["orders"] != "schema_only" || fake.loads != 0 {
		t.Fatal("incorrect empty sandbox")
	}
	if _, err = r.clean(context.Background(), testProfile(), "empty", true); err != nil {
		t.Fatal(err)
	}
}
func TestRecordsSurviveMissingManifestInstanceCleanup(t *testing.T) {
	base := t.TempDir()
	t.Setenv(instance.EnvBaseHome, base)
	t.Setenv(instance.EnvName, "test")
	runtime, err := instance.Resolve(base, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORBIT_HOME", runtime.Home)
	first, err := runtimeGeneration(true)
	if err != nil {
		t.Fatal(err)
	}
	dir := storeDirectory("orders-dev")
	r := testRunner(t, testFake(t), dir)
	r.generation = first
	rec := ready(t, r)
	if _, err := instance.RemoveResidue(base, "test"); err != nil {
		t.Fatal(err)
	}
	records, err := readRecords(dir)
	if err != nil || len(records) != 1 || records[0].Generation != rec.Generation {
		t.Fatalf("lost recovery evidence %v %v", records, err)
	}
	next, err := runtimeGeneration(true)
	if err != nil || next == first {
		t.Fatal("recreated home inherited authority")
	}
}
func TestMultiprocessOperationLock(t *testing.T) {
	if dir := os.Getenv("ORBIT_BQ_LOCK_TEST_DIR"); dir != "" {
		store, err := openStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.acquire(scopeFor(testProfile(), "run-1"))
		requireCode(t, err, "operation_busy")
		return
	}
	dir := t.TempDir()
	store, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := store.acquire(scopeFor(testProfile(), "run-1"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMultiprocessOperationLock$")
	cmd.Env = append(os.Environ(), "ORBIT_BQ_LOCK_TEST_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child lock check %s: %v", out, err)
	}
}
func TestUnconfiguredDaemonRouteIsLocalAndReadOnly(t *testing.T) {
	mux := http.NewServeMux()
	SetupDaemon(&testHost{cfg: &config.Config{}}, mux)
	request := httptest.NewRequest(http.MethodGet, "/api/features/bigquery", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	var snapshot BQSnapshot
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil || snapshot.Configured || len(snapshot.Records) != 0 {
		t.Fatalf("bad snapshot %s", response.Body.String())
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/features/bigquery", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatal("mutation route exists")
	}
}

func TestRecoveryEnvelopePreservesExplicitTarget(t *testing.T) {
	old := cli.JSONOutput
	cli.JSONOutput = true
	t.Cleanup(func() { cli.JSONOutput = old })
	t.Setenv("ORBIT_INSTANCE", "bq-review")
	root := &cobra.Command{Use: "orbit"}
	root.PersistentFlags().String("config", filepath.Join(t.TempDir(), "space in path", "orbit.yaml"), "")
	command := Command()
	root.AddCommand(command)
	prepare, _, err := root.Find([]string{"bq", "sandbox", "prepare"})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepare.ParseFlags([]string{"integration", "--run", "pr 'quoted'"}); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	root.SetOut(&buffer)
	if err := output(prepare, nil, failure("job_pending", context.DeadlineExceeded)); err == nil {
		t.Fatal("failure lost")
	}
	var envelope cli.JSONEnvelope
	if err := json.Unmarshal(buffer.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error == nil || !envelope.Error.Retryable || len(envelope.RecommendedActions) != 1 {
		t.Fatalf("incomplete recovery %s", buffer.String())
	}
	action := envelope.RecommendedActions[0]
	for _, part := range []string{"--instance bq-review", "--config", "space in path", "sandbox inspect integration", "--run", "quoted", "--json"} {
		if !strings.Contains(action.Command, part) {
			t.Fatalf("lost target %q in %q", part, action.Command)
		}
	}
	if action.Destructive {
		t.Fatal("recovery unexpectedly mutates")
	}
}

func TestInputNamedPipeRejectedBeforeOpen(t *testing.T) {
	binary, err := exec.LookPath("mkfifo")
	if err != nil {
		t.Skip("mkfifo unavailable on this platform")
	}
	root := t.TempDir()
	path := filepath.Join(root, "fixture.pipe")
	if out, err := exec.Command(binary, path).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo %s: %v", out, err)
	}
	_, err = readWorkspaceFile(root, "fixture.pipe")
	requireCode(t, err, "input_not_regular")
}
