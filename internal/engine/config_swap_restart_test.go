package engine

import (
	"context"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
)

// The SQL mode-switch correctness path: after the daemon publishes a config
// with a different sql-server image, RestartService must launch the
// container from the NEW snapshot — startService takes one Load per start,
// so the restart generation picks up the published splice.
func TestRestartService_UsesFreshlyPublishedImage(t *testing.T) {
	holder := config.NewHolder(&config.Config{
		Containers: map[string]*config.Container{
			"sql-server": {Name: "sql-server", Image: "registry/example:remote"},
		},
	})
	o := NewOrchestrator(holder, nil, nil)
	started := make(chan string, 2)
	o.OnStartContainer = func(_ context.Context, _ string, c *config.Container) error {
		started <- c.Image
		return nil
	}
	o.OnStopContainer = func(_ context.Context, _ string) error { return nil }
	o.OnHealthCheck = func(_ context.Context, _ string, _ int) error { return nil }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = o.Run(ctx) }()

	o.Start([]string{"sql-server"})
	select {
	case img := <-started:
		if img != "registry/example:remote" {
			t.Fatalf("first start image = %q, want the original", img)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("sql-server never started")
	}

	// The daemon's writer publishes the local-mode splice…
	holder.Store(holder.Load().WithContainer("sql-server",
		&config.Container{Name: "sql-server", Image: "example.db:latest"}))

	// …and the restart must start from the new snapshot.
	if err := o.RestartService(context.Background(), "sql-server"); err != nil {
		t.Fatal(err)
	}
	select {
	case img := <-started:
		if img != "example.db:latest" {
			t.Fatalf("restart image = %q, want the freshly published example.db:latest", img)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("sql-server never restarted")
	}
}
