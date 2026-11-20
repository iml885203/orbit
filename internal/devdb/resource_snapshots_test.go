package devdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iml885203/orbit/internal/daemon"
)

func TestDatabaseResourcesFollowRecordedEvents(t *testing.T) {
	feature := newTestDBFeature(t, arbitraryTargetConfig())
	host := feature.host.(*fakeDaemonHost)
	mux := http.NewServeMux()
	SetupDaemon(host, mux)
	if len(host.resources) != 1 {
		t.Fatalf("database contributor not registered: %d", len(host.resources))
	}
	for _, event := range []string{
		`{"kind":"publish","db":"CatalogDB","source":"cli","status":"ok"}`,
		`{"kind":"apply","db":"OrdersDB","source":"cli","status":"ok"}`,
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/db-state/event", strings.NewReader(event)))
		if response.Code != http.StatusOK {
			t.Fatalf("record DB event: %d %s", response.Code, response.Body.String())
		}
	}
	states := map[string]daemon.ResourceSnapshot{}
	for _, resource := range host.resources[0](context.Background()) {
		states[resource.Name] = resource
		if resource.Type != "database" || resource.Parent != "mydb" {
			t.Fatalf("wrong resource ownership: %+v", resource)
		}
	}
	if len(states) != 2 || states["CatalogDB"].State != "baseline" {
		t.Fatalf("baseline resources: %+v", states)
	}
	if states["OrdersDB"].State != "modified" || !strings.Contains(states["OrdersDB"].Properties["last_apply"], "(cli)") {
		t.Fatalf("apply evidence: %+v", states["OrdersDB"])
	}
}
