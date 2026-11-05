package daemon

import (
	"strings"
	"testing"
)

func TestValidateSocketPath_ShortPathOK(t *testing.T) {
	if err := ValidateSocketPath("/tmp/orbit-lab/orbit.sock"); err != nil {
		t.Errorf("short path rejected: %v", err)
	}
}

func TestValidateSocketPath_OverlongPathExplainsLimit(t *testing.T) {
	long := "/tmp/" + strings.Repeat("a", 200) + "/orbit.sock"
	err := ValidateSocketPath(long)
	if err == nil {
		t.Fatal("overlong path accepted — bind would fail with an opaque EINVAL")
	}
	if !strings.Contains(err.Error(), "ORBIT_HOME") {
		t.Errorf("error %q does not tell the user to shorten ORBIT_HOME", err)
	}
}

func TestValidateSocketPath_AtLimitRejected(t *testing.T) {
	// The limit includes the trailing NUL, so len == limit must fail too.
	at := strings.Repeat("a", sunPathLimit())
	if err := ValidateSocketPath(at); err == nil {
		t.Error("path at sun_path limit accepted, want rejection (no room for NUL)")
	}
	under := strings.Repeat("a", sunPathLimit()-1)
	if err := ValidateSocketPath(under); err != nil {
		t.Errorf("path one byte under limit rejected: %v", err)
	}
}
