package daemon

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDoctorCheckHintJSON(t *testing.T) {
	c := DoctorCheck{Name: "x", Status: CheckFail, Message: "m", Hint: "do the thing"}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"hint":"do the thing"`) {
		t.Errorf("missing hint: %s", b)
	}
	cpass := DoctorCheck{Name: "x", Status: CheckPass, Message: "m"}
	bp, _ := json.Marshal(cpass)
	if strings.Contains(string(bp), "hint") {
		t.Errorf("hint should be omitted when empty: %s", bp)
	}
}
