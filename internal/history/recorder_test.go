package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecorderRecordListAndSubscribe(t *testing.T) {
	dir := t.TempDir()
	rec, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rec.Close() }()

	ch, done := rec.Subscribe()
	defer done()

	rec.Record(Record{ID: "a", Timestamp: time.Now(), Source: SourceUI, Path: "/api/up", Summary: "up", HasCLI: true, Status: StatusPending})
	rec.Record(Record{ID: "a", Timestamp: time.Now(), Source: SourceUI, Path: "/api/up", Summary: "up", HasCLI: true, Status: StatusOK, DurationMs: 12})
	rec.Record(Record{ID: "b", Timestamp: time.Now(), Source: SourceUI, Path: "/api/settings", Summary: "settings", HasCLI: false, Status: StatusOK})

	got := rec.List(Filter{OnlyNoCLI: true})
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("OnlyNoCLI got %+v", got)
	}
	got = rec.List(Filter{})
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "a" || got[1].Status != StatusOK {
		t.Fatalf("List should return latest record per action, got %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "history.jsonl")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for subscription")
		}
	}
}
