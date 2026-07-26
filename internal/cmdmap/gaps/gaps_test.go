package gaps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTrackDedup(t *testing.T) {
	dir := t.TempDir()
	c := New(filepath.Join(dir, "gaps.json"))

	c.Track("PUT", "/api/settings", "update settings")
	c.Track("PUT", "/api/settings", "update settings")
	c.Track("PUT", "/api/edges/:from/:to", "detach edge")

	gaps := c.List()
	if len(gaps) != 2 {
		t.Fatalf("want 2 gaps, got %d: %+v", len(gaps), gaps)
	}
	for _, g := range gaps {
		if g.Method == "PUT" && g.PathPattern == "/api/settings" && g.Count != 2 {
			t.Fatalf("settings count: want 2, got %d", g.Count)
		}
	}
	if err := c.Flush(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "gaps.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out []Gap
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("file: want 2, got %d", len(out))
	}
}
