package cli

import (
	"errors"
	"os"
	"strings"
)

// human_guidance.go owns one question: given a failure, what do we tell the
// person at the terminal? The answers come from classify() and the error's
// own recommended actions — the same vocabulary the --json envelope reports —
// so a human and an agent never receive different advice about the same
// failure. Only the spelling differs, and spellHuman owns that difference.

// HumanGuidance returns the headline, recovery hint, and next command that
// classify already derives for the JSON envelope.
//
// message is set only where classify replaced the raw error text — a
// transport failure reads as "Orbit is not running.", not as the socket path
// and syscall that produced it. Empty means the raw error already reads well.
//
// The catch-all classification deliberately yields nothing: "run orbit
// doctor" on every unrecognised error is noise, and points away from the
// real fix as often as toward it.
func HumanGuidance(err error) (message, hint, nextCommand string) {
	classified := classify(err)
	if classified.Code == "command_failed" {
		return "", "", ""
	}
	if err != nil && classified.Message != err.Error() {
		message = classified.Message
	}
	return message, classified.Hint, spellHuman(classified.NextCommand)
}

// FirstRecommendedCommand returns the leading action an error recommends,
// spelled for a terminal. An error that can name the command an agent should
// run next can name it for a person too.
func FirstRecommendedCommand(err error) string {
	if actions := RecommendedActions(err); len(actions) > 0 {
		return spellHuman(actions[0].Command)
	}
	return ""
}

// spellHuman converts a contract command into one a person can paste. The
// ` --json` suffix is for agents only, but the `--instance` prefix is not:
// dropping it would name the default runtime while the user is working in a
// named one, which is worse advice than none.
func spellHuman(command string) string {
	if command == "" {
		return ""
	}
	if instance := os.Getenv("ORBIT_INSTANCE"); instance != "" {
		command = instanceTargetedCommand(command, instance)
	}
	return strings.TrimSuffix(command, " --json")
}

// RecommendedActions returns the actions an error recommends, in the order
// WriteJSONFailure assembles them: replacement actions supersede the attempted
// command and win outright; otherwise the classification's own action leads
// and the error's additive actions follow.
//
// Mirroring that order is the point — the human fallback and the envelope's
// recommended_actions[0] must name the same command, or the two audiences get
// different advice about the same failure. Callers that pass extra actions
// into WriteJSONFailure are the one case this cannot reproduce; the human
// path never does.
func RecommendedActions(err error) []JSONAction {
	var replacement interface{ CLIJSONReplacementActions() []JSONAction }
	if errors.As(err, &replacement) {
		return replacement.CLIJSONReplacementActions()
	}
	actions := recommendedActionsForError(classify(err))
	var additive interface{ CLIJSONActions() []JSONAction }
	if errors.As(err, &additive) {
		actions = MergeActions(actions, additive.CLIJSONActions())
	}
	return actions
}
