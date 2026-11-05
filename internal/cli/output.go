package cli

// JSONAlreadyRenderedError preserves a command's failure exit status after an
// extension has written its own machine-readable error contract.
type JSONAlreadyRenderedError struct {
	Err error
}

func (e JSONAlreadyRenderedError) Error() string           { return e.Err.Error() }
func (e JSONAlreadyRenderedError) Unwrap() error           { return e.Err }
func (e JSONAlreadyRenderedError) CLIJSONAlreadyRendered() {}

// MarkJSONErrorRendered prevents the root command from printing a second error.
func MarkJSONErrorRendered(err error) error {
	return JSONAlreadyRenderedError{Err: err}
}

// JSONOutput mirrors the root command's --json flag. cmd/orbit binds the
// pflag to it before any RunE executes; commands (including extension
// commands) read it afterwards, so no lock is needed. Tests that flip it
// must restore the previous value.
var JSONOutput bool
