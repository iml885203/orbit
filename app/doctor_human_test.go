package app

import (
	"testing"

	"github.com/iml885203/orbit/daemon"
)

func TestSummarizeDoctorForHumanLeadsWithAttention(t *testing.T) {
	resp := &daemon.DoctorResponse{Checks: []daemon.DoctorCheck{
		{Name: "Config", Status: daemon.CheckInfo, Message: "/workspace/orbit.yaml"},
		{Name: "Docker", Status: daemon.CheckPass, Message: "available"},
		{Name: "Python", Status: daemon.CheckPass, Message: "available"},
		{Name: "Packages (api)", Status: daemon.CheckFail, Message: "not installed", Hint: "run: pip install"},
		{Name: "PATH", Status: daemon.CheckWarn, Message: "another install wins"},
	}}

	got := summarizeDoctorForHuman(resp, true)

	if got.passed != 2 {
		t.Fatalf("passed = %d, want 2", got.passed)
	}
	if len(got.attention) != 2 ||
		got.attention[0].label != "Packages (api)" ||
		got.attention[1].label != "PATH" {
		t.Fatalf("attention = %+v", got.attention)
	}
	if len(got.context) != 0 {
		t.Fatalf("context = %+v", got.context)
	}
}

func TestSummarizeDoctorForHumanHidesInactiveDaemon(t *testing.T) {
	resp := &daemon.DoctorResponse{Checks: []daemon.DoctorCheck{
		{Name: "Config", Status: daemon.CheckPass, Message: "valid"},
		{Name: "Daemon", Status: daemon.CheckInfo, Message: "not running"},
	}}

	got := summarizeDoctorForHuman(resp, true)

	if got.passed != 1 || len(got.attention) != 0 || len(got.context) != 0 {
		t.Fatalf("summary = %+v", got)
	}
}

func TestSummarizeDoctorForHumanKeepsUsefulEnvironmentContext(t *testing.T) {
	resp := &daemon.DoctorResponse{Checks: []daemon.DoctorCheck{
		{
			Name:    "Daemon",
			Status:  daemon.CheckInfo,
			Message: "shop is still active; run commands from /workspace/shop",
			Hint:    "run: orbit up",
		},
	}}

	got := summarizeDoctorForHuman(resp, true)

	if len(got.context) != 1 || got.context[0].label != "Environment" {
		t.Fatalf("context = %+v", got.context)
	}
}
