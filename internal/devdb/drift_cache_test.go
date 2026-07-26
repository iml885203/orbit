package devdb

import (
	"errors"
	"testing"

	"github.com/iml885203/orbit/internal/sqlpublish"
)

func recordCurrent(t *testing.T, c *driftCache, db string, result *sqlpublish.DiffResult, code sqlpublish.ErrorCode, err error) {
	t.Helper()
	if !c.recordIfCurrent(db, c.currentGeneration(db), result, code, err) {
		t.Fatal("current generation was rejected")
	}
}

func TestDriftCache_RecordAndSnapshotOrder(t *testing.T) {
	c := newDriftCache()
	recordCurrent(t, c, "B", &sqlpublish.DiffResult{DB: "B", InSync: true}, sqlpublish.CodeNone, nil)
	recordCurrent(t, c, "A", nil, sqlpublish.CodePublishFailed, errors.New("boom"))

	snap := c.snapshot()
	if len(snap) != 2 || snap[0].DB != "A" || snap[1].DB != "B" {
		t.Fatalf("want [A B]; got %+v", snap)
	}
	if snap[0].Error != "boom" || snap[0].Code != string(sqlpublish.CodePublishFailed) || snap[0].Result != nil {
		t.Errorf("error entry not recorded: %+v", snap[0])
	}
	if snap[1].Result == nil || !snap[1].Result.InSync || snap[1].Error != "" {
		t.Errorf("result entry not recorded: %+v", snap[1])
	}
	if snap[0].At == "" || snap[1].At == "" {
		t.Error("entries must carry a timestamp")
	}
}

func TestDriftCache_MarkStaleThenRecordClears(t *testing.T) {
	c := newDriftCache()
	c.markStale("A") // never diffed — must be a no-op, not create an entry
	if len(c.snapshot()) != 0 {
		t.Fatal("markStale of an unknown DB must not create an entry")
	}

	recordCurrent(t, c, "A", &sqlpublish.DiffResult{DB: "A"}, sqlpublish.CodeNone, nil)
	c.markStale("A")
	if snap := c.snapshot(); !snap[0].Stale {
		t.Fatal("entry must be stale after markStale")
	}
	// A fresh diff replaces the stale entry outright.
	recordCurrent(t, c, "A", &sqlpublish.DiffResult{DB: "A"}, sqlpublish.CodeNone, nil)
	if snap := c.snapshot(); snap[0].Stale {
		t.Fatal("recording a fresh diff must clear the stale flag")
	}
}

func TestDriftCache_MarkAllStale(t *testing.T) {
	c := newDriftCache()
	recordCurrent(t, c, "FirstDB", &sqlpublish.DiffResult{DB: "FirstDB", InSync: true}, sqlpublish.CodeNone, nil)
	recordCurrent(t, c, "SecondDB", &sqlpublish.DiffResult{DB: "SecondDB", Created: 1}, sqlpublish.CodeNone, nil)

	c.markAllStale()

	snapshot := c.snapshot()
	if len(snapshot) != 2 {
		t.Fatalf("got %d entries, want 2", len(snapshot))
	}
	for _, entry := range snapshot {
		if !entry.Stale {
			t.Errorf("%s remained fresh after target image changed", entry.DB)
		}
	}
}

func TestDriftCache_DiffStartedBeforePublishCannotOverwriteStaleState(t *testing.T) {
	c := newDriftCache()
	recordCurrent(t, c, "AppDB", &sqlpublish.DiffResult{DB: "AppDB", InSync: true}, sqlpublish.CodeNone, nil)
	generation := c.currentGeneration("AppDB")

	c.markStale("AppDB")
	recorded := c.recordIfCurrent("AppDB", generation, &sqlpublish.DiffResult{DB: "AppDB", Created: 1}, sqlpublish.CodeNone, nil)

	if recorded {
		t.Fatal("a diff started before publish overwrote the invalidation")
	}
	snapshot := c.snapshot()
	if len(snapshot) != 1 || !snapshot[0].Stale || snapshot[0].Result.Created != 0 {
		t.Fatalf("publish invalidation was lost: %+v", snapshot)
	}
}

func TestDriftCache_DiffStartedBeforeImageSwitchCannotRecord(t *testing.T) {
	c := newDriftCache()
	generation := c.currentGeneration("AppDB")

	c.markAllStale()
	recorded := c.recordIfCurrent("AppDB", generation, &sqlpublish.DiffResult{DB: "AppDB", InSync: true}, sqlpublish.CodeNone, nil)

	if recorded || len(c.snapshot()) != 0 {
		t.Fatal("a diff from the previous target image was recorded as current")
	}
}
