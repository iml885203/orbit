package daemon

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckOrbitPathRequiresRunningBinaryToWin(t *testing.T) {
	check := checkOrbitPath("/active/orbit", "/other/orbit", nil)
	if check.Status != CheckWarn ||
		!strings.Contains(check.Message, filepath.Clean("/other/orbit")) ||
		!strings.Contains(check.Hint, filepath.Clean("/active")) {
		t.Fatalf("check = %+v", check)
	}
}

func TestCheckOrbitPathPassesForRunningBinary(t *testing.T) {
	check := checkOrbitPath("/active/orbit", "/active/orbit", nil)
	if check.Status != CheckPass {
		t.Fatalf("check = %+v", check)
	}
}

func TestCheckOrbitPathWarnsWhenOrbitIsMissing(t *testing.T) {
	check := checkOrbitPath("/active/orbit", "", errors.New("not found"))
	if check.Status != CheckWarn || !strings.Contains(check.Hint, filepath.Clean("/active")) {
		t.Fatalf("check = %+v", check)
	}
}
