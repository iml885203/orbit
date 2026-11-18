package bigquery

import (
	"context"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func requireDirectoryPermissions(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires POSIX directory permissions enforced for a non-root user")
	}
}

func denyReceiptWrites(t *testing.T, dir string) {
	t.Helper()
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0700); err != nil {
			t.Error(err)
		}
	})
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
}

func TestReceiptFailureBeforeCreateNeverContactsCloud(t *testing.T) {
	requireDirectoryPermissions(t)
	fake := testFake(t)
	r := testRunner(t, fake, t.TempDir())
	unlock, err := r.store.acquire(scopeFor(testProfile(), "run-1"))
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	denyReceiptWrites(t, r.store.dir)
	rec, err := r.prepare(context.Background(), testProfile(), "run-1", testInputs())
	requireCode(t, err, "receipt_write_failed")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if rec.DataReady || fake.requests != 0 || len(fake.datasets) != 0 {
		t.Fatal("cloud mutation occurred without durable intent")
	}
}

func TestReceiptFailureAfterCreateResumesOriginalDataset(t *testing.T) {
	requireDirectoryPermissions(t)
	fake := testFake(t)
	dir := t.TempDir()
	r := testRunner(t, fake, dir)
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0700); err != nil {
			t.Error(err)
		}
	})
	original := fake.server.Config.Handler
	var once sync.Once
	fake.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/datasets/") {
			once.Do(func() {
				if err := os.Chmod(dir, 0500); err != nil {
					t.Error(err)
				}
			})
		}
		original.ServeHTTP(w, req)
	})
	rec, err := r.prepare(context.Background(), testProfile(), "run-1", testInputs())
	requireCode(t, err, "receipt_write_failed")
	saved, err := r.store.read(rec.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if rec.DataReady || saved.Phase != "intent" || saved.DatasetID != rec.DatasetID || saved.Jobs["orders"] != rec.Jobs["orders"] {
		t.Fatal("lost durable intent after accepted dataset creation")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	resumed := ready(t, testRunner(t, fake, dir))
	if resumed.DatasetID != saved.DatasetID || resumed.Jobs["orders"] != saved.Jobs["orders"] {
		t.Fatal("recovery replaced the original operation")
	}
	fake.mu.Lock()
	datasets, loads := len(fake.datasets), fake.loads
	fake.mu.Unlock()
	if datasets != 1 || loads != 1 {
		t.Fatalf("datasets=%d loads=%d", datasets, loads)
	}
	if _, err := r.clean(context.Background(), testProfile(), "run-1", true); err != nil {
		t.Fatal(err)
	}
}

func TestCloudMetadataFailureBlocksReadinessAndDeletion(t *testing.T) {
	for _, scenario := range []struct{ name, prepareCode, cleanCode string }{
		{"dataset_location", "ownership_mismatch", "ownership_mismatch"},
		{"job_location", "job_state_unknown", "cleanup_job_unknown"},
		{"metadata_unavailable", "dataset_state_unknown", "dataset_state_unknown"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fake := testFake(t)
			r := testRunner(t, fake, t.TempDir())
			rec := ready(t, r)
			fake.mu.Lock()
			switch scenario.name {
			case "dataset_location":
				fake.datasets[rec.Target.DataProject+"."+rec.DatasetID].Location = "EU"
			case "job_location":
				fake.jobs[rec.Target.JobProject+"."+rec.Jobs["orders"]].JobReference.Location = "EU"
			case "metadata_unavailable":
				fake.metadataError = true
			}
			fake.mu.Unlock()
			retried, err := r.prepare(context.Background(), testProfile(), "run-1", testInputs())
			requireCode(t, err, scenario.prepareCode)
			if retried.DataReady {
				t.Fatal("uncertain cloud evidence reported ready")
			}
			cleaned, err := r.clean(context.Background(), testProfile(), "run-1", true)
			requireCode(t, err, scenario.cleanCode)
			if cleaned.Phase == "cleaned" {
				t.Fatal("uncertain cloud evidence reported disposed")
			}
			saved, err := r.store.read(rec.Scope)
			if err != nil {
				t.Fatal(err)
			}
			if saved.DatasetID != rec.DatasetID || saved.Jobs["orders"] != rec.Jobs["orders"] || saved.DataReady {
				t.Fatal("recovery evidence lost or reported ready")
			}
			loads, deletes := fake.counts()
			if loads != 1 || deletes != 0 {
				t.Fatalf("loads=%d deletes=%d", loads, deletes)
			}
		})
	}
}
