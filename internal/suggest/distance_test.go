package suggest

import "testing"

func TestDistanceTreatsAdjacentTypoAsOneEdit(t *testing.T) {
	if got := Distance("aip", "api"); got != 1 {
		t.Fatalf("Distance(aip, api) = %d, want 1", got)
	}
}
