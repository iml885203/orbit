package container

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/moby/moby/client"
)

func TestSweepReportsIndividualStopAndRemoveFailures(t *testing.T) {
	for _, operation := range []string{"stop", "remove"} {
		t.Run(operation, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/containers/json") {
					_ = json.NewEncoder(w).Encode([]map[string]any{{"Id": "owned", "Labels": map[string]string{labelManaged: "true"}}, {"Id": "foreign", "Labels": map[string]string{labelManaged: "true", labelNamespace: "another"}}})
					return
				}
				if strings.Contains(r.URL.Path, "foreign") {
					t.Error("mutated a foreign namespace")
				}
				failing := operation == "stop" && r.Method == http.MethodPost || operation == "remove" && r.Method == http.MethodDelete
				if failing {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"message":"controlled failure"}`))
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer api.Close()
			endpoint, _ := url.Parse(api.URL)
			cli, err := client.New(client.WithHost("tcp://"+endpoint.Host), client.WithScheme("http"), client.WithHTTPClient(api.Client()), client.WithAPIVersion("1.52"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cli.Close() }()
			manager := &Manager{cli: cli}
			err = manager.StopAll(context.Background())
			if err == nil || !strings.Contains(err.Error(), "owned") || !strings.Contains(err.Error(), "sweep "+operation) {
				t.Fatalf("result=%v", err)
			}
		})
	}
}
