package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunSelfUpdateFailsWhenInstallScriptDownloadFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	t.Setenv("ORBIT_INSTALL_URL", server.URL+"/install.sh")

	err := runSelfUpdate(context.Background())
	if err == nil {
		t.Fatal("runSelfUpdate returned success after the install script download failed")
	}
	if !strings.Contains(err.Error(), "download install script") {
		t.Fatalf("error = %q, want download context", err)
	}
}
