package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
)

// A user raised a service's health_check.retries to 20 minutes, timed out
// anyway, and spent six CI rounds looking at port binding — because the
// message named neither the budget that was actually spent nor the fact that
// it is separate from retries. The job also failed at 503s against a 5m
// budget, which reads as a contradiction until the message reports elapsed
// time: the clock starts at the wait loop, after daemon startup.
func TestWaitTimeoutMessageNamesBudgetAndElapsed(t *testing.T) {
	t.Run("default budget is attributed to the default, not the flag", func(t *testing.T) {
		err := newWaitTimeoutError("resources to become healthy", 0, 5*time.Minute, 503*time.Second)

		for _, want := range []string{
			"resources to become healthy",
			"5m0s",                 // the budget that was actually enforced
			"8m23s",                // elapsed, which does not equal the budget
			"default",              // where that budget came from
			"health_check.retries", // the other budget, named so it is not raised in vain
			"--timeout",            // the way to change the one that expired
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("message = %q, want it to mention %q", err, want)
			}
		}
	})

	t.Run("an explicit flag is attributed to the flag", func(t *testing.T) {
		err := newWaitTimeoutError("resources to become healthy", 25*time.Minute, 25*time.Minute, 25*time.Minute)
		if !strings.Contains(err.Error(), "--timeout") {
			t.Errorf("message = %q, want it to attribute the budget to --timeout", err)
		}
		if strings.Contains(err.Error(), "the default") {
			t.Errorf("message = %q, want it not to claim the default was used", err)
		}
	})

	// `--timeout 5m` produces a budget identical to the default, so a message
	// that infers provenance from the value tells the user to raise a flag
	// they just set — the same "recommend what was already done" failure this
	// message exists to avoid.
	t.Run("an explicit flag equal to the default is still the flag", func(t *testing.T) {
		err := newWaitTimeoutError("resources to become healthy", defaultWaitTimeout, defaultWaitTimeout, 6*time.Minute)
		if strings.Contains(err.Error(), "the default") {
			t.Errorf("message = %q, want it to credit --timeout, not the default", err)
		}
	})
}

// The timeout stays classified as a timeout: the JSON contract routes on
// error.code, and callers key recovery actions off it.
func TestWaitTimeoutErrorStaysClassifiedAsTimeout(t *testing.T) {
	err := newWaitTimeoutError("resources to stop", 0, 5*time.Minute, time.Minute)
	if !strings.Contains(err.Error(), "resources to stop") {
		t.Fatalf("message = %q", err)
	}
	if !errors.Is(err, cli.ErrTimeout) {
		t.Errorf("error = %v, want it to classify as a timeout", err)
	}
}

// A pending resource reported an empty state_reason, which a caller's
// formatter rendered as "web-e2e (pending): undefined". Empty is
// indistinguishable from a resource that started and failed without saying
// why; what it is waiting on is both known and the actual answer.
func TestPendingResourceReportsWhatItIsWaitingOn(t *testing.T) {
	status := &daemon.StatusResponse{Resources: []daemon.ResourceStatus{
		{Name: "web-e2e", State: "pending", PendingDependencies: []string{"api-e2e"}},
		{Name: "api-e2e", State: "building"},
	}}

	data := buildUpFailureJSONData(nil, status, func(string) []string { return nil })

	byName := map[string]upFailedResource{}
	for _, r := range data.FailedResources {
		byName[r.Name] = r
	}
	if got := byName["web-e2e"].StateReason; got != "waiting for api-e2e" {
		t.Errorf("pending state_reason = %q, want it to name the dependency", got)
	}
	// A resource with nothing to wait on must not gain an invented reason.
	if got := byName["api-e2e"].StateReason; got != "" {
		t.Errorf("building state_reason = %q, want it left empty", got)
	}
}
