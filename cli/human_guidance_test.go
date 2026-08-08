package cli

import (
	"testing"

	"github.com/iml885203/orbit/daemon"
)

func TestHumanGuidanceKeepsTheTargetedInstance(t *testing.T) {
	t.Setenv("ORBIT_INSTANCE", "checkout-a")

	_, _, next := HumanGuidance(daemon.ErrDaemonUnreachable)

	if next != "orbit --instance checkout-a up" {
		t.Fatalf("next command = %q; a person working in checkout-a must not be sent to the default runtime", next)
	}
}

func TestHumanGuidanceDropsTheAgentOnlyJSONSuffix(t *testing.T) {
	_, _, next := HumanGuidance(daemon.ErrDaemonUnreachable)

	if next != "orbit up" {
		t.Fatalf("next command = %q, want %q", next, "orbit up")
	}
}

// The catch-all classification applies to anything unrecognised, so surfacing
// its "run orbit doctor" would attach the same non-advice to every error.
func TestHumanGuidanceStaysSilentOnTheCatchAll(t *testing.T) {
	message, hint, next := HumanGuidance(errUnclassified{})

	if message != "" || hint != "" || next != "" {
		t.Fatalf("unclassified error produced guidance: %q / %q / %q", message, hint, next)
	}
}

type errUnclassified struct{}

func (errUnclassified) Error() string { return "something specific went wrong" }

// A busy daemon is running, so it must not inherit daemon_unreachable's
// "run orbit up" — that names a recovery the user has already performed.
func TestDaemonTimeoutIsNotReportedAsNotRunning(t *testing.T) {
	classified := classify(daemon.ErrDaemonTimeout)

	if classified.Code != "timeout" {
		t.Fatalf("code = %q, want timeout", classified.Code)
	}
	if !classified.Retryable {
		t.Error("a timeout against a running daemon is retryable")
	}
}

func TestRecommendedActionsLeadWithTheSameCommandTheEnvelopeReports(t *testing.T) {
	err := WithJSONActions(
		NewEnvRepoAccessError("cannot access environment repo"),
		[]JSONAction{{Command: "gh auth login", Reason: "Authenticate."}},
	)

	actions := RecommendedActions(err)

	if len(actions) == 0 {
		t.Fatal("no actions")
	}
	// WriteJSONFailure leads with the classification's own action, so the
	// human fallback must too, or the two audiences name different commands.
	if actions[0].Command != "orbit source sync --json" {
		t.Fatalf("actions[0] = %q; diverges from the envelope's recommended_actions[0]", actions[0].Command)
	}
}
