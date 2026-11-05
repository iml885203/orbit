package cli

import "github.com/fatih/color"

// Shared terminal palette for human-readable command output. Exported as
// variables (not funcs) so render tests can swap one out to force a
// deterministic style. Mutation is test-only: production code never
// writes them, and a test that swaps one must restore the previous value
// in t.Cleanup.
var (
	Green  = color.New(color.FgGreen)
	Yellow = color.New(color.FgYellow)
	Red    = color.New(color.FgRed)
	Faint  = color.New(color.Faint)
	Bold   = color.New(color.Bold)
)
