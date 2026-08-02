package dbstate

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStore_ApplyThenReset_ClearsLastApply(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply("AppDB", SourceCLI, 1234); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if snap.DBs["AppDB"].LastApply == nil {
		t.Fatal("LastApply must be set after Apply")
	}

	if err := s.Reset("AppDB", SourceUI, 500); err != nil {
		t.Fatal(err)
	}
	snap = s.Snapshot()
	if snap.DBs["AppDB"].LastApply != nil {
		t.Error("LastApply must be cleared after Reset")
	}
	if snap.DBs["AppDB"].LastReset == nil {
		t.Error("LastReset must be set after Reset")
	}
}

func TestStore_PersistAndReload(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	_ = s.Apply("AppDB", SourceCLI, 100)
	_ = s.Reset("OrdersDB", SourceUI, 200)
	want := s.Snapshot()

	s2, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.Snapshot()
	if len(got.DBs) != len(want.DBs) {
		t.Fatalf("reloaded dbs = %d, want %d", len(got.DBs), len(want.DBs))
	}
	if got.DBs["AppDB"].LastApply == nil {
		t.Error("AppDB.LastApply lost across reload")
	}
	if got.DBs["OrdersDB"].LastReset == nil {
		t.Error("OrdersDB.LastReset lost across reload")
	}
}

func TestStore_MalformedFile_StartsEmpty(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "db-state.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshot().DBs) != 0 {
		t.Error("malformed file should yield empty state, not crash")
	}
}

func TestStore_Subscribe_DeliversInitialAndOnUpdate(t *testing.T) {
	s, _ := New(t.TempDir())
	_ = s.Apply("X", SourceCLI, 1)

	ch, cancel := s.Subscribe()
	defer cancel()

	select {
	case snap := <-ch:
		if snap.DBs["X"].LastApply == nil {
			t.Error("initial frame must contain current state")
		}
	case <-time.After(time.Second):
		t.Fatal("no initial frame")
	}

	_ = s.Reset("X", SourceUI, 1)
	select {
	case snap := <-ch:
		if snap.DBs["X"].LastApply != nil {
			t.Error("update frame should reflect Reset")
		}
	case <-time.After(time.Second):
		t.Fatal("no update frame")
	}
}

func TestStore_ConcurrentWrites_NoRaces(t *testing.T) {
	s, _ := New(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			db := "DB" + string(rune('A'+(i%5)))
			if i%2 == 0 {
				_ = s.Apply(db, SourceCLI, int64(i))
			} else {
				_ = s.Reset(db, SourceUI, int64(i))
			}
		}(i)
	}
	wg.Wait()
	if len(s.Snapshot().DBs) == 0 {
		t.Error("no state recorded after concurrent writes")
	}
}
