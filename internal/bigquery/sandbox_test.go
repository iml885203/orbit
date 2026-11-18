package bigquery

import (
	"context"
	"fmt"
	bq "google.golang.org/api/bigquery/v2"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testProfile() Profile {
	return Profile{ID: "integration", Mode: "sandbox", JobProject: "orbit-tests", DataProject: "orbit-data", Location: "asia-east1", Tables: map[string]Table{"orders": {Schema: "orders.json", Fixture: "orders.ndjson"}}, Mapping: map[string]string{"dataset": "ORDERS_DATASET", "job_project": "GOOGLE_CLOUD_PROJECT", "location": "ORDERS_LOCATION"}}
}
func testInputs() inputs {
	return inputs{Hash: digest([]byte("fixture-v1")), Tables: []tableInput{{ID: "orders", Schema: &bq.TableSchema{Fields: []*bq.TableFieldSchema{{Name: "customer", Type: "STRING"}, {Name: "amount", Type: "INTEGER"}}}, Fixture: []byte("{\"customer\":\"A\",\"amount\":100}\n")}}}
}
func testRunner(t *testing.T, fake *fakeBigQuery, dir string) *sandboxRunner {
	t.Helper()
	store, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	api, err := offlineClient(fake.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &sandboxRunner{api: api, store: store, generation: digest([]byte("test-generation"))[:32], instance: "test"}
}
func testFake(t *testing.T) *fakeBigQuery {
	t.Helper()
	fake := newFake()
	t.Cleanup(fake.server.Close)
	return fake
}
func requireCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || errorCode(err) != want {
		t.Fatalf("error=%v; want %s", err, want)
	}
}
func ready(t *testing.T, r *sandboxRunner) *Receipt {
	t.Helper()
	rec, err := r.prepare(context.Background(), testProfile(), "run-1", testInputs())
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestLifecycleOfficialSDKOffline(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/credentials-must-not-be-read")
	fake := testFake(t)
	r := testRunner(t, fake, t.TempDir())
	rec := ready(t, r)
	if !rec.DataReady || rec.AppRuntime != "unverified" || rec.AppConfig["ORDERS_DATASET"] != "orbit-data."+rec.DatasetID || rec.CreationTime == 0 {
		t.Fatalf("bad receipt %+v", rec)
	}
	ready(t, r)
	if _, err := r.clean(context.Background(), testProfile(), "run-1", true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.clean(context.Background(), testProfile(), "run-1", true); err != nil {
		t.Fatal(err)
	}
	loads, deletes := fake.counts()
	if loads != 1 || deletes != 1 {
		t.Fatalf("side effects: %v", fake)
	}
	_, err := r.prepare(context.Background(), testProfile(), "run-1", testInputs())
	requireCode(t, err, "new_run_key_required")
}

func TestAcceptedLoadLostReplyResumesSameJob(t *testing.T) {
	fake := testFake(t)
	fake.loseLoadReply = true
	dir := t.TempDir()
	r := testRunner(t, fake, dir)
	rec, err := r.prepare(context.Background(), testProfile(), "run-1", testInputs())
	requireCode(t, err, "load_submit_unknown")
	saved, err := r.store.read(rec.Scope)
	if err != nil || saved.JobStates["orders"] != "submission_intent" {
		t.Fatalf("lost intent: %+v %v", saved, err)
	}
	ready(t, testRunner(t, fake, dir))
	loads, _ := fake.counts()
	if loads != 1 {
		t.Fatalf("duplicate loads: %d", loads)
	}
}
func TestAcceptedCreateLostReplyResumes(t *testing.T) {
	fake := testFake(t)
	fake.loseCreateReply = true
	r := testRunner(t, fake, t.TempDir())
	_, err := r.prepare(context.Background(), testProfile(), "run-1", testInputs())
	requireCode(t, err, "dataset_create_unknown")
	ready(t, r)
	if len(fake.datasets) != 1 {
		t.Fatal("duplicate datasets")
	}
}
func TestImmutableRunKeyConflicts(t *testing.T) {
	fake := testFake(t)
	r := testRunner(t, fake, t.TempDir())
	ready(t, r)
	before := fake.requests
	input := testInputs()
	input.Hash = digest([]byte("changed"))
	_, err := r.prepare(context.Background(), testProfile(), "run-1", input)
	requireCode(t, err, "sandbox_config_conflict")
	p := testProfile()
	p.Location = "US"
	_, err = r.prepare(context.Background(), p, "run-1", testInputs())
	requireCode(t, err, "sandbox_config_conflict")
	if fake.requests != before {
		t.Fatal("conflict contacted cloud")
	}
}
func TestIndependentStoresDoNotClaimGlobalIdempotency(t *testing.T) {
	fake := testFake(t)
	a := ready(t, testRunner(t, fake, t.TempDir()))
	b := ready(t, testRunner(t, fake, t.TempDir()))
	if a.DatasetID == b.DatasetID {
		t.Fatal("dataset reused")
	}
}
func TestForeignRecordCannotAuthorizeMutation(t *testing.T) {
	fake := testFake(t)
	a := testRunner(t, fake, t.TempDir())
	rec := ready(t, a)
	b := testRunner(t, fake, t.TempDir())
	if err := b.store.save(rec); err != nil {
		t.Fatal(err)
	}
	before := fake.requests
	_, err := b.clean(context.Background(), testProfile(), "run-1", true)
	requireCode(t, err, "foreign_generation_mutation_blocked")
	if fake.requests != before {
		t.Fatal("foreign mutation contacted cloud")
	}
}
func TestRecreatedInstanceCannotInheritAuthority(t *testing.T) {
	fake := testFake(t)
	r := testRunner(t, fake, t.TempDir())
	ready(t, r)
	r.generation = "another-incarnation"
	_, err := r.clean(context.Background(), testProfile(), "run-1", true)
	requireCode(t, err, "foreign_generation_mutation_blocked")
}
func TestCleanupConfirmationAndOwnership(t *testing.T) {
	fake := testFake(t)
	r := testRunner(t, fake, t.TempDir())
	rec := ready(t, r)
	_, err := r.clean(context.Background(), testProfile(), "run-1", false)
	requireCode(t, err, "cleanup_requires_confirmation_and_quiescence")
	fake.datasets[rec.Target.DataProject+"."+rec.DatasetID].Labels["orbit_generation"] = "foreign"
	_, err = r.clean(context.Background(), testProfile(), "run-1", true)
	requireCode(t, err, "ownership_mismatch")
	if fake.deletes != 0 {
		t.Fatal("deleted foreign dataset")
	}
}
func TestUnknownGenerationBlocksCleanup(t *testing.T) {
	fake := testFake(t)
	r := testRunner(t, fake, t.TempDir())
	rec := ready(t, r)
	rec.CreationTime = 0
	if err := r.store.save(rec); err != nil {
		t.Fatal(err)
	}
	_, err := r.clean(context.Background(), testProfile(), "run-1", true)
	requireCode(t, err, "cleanup_generation_unknown")
	if fake.deletes != 0 {
		t.Fatal("deleted unobserved generation")
	}
}
func TestReplacedGenerationBlocksCleanup(t *testing.T) {
	fake := testFake(t)
	r := testRunner(t, fake, t.TempDir())
	rec := ready(t, r)
	fake.datasets[rec.Target.DataProject+"."+rec.DatasetID].CreationTime++
	_, err := r.clean(context.Background(), testProfile(), "run-1", true)
	requireCode(t, err, "dataset_generation_changed")
	if fake.deletes != 0 {
		t.Fatal("deleted replacement")
	}
}
func TestCleanupUnknownJobBlocksButNeverSubmittedJobDoesNot(t *testing.T) {
	for _, state := range []string{"planned", "submission_intent"} {
		t.Run("state_"+state, func(t *testing.T) {
			fake := testFake(t)
			r := testRunner(t, fake, t.TempDir())
			rec := ready(t, r)
			delete(fake.jobs, rec.Target.JobProject+"."+rec.Jobs["orders"])
			rec.JobStates["orders"] = state
			if err := r.store.save(rec); err != nil {
				t.Fatal(err)
			}
			_, err := r.clean(context.Background(), testProfile(), "run-1", true)
			if state == "planned" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				requireCode(t, err, "cleanup_job_unknown")
				if fake.deletes != 0 {
					t.Fatal("deleted with ambiguous job")
				}
			}
		})
	}
}
func TestJobPendingDeadlinePreservesEvidence(t *testing.T) {
	fake := testFake(t)
	fake.pending = true
	r := testRunner(t, fake, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	rec, err := r.prepare(ctx, testProfile(), "run-1", testInputs())
	requireCode(t, err, "job_pending")
	if rec.DataReady || rec.Jobs["orders"] == "" {
		t.Fatal("incorrect pending evidence")
	}
	_, err = r.clean(context.Background(), testProfile(), "run-1", true)
	requireCode(t, err, "cleanup_job_pending")
}
func TestWarningsAreNotFatal(t *testing.T) {
	fake := testFake(t)
	fake.loadWarning = true
	rec := ready(t, testRunner(t, fake, t.TempDir()))
	if len(rec.Warnings) == 0 {
		t.Fatal("lost warning")
	}
}
func TestFatalLoadNotReadyAndCanBeDisposed(t *testing.T) {
	fake := testFake(t)
	fake.loadError = true
	r := testRunner(t, fake, t.TempDir())
	rec, err := r.prepare(context.Background(), testProfile(), "run-1", testInputs())
	requireCode(t, err, "load_failed")
	if rec.DataReady {
		t.Fatal("failed load ready")
	}
	if _, err = r.clean(context.Background(), testProfile(), "run-1", true); err != nil {
		t.Fatal(err)
	}
}
func TestPermissionFailureLeavesIntent(t *testing.T) {
	fake := testFake(t)
	fake.deny = true
	r := testRunner(t, fake, t.TempDir())
	rec, err := r.prepare(context.Background(), testProfile(), "run-1", testInputs())
	requireCode(t, err, "dataset_create_unknown")
	saved, err := r.store.read(rec.Scope)
	if err != nil || saved.Phase != "intent" {
		t.Fatal("missing durable intent")
	}
}
func TestOperationLock(t *testing.T) {
	r := testRunner(t, testFake(t), t.TempDir())
	scope := scopeFor(testProfile(), "run-1")
	unlock, err := r.store.acquire(scope)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	_, err = r.prepare(context.Background(), testProfile(), "run-1", testInputs())
	requireCode(t, err, "operation_busy")
}
func TestInputsRejectTraversalSymlinkMalformedAndOversize(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "data"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readWorkspaceFile(root, "../data"); err == nil {
		t.Fatal("accepted traversal")
	}
	if err := os.Symlink(filepath.Join(outside, "data"), filepath.Join(root, "link")); err == nil {
		_, err = readWorkspaceFile(root, "link")
		requireCode(t, err, "input_path_outside_workspace")
	}
	if err := os.WriteFile(filepath.Join(root, "huge"), make([]byte, maxInputBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := readWorkspaceFile(root, "huge")
	requireCode(t, err, "input_too_large")
}

// This deliberately demonstrates the API's unfenced GET/DELETE race. Passing
// proves the counterexample, not distributed cleanup safety.
func TestDatasetDeleteCannotFenceExternalReplacement(t *testing.T) {
	fake := testFake(t)
	r := testRunner(t, fake, t.TempDir())
	ready(t, r)
	fake.recreateOnDel = true
	if _, err := r.clean(context.Background(), testProfile(), "run-1", true); err != nil {
		t.Fatal(err)
	}
	if fake.deletes != 1 {
		t.Fatal("counterexample did not execute")
	}
}

func TestExpiredOrReplacedTableCannotReuseOldLoadEvidence(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(fmt.Sprint(replace), func(t *testing.T) {
			fake := testFake(t)
			r := testRunner(t, fake, t.TempDir())
			rec := ready(t, r)
			key := rec.Target.DataProject + "." + rec.DatasetID + ".orders"
			if replace {
				fake.tables[key].CreationTime++
			} else {
				delete(fake.tables, key)
				delete(fake.rows, key)
			}
			loads := fake.loads
			retried, err := r.prepare(context.Background(), testProfile(), "run-1", testInputs())
			if err == nil || retried.DataReady {
				t.Fatal("reused load evidence for missing/replaced table")
			}
			if fake.loads != loads {
				t.Fatal("silently reseeded")
			}
			if !replace && fake.tables[key] != nil {
				t.Fatal("recreated empty table")
			}
		})
	}
}
func TestMissingSubmittedJobNeverResubmits(t *testing.T) {
	fake := testFake(t)
	r := testRunner(t, fake, t.TempDir())
	rec := ready(t, r)
	delete(fake.jobs, rec.Target.JobProject+"."+rec.Jobs["orders"])
	loads := fake.loads
	retried, err := r.prepare(context.Background(), testProfile(), "run-1", testInputs())
	requireCode(t, err, "job_state_unknown")
	if retried.DataReady || fake.loads != loads {
		t.Fatal("reseeded unknown job")
	}
}

func TestPartialSetupWithNoSubmittedJobsCanBeDisposed(t *testing.T) {
	fake := testFake(t)
	fake.schemaError = true
	r := testRunner(t, fake, t.TempDir())
	rec, err := r.prepare(context.Background(), testProfile(), "partial", testInputs())
	requireCode(t, err, "schema_create_unknown")
	if rec.CreationTime == 0 || rec.JobStates["orders"] != "planned" {
		t.Fatal("incorrect partial intent")
	}
	if _, err = r.clean(context.Background(), testProfile(), "partial", true); err != nil {
		t.Fatal(err)
	}
	if fake.deletes != 1 || fake.loads != 0 {
		t.Fatal("partial teardown did not dispose without loading")
	}
}

func TestMissingDatasetDoesNotHidePendingOrUnknownJobs(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprint(pending), func(t *testing.T) {
			fake := testFake(t)
			r := testRunner(t, fake, t.TempDir())
			rec := ready(t, r)
			delete(fake.datasets, rec.Target.DataProject+"."+rec.DatasetID)
			if pending {
				fake.jobs[rec.Target.JobProject+"."+rec.Jobs["orders"]].Status.State = "RUNNING"
			} else {
				delete(fake.jobs, rec.Target.JobProject+"."+rec.Jobs["orders"])
			}
			cleaned, err := r.clean(context.Background(), testProfile(), "run-1", true)
			want := "cleanup_job_unknown"
			if pending {
				want = "cleanup_job_pending"
			}
			requireCode(t, err, want)
			if cleaned.Phase == "cleaned" || fake.deletes != 0 {
				t.Fatal("reported disposal without job evidence")
			}
		})
	}
}
