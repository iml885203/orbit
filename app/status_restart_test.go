package app

import (
	"testing"
	"time"

	"github.com/iml885203/orbit/daemon"
)

func TestApplyRuntimeStatusPreservesExternalRestartTruth(t *testing.T) {
	startedAt := time.Date(2026, time.July, 28, 14, 4, 6, 0, time.UTC)
	observedAt := startedAt.Add(2 * time.Second)
	source := daemon.ResourceStatus{
		Name:                 "redis",
		Kind:                 daemon.ResourceKindContainer,
		State:                "healthy",
		ExternalRestartCount: 1,
		LastRestart: &daemon.ResourceRestart{
			Source: "external", StartedAt: startedAt, ObservedAt: observedAt,
		},
		Uptime: "4s",
	}
	var target jsonService

	applyRuntimeStatus(&target, source, map[string]daemon.ResourceStatus{"redis": source})

	if target.ExternalRestartCount != 1 {
		t.Fatalf("ExternalRestartCount = %d, want 1", target.ExternalRestartCount)
	}
	if target.LastRestart == nil || !target.LastRestart.StartedAt.Equal(startedAt) {
		t.Fatalf("LastRestart = %#v, want external restart at %s", target.LastRestart, startedAt)
	}
	if detail := statusDetail(source, map[string]daemon.ResourceStatus{"redis": source}); detail == "" {
		t.Fatal("human status hides external restart")
	}
}
