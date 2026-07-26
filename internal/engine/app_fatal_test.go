package engine

import (
	"errors"
	"testing"
)

func TestSignalFatal_InvokesOnFatal(t *testing.T) {
	a := &App{}
	var got error
	a.OnFatal = func(err error) { got = err }

	boom := errors.New("orchestrator blew up")
	a.signalFatal(boom)

	if !errors.Is(got, boom) {
		t.Fatalf("OnFatal not invoked with %v, got %v", boom, got)
	}
}
