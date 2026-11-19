package autoupdate

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func notificationState(t *testing.T) State {
	t.Helper()
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	s, err := Load(filepath.Join(t.TempDir(), "orbit"))
	if err != nil {
		t.Fatal(err)
	}
	s.CurrentVersion = "v1.3.0"
	return s
}

func emitNotification(t *testing.T, s State, installed string, now time.Time) string {
	t.Helper()
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Notify(s.LaunchPath, installed, now, &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestNotificationDescribesUpdateEvidence(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name, want, absent string
		change             func(*State)
	}{
		{"staged", "verified and ready", "updated to", func(s *State) {
			s.Phase, s.TargetVersion, s.StagedBinary = "ready", "v1.4.0", "staged"
			s.StagedEvidence = &VerificationRecord{Tag: "v1.4.0"}
		}},
		{"unverified", "", "verified", func(s *State) { s.Phase, s.TargetVersion, s.StagedBinary = "ready", "v1.4.0", "staged" }},
		{"homebrew", "brew upgrade iml885203/tap/orbit", "verified", func(s *State) { s.Owner, s.Phase, s.TargetVersion = OwnerHomebrew, "available", "v1.4.0" }},
		{"scoop", "scoop update orbit", "automatically", func(s *State) { s.Owner, s.Phase, s.TargetVersion = OwnerScoop, "available", "v1.4.0" }},
		{"already-installed", "", "available", func(s *State) { s.Owner, s.Phase, s.TargetVersion = OwnerHomebrew, "available", "v1.3.0" }},
		{"policy-off", "", "available", func(s *State) {
			s.Owner, s.Phase, s.TargetVersion, s.Policy = OwnerHomebrew, "available", "v1.4.0", PolicyOff
		}},
		{"check-failed", "could not check", "could not complete", func(s *State) { s.Phase, s.LastError, s.LastCheckedAt = "failed", "offline", &now }},
		{"apply-failed", "could not complete", "updated to", func(s *State) { s.Transaction = &Transaction{ID: "fail", Phase: "failed", FinishedAt: &now} }},
		{"partial", "not restored", "updated to", func(s *State) { s.Transaction = &Transaction{ID: "partial", Phase: "partial", FinishedAt: &now} }},
		{"transaction-in-progress", "", "ready", func(s *State) { s.Transaction = &Transaction{ID: "active", Phase: "applying"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := notificationState(t)
			tc.change(&s)
			got := emitNotification(t, s, "v1.3.0", now)
			if (tc.want == "" && got != "") || !strings.Contains(got, tc.want) || strings.Contains(got, tc.absent) {
				t.Fatalf("notice = %q; want %q, absent %q", got, tc.want, tc.absent)
			}
		})
	}
}

func TestNotificationDedupeSeparatesDiscoveryAndStateChanges(t *testing.T) {
	s := notificationState(t)
	now := time.Now().UTC()
	s.Owner, s.Phase, s.TargetVersion, s.LastCheckedAt = OwnerHomebrew, "available", "v1.4.0", &now
	if got := emitNotification(t, s, "v1.3.0", now); got == "" {
		t.Fatal("missing initial notice")
	}
	if got := emitNotification(t, s, "v1.3.0", now.Add(time.Hour)); got != "" {
		t.Fatalf("repeated notice: %q", got)
	}
	if got := emitNotification(t, s, "v1.3.0", now.Add(24*time.Hour)); got == "" {
		t.Fatal("missing daily reminder")
	}
	s.TargetVersion = "v1.5.0"
	if got := emitNotification(t, s, "v1.3.0", now.Add(24*time.Hour)); !strings.Contains(got, "v1.5.0") {
		t.Fatalf("new target suppressed: %q", got)
	}
	got, err := Load(s.LaunchPath)
	if err != nil || !got.LastCheckedAt.Equal(now) {
		t.Fatalf("notification changed discovery state: %+v, %v", got, err)
	}
}

func TestNotificationSuccessRequiresMatchingInstalledTransactionAndRestoration(t *testing.T) {
	s := notificationState(t)
	now := time.Now().UTC()
	s.Transaction = &Transaction{ID: "completed", Operation: "automatic", Phase: "succeeded", TargetVersion: "v1.4.0", FinishedAt: &now,
		RuntimeOutcomes: map[string]RuntimeOutcome{"default": {Phase: "restored"}}}
	if got := emitNotification(t, s, "v1.3.0", now); got != "" {
		t.Fatalf("announced uninstalled target: %q", got)
	}
	if got := emitNotification(t, s, "v1.4.0", now); !strings.Contains(got, "updated to v1.4.0") {
		t.Fatalf("missing success: %q", got)
	}
	if got := emitNotification(t, s, "v1.4.0", now.Add(48*time.Hour)); got != "" {
		t.Fatalf("repeated success: %q", got)
	}
	s.Transaction.ID = "incomplete"
	s.Transaction.RuntimeOutcomes["default"] = RuntimeOutcome{Phase: "failed", Error: "restore failed"}
	if got := emitNotification(t, s, "v1.4.0", now); got != "" {
		t.Fatalf("claimed restoration success: %q", got)
	}
}

type failedNoticeWriter struct{}

func (failedNoticeWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestNotificationDoesNotConsumeFailedOutputOrWaitForRegistryLock(t *testing.T) {
	s := notificationState(t)
	s.Owner, s.Phase, s.TargetVersion = OwnerScoop, "available", "v1.4.0"
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	dir, _ := GlobalDir()
	if err := os.Mkdir(filepath.Join(dir, "update-registry.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := Notify(s.LaunchPath, "v1.3.0", started, failedNoticeWriter{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write error = %v", err)
	}
	var out bytes.Buffer
	if err := Notify(s.LaunchPath, "v1.3.0", started, &out); err != nil || out.Len() == 0 {
		t.Fatalf("failed output consumed notice: %q, %v", out.String(), err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("notification waited for registry lock")
	}
}

func TestNotificationConcurrentProcesses(t *testing.T) {
	if os.Getenv("ORBIT_NOTICE_CHILD") == "1" {
		_ = Notify(os.Getenv("ORBIT_NOTICE_LAUNCH"), "v1.3.0", time.Now(), os.Stdout)
		return
	}
	s := notificationState(t)
	s.Owner, s.Phase, s.TargetVersion = OwnerHomebrew, "available", "v1.4.0"
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	outputs := make([][]byte, 8)
	for i := range outputs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestNotificationConcurrentProcesses$")
			cmd.Env = append(os.Environ(), "ORBIT_NOTICE_CHILD=1", "ORBIT_NOTICE_LAUNCH="+s.LaunchPath)
			var err error
			outputs[i], err = cmd.CombinedOutput()
			if err != nil {
				t.Errorf("child: %v, %s", err, outputs[i])
			}
		}()
	}
	wg.Wait()
	count := 0
	for _, out := range outputs {
		count += strings.Count(string(out), "Orbit v1.4.0 is available")
	}
	if count != 1 {
		t.Fatalf("%d concurrent notifications; outputs=%q", count, outputs)
	}
}

func TestBackgroundNotificationBoundarySkipsBusyUpdateRegistry(t *testing.T) {
	s := notificationState(t)
	s.Owner, s.Phase, s.TargetVersion = OwnerHomebrew, "available", "v1.2.0"
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	dir, _ := GlobalDir()
	if err := os.Mkdir(filepath.Join(dir, "update-registry.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	got, err := TryReconcileInstalled(s.LaunchPath, "v1.3.0")
	if err != nil || got.TargetVersion != "" {
		t.Fatalf("reconcile view = %+v, %v", got, err)
	}
	checker := Checker{Channel: Channel{ReleaseAPIURL: "https://example.invalid/releases"}}
	if claimed, err := checker.ClaimBackgroundCheck(s.LaunchPath); err == nil || claimed {
		t.Fatalf("busy background claim = %v, %v", claimed, err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("background notification boundary waited for the update registry")
	}
}
