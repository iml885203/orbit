package sqlpublish

import (
	"context"
	"io"
	"testing"
)

func TestBaselineName(t *testing.T) {
	if got := BaselineName("PaymentDB"); got != "PaymentDB_baseline" {
		t.Errorf("BaselineName = %q", got)
	}
}

// Identifier validation must reject injection-shaped names on every
// snapshot entry point before any connection is attempted.
func TestSnapshotOps_RejectUnsafeNames(t *testing.T) {
	bad := "x]; DROP DATABASE [y"
	opts := Opts{Host: "localhost", Port: 1, User: "sa", Password: "p"}
	if _, err := BaselineExists(context.Background(), opts, bad); err == nil {
		t.Error("BaselineExists accepted unsafe name")
	}
	if err := RefreshBaseline(context.Background(), opts, bad, io.Discard); err == nil {
		t.Error("RefreshBaseline accepted unsafe name")
	}
	if err := RevertToBaseline(context.Background(), opts, bad, io.Discard); err == nil {
		t.Error("RevertToBaseline accepted unsafe name")
	}
}
