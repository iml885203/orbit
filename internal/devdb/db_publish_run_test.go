package devdb

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValidatePublishArtifacts_ChecksEveryProjectBeforePublish(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "First")
	if err := os.Mkdir(first, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "First.dacpac"), []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	targets := []publishTargetRef{
		{DB: "First", SQLProj: "/workspace/First.sqlproj"},
		{DB: "Second", SQLProj: "/workspace/Second.sqlproj"},
	}

	targets = append(targets, publishTargetRef{DB: "Third", SQLProj: "/workspace/Third.sqlproj"})
	err := validatePublishArtifacts(root, targets)
	if err == nil || !strings.Contains(err.Error(), "project Second") || !strings.Contains(err.Error(), "project Third") {
		t.Fatalf("validation error = %v, want all missing projects before publishing begins", err)
	}
}

// runBoundedPublish is the mutex/semaphore core of --parallel, exercised
// here with a fake work func (no real SQL): it must run every target,
// never exceed the concurrency bound, and report all failures together
// rather than stopping at the first.
func TestRunBoundedPublish(t *testing.T) {
	targets := make([]publishTargetRef, 20)
	for i := range targets {
		targets[i] = publishTargetRef{DB: fmt.Sprintf("DB%d", i)}
	}

	var mu sync.Mutex
	ran := map[string]bool{}
	inFlight, maxInFlight := 0, 0
	release := make(chan struct{})
	full := make(chan struct{})
	var fullOnce sync.Once
	work := func(target publishTargetRef) (string, error) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		if inFlight == 4 {
			fullOnce.Do(func() { close(full) })
		}
		mu.Unlock()
		<-release
		mu.Lock()
		ran[target.DB] = true
		inFlight--
		mu.Unlock()
		if target.DB == "DB7" || target.DB == "DB13" {
			return "output", fmt.Errorf("boom")
		}
		return "ok", nil
	}

	done := make(chan error, 1)
	go func() {
		done <- runBoundedPublish(targets, 4, "published", io.Discard, work)
	}()
	select {
	case <-full:
	case <-time.After(time.Second):
		t.Fatal("publish workers did not reach the concurrency cap")
	}
	close(release)
	err := <-done

	if len(ran) != 20 {
		t.Errorf("every target must run; ran %d of 20", len(ran))
	}
	if maxInFlight > 4 {
		t.Errorf("concurrency bound exceeded: %d workers in flight, cap 4", maxInFlight)
	}
	if maxInFlight != 4 {
		t.Errorf("expected the worker cap to be exercised; peak in-flight was %d", maxInFlight)
	}
	if err == nil {
		t.Fatal("expected an aggregate error for the two failures")
	}
	if !strings.Contains(err.Error(), "2 of 20 published failed") {
		t.Errorf("error should summarise both failures; got %q", err)
	}
	for _, db := range []string{"DB7", "DB13"} {
		if !strings.Contains(err.Error(), db) {
			t.Errorf("error should name failed %s; got %q", db, err)
		}
	}

	// All-success returns nil.
	ok := func(publishTargetRef) (string, error) { return "ok", nil }
	if err := runBoundedPublish(targets[:3], 2, "published", io.Discard, ok); err != nil {
		t.Errorf("all-success run should return nil; got %v", err)
	}
}

func TestForcedPublishRequiresVisibleApproval(t *testing.T) {
	previousForce, previousYes := publishForce, publishYes
	t.Cleanup(func() {
		publishForce = previousForce
		publishYes = previousYes
	})
	targets := []publishTargetRef{{DB: "Accounts"}, {DB: "Orders"}}

	publishForce, publishYes = false, false
	if !authorizeForcedPublish(targets) {
		t.Fatal("ordinary data-preserving publish must not prompt")
	}

	publishForce, publishYes = true, true
	if !authorizeForcedPublish(targets) {
		t.Fatal("--force --yes must count as explicit approval")
	}

	publishForce, publishYes = true, false
	if authorizeForcedPublish(targets) {
		t.Fatal("non-interactive --force without --yes must abort")
	}

	prompt := forcedPublishPrompt(targets)
	for _, want := range []string{"Accounts, Orders", "permanently delete data"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt %q does not make scope and consequence visible; missing %q", prompt, want)
		}
	}
}
