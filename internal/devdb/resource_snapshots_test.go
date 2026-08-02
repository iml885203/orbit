package devdb

import (
	"testing"
	"time"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/dbstate"
)

func TestSnapshotDatabases_DerivesStates(t *testing.T) {
	at := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	snap := dbstate.Snapshot{DBs: map[string]dbstate.DBState{
		"CatalogDB":   {Name: "CatalogDB"},
		"OrdersDB":    {Name: "OrdersDB", LastApply: &dbstate.Event{At: at, Source: "cli"}},
		"InventoryDB": {Name: "InventoryDB", ResetPending: true, ResetError: "sql-server not ready"},
	}}

	states := map[string]daemon.ResourceSnapshot{}
	for _, r := range snapshotDatabases(snap, "sql-server") {
		states[r.Name] = r
		if r.Type != "database" || r.Parent != "sql-server" {
			t.Errorf("%s: wrong type/parent: %+v", r.Name, r)
		}
	}
	if states["CatalogDB"].State != "baseline" {
		t.Errorf("CatalogDB state = %q, want baseline", states["CatalogDB"].State)
	}
	if states["OrdersDB"].State != "modified" || states["OrdersDB"].Properties["last_apply"] == "" {
		t.Errorf("OrdersDB snapshot wrong: %+v", states["OrdersDB"])
	}
	if states["InventoryDB"].State != "reset_pending" || states["InventoryDB"].StateReason != "sql-server not ready" {
		t.Errorf("InventoryDB snapshot wrong: %+v", states["InventoryDB"])
	}
}
