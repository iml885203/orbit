package cli

// state_display.go owns the mapping from service/task state strings to their
// terminal-rendering (Unicode icon + ANSI colour). Single domain: how do we
// show a state in the CLI.

func StateIcon(state string) string {
	switch state {
	case "healthy":
		return Green.Sprint("●")
	case "building":
		return Bold.Sprint("◐")
	case "starting", "reconciling":
		return Yellow.Sprint("◐")
	case "degraded":
		return Red.Sprint("◑")
	case "stopping":
		return Yellow.Sprint("◔")
	case "stopped":
		return Faint.Sprint("○")
	case "pending":
		return Faint.Sprint("○")
	default:
		return "?"
	}
}

func ColorState(state string) string {
	switch state {
	case "healthy", "running", "succeeded":
		return Green.Sprint(state)
	case "building":
		return Bold.Sprint(state)
	case "starting", "pending", "stopping", "reconciling":
		return Yellow.Sprint(state)
	case "degraded", "failed":
		return Red.Sprint(state)
	case "stopped":
		return Faint.Sprint(state)
	default:
		return state
	}
}
