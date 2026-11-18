package bigquery

import (
	"context"
	"github.com/iml885203/orbit/internal/config"
	"os"
	"testing"
	"time"
)

// This opt-in smoke reads cloud metadata only. Dataset creation, application
// assertions and disposal remain the explicit documented live journey.
func TestLiveSharedMetadata(t *testing.T) {
	if os.Getenv("ORBIT_BQ_LIVE") != "1" {
		t.Skip("SKIP — no explicit real GCP test project/credentials opt-in")
	}
	path, name := os.Getenv("ORBIT_BQ_LIVE_CONFIG"), os.Getenv("ORBIT_BQ_LIVE_PROFILE")
	if path == "" || name == "" {
		t.Fatal("ORBIT_BQ_LIVE_CONFIG and ORBIT_BQ_LIVE_PROFILE are required")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c := From(cfg)
	if c == nil {
		t.Fatal("BigQuery is not configured")
	}
	p, ok := c.Profiles[name]
	if !ok || p.Mode != "shared" {
		t.Fatal("live smoke requires an explicit shared profile")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	api, err := inspectionClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := api.Datasets.Get(p.DataProject, p.Dataset).Context(ctx).Do()
	if err != nil {
		t.Fatal(err)
	}
	if dataset.Location != p.Location {
		t.Fatalf("location mismatch: got %s want %s", dataset.Location, p.Location)
	}
	t.Log("Real dataset metadata read passed; job creation, GoogleSQL, app identity and full E2E remain unverified")
}
