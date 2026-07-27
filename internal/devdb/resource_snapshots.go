package devdb

// DB-workflow resource contributions (databases under sql-server) —
// moved from the core aggregation (spec B6).

import (
	"context"
	"fmt"
	"time"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/dbstate"
)

// dbResources is the registered contributor: databases.
func (f *dbFeature) dbResources(_ context.Context) []daemon.ResourceSnapshot {
	var out []daemon.ResourceSnapshot
	if f.dbState != nil {
		section := SQLServerFrom(f.host.Config())
		if section != nil {
			out = append(out, snapshotDatabases(f.dbState.Snapshot(), section.Target)...)
		}
	}
	return out
}

// snapshotDatabases derives a per-database state from the dbstate event
// log: baseline (matches the image), modified (a local apply sits on top),
// or reset_pending (a post-build reset failed and needs attention).
func snapshotDatabases(snap dbstate.Snapshot, parent string) []daemon.ResourceSnapshot {
	out := make([]daemon.ResourceSnapshot, 0, len(snap.DBs))
	for name, db := range snap.DBs {
		state, reason := db.DerivedState()
		props := map[string]string{}
		if db.LastApply != nil {
			props["last_apply"] = fmt.Sprintf("%s (%s)", db.LastApply.At.Format(time.RFC3339), db.LastApply.Source)
		}
		if db.LastReset != nil {
			props["last_reset"] = fmt.Sprintf("%s (%s)", db.LastReset.At.Format(time.RFC3339), db.LastReset.Source)
		}
		if db.LastBuild != nil {
			props["last_build"] = fmt.Sprintf("%s (%s)", db.LastBuild.At.Format(time.RFC3339), db.LastBuild.Project)
		}
		out = append(out, daemon.ResourceSnapshot{
			Name:        name,
			Type:        "database",
			State:       state,
			StateReason: reason,
			Parent:      parent,
			Properties:  emptyAsNil(props),
		})
	}
	return out
}

// emptyAsNil keeps zero-property resources free of an empty map on the
// wire (mirrors the core aggregation helper).
func emptyAsNil(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}
