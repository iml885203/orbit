package daemon

import (
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/engine"
)

func TestResourceLastRestartDoesNotUseCurrentManagedRuntime(t *testing.T) {
	externalStart := time.Now().Add(-time.Minute)
	managedStart := time.Now()
	observedAt := externalStart.Add(time.Second)
	svc := &engine.ServiceInfo{
		ContainerStartedAt:    managedStart,
		LastExternalStartedAt: externalStart,
		LastExternalRestart:   observedAt,
	}

	restart := resourceLastRestart(svc)

	if restart == nil || !restart.StartedAt.Equal(externalStart) {
		t.Fatalf("restart = %#v, want external runtime %s", restart, externalStart)
	}
}
