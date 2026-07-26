package devdb

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDBOpsManager_LockOrReject_Exclusive(t *testing.T) {
	m := newDBOpsManager()
	if !m.LockOrReject(dbOpPublish, "X", false) {
		t.Fatal("first lock should succeed")
	}
	if m.LockOrReject(dbOpPublish, "Y", false) {
		t.Error("second lock must fail while op in flight")
	}
	m.Finish(true, 1, "", "")
	if !m.LockOrReject(dbOpPublish, "Y", false) {
		t.Error("lock should succeed after Finish releases")
	}
}

func TestDBOpsManager_Subscribe_IdleFrameWhenIdle(t *testing.T) {
	m := newDBOpsManager()
	ch, cancel := m.Subscribe()
	defer cancel()
	select {
	case f := <-ch:
		if f.Kind != "idle" {
			t.Errorf("first frame = %q, want idle", f.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("no initial frame")
	}
}

func TestDBOpsManager_Subscribe_ReplaysWhileInFlight(t *testing.T) {
	m := newDBOpsManager()
	if !m.LockOrReject(dbOpPublish, "AppDB", false) {
		t.Fatal("lock")
	}
	_, _ = m.Write([]byte("line one\nline two\n"))

	ch, cancel := m.Subscribe()
	defer cancel()

	want := []string{"start", "output", "output"}
	got := []string{}
	for i := 0; i < 3; i++ {
		select {
		case f := <-ch:
			got = append(got, f.Kind)
		case <-time.After(time.Second):
			t.Fatalf("only got %d frames", i)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDBOpsManager_Write_SplitsOnNewlines(t *testing.T) {
	m := newDBOpsManager()
	if !m.LockOrReject(dbOpPublish, "X", false) {
		t.Fatal("lock")
	}
	ch, cancel := m.Subscribe()
	defer cancel()
	// Drain initial start frame.
	<-ch

	_, _ = m.Write([]byte("alpha\nbeta\npart"))
	_, _ = m.Write([]byte("ial line\n"))

	want := []string{"alpha", "beta", "partial line"}
	for _, exp := range want {
		select {
		case f := <-ch:
			if f.Kind != "output" || f.Line != exp {
				t.Errorf("got kind=%q line=%q, want output / %q", f.Kind, f.Line, exp)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing frame for %q", exp)
		}
	}
}

func TestDBOpsManager_Finish_EmitsDoneAndFlushesPartial(t *testing.T) {
	m := newDBOpsManager()
	if !m.LockOrReject(dbOpPublish, "X", false) {
		t.Fatal("lock")
	}
	ch, cancel := m.Subscribe()
	defer cancel()
	<-ch // drain start

	_, _ = m.Write([]byte("no newline ending"))
	m.Finish(false, 1234, "boom", "")

	// Expect: output (the trailing partial) then done.
	got := []DBOpFrame{}
	for i := 0; i < 2; i++ {
		select {
		case f := <-ch:
			got = append(got, f)
		case <-time.After(time.Second):
			t.Fatalf("only got %d frames", i)
		}
	}
	if got[0].Kind != "output" || got[0].Line != "no newline ending" {
		t.Errorf("expected partial-line flush, got %+v", got[0])
	}
	if got[1].Kind != "done" || got[1].OK || got[1].DurationMs != 1234 || got[1].Err != "boom" {
		t.Errorf("expected done failure frame, got %+v", got[1])
	}
}

func TestDBOpsManager_ConcurrentWrites_NoRaces(t *testing.T) {
	m := newDBOpsManager()
	if !m.LockOrReject(dbOpPublish, "X", false) {
		t.Fatal("lock")
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Write([]byte("hello\n"))
		}()
	}
	wg.Wait()
	m.Finish(true, 1, "", "")
}

func TestDBOpsManager_InFlight_ReturnsCurrentOp(t *testing.T) {
	m := newDBOpsManager()
	k, db := m.InFlight()
	if k != "" || db != "" {
		t.Errorf("idle InFlight should be empty, got %q %q", k, db)
	}
	m.LockOrReject(dbOpPublish, "AppDB", false)
	k, db = m.InFlight()
	if k != dbOpPublish || db != "AppDB" {
		t.Errorf("got %q %q, want publish AppDB", k, db)
	}
	m.Finish(true, 0, "", "")
	k, db = m.InFlight()
	if k != "" || db != "" {
		t.Error("after Finish, InFlight must be empty")
	}
}
