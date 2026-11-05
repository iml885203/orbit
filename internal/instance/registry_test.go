package instance

import (
	"os"
	"path/filepath"
	"testing"
)

// A home with no manifest is what an interrupted clean leaves behind. List and
// clean disagreed about it: List enumerated the directory while clean read the
// manifest first and reported the instance missing, so the name showed up in
// one surface and was unusable in the other. A downstream user accumulated 42
// of them, none removable through orbit.
func TestListSkipsHomesWithoutAManifest(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "instances", "live")
	residue := filepath.Join(base, "instances", "leftover")
	for _, dir := range []string{real, residue} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(real, manifestFile), []byte(`{"config_path":"/tmp/orbit.yaml"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := List(base)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "live" {
		names := make([]string, 0, len(got))
		for _, s := range got {
			names = append(names, s.Name)
		}
		t.Fatalf("List = %v, want only [live]", names)
	}
}

// Only an empty home is swept. A directory with contents belongs to a real
// instance or one mid-creation, and clean calls this after removing state —
// so deleting a non-empty home here would race whoever just wrote to it.
func TestRemoveEmptyHomeLeavesNonEmptyHomes(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "instances", "busy")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "orbit.sock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveEmptyHome(base, "busy")
	if err != nil || removed {
		t.Fatalf("RemoveEmptyHome on non-empty home = (%v, %v), want (false, nil)", removed, err)
	}
	if _, err := os.Stat(home); err != nil {
		t.Errorf("non-empty home was removed: %v", err)
	}
}

// The shell clean's own Activate recreates: empty, and nobody's.
func TestRemoveEmptyHomeSweepsTheShell(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "instances", "swept")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveEmptyHome(base, "swept")
	if err != nil || !removed {
		t.Fatalf("RemoveEmptyHome = (%v, %v), want (true, nil)", removed, err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Errorf("empty home survived: %v", err)
	}
}

func TestRemoveResidueReportsWhetherAnythingWasThere(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "instances", "leftover")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveResidue(base, "leftover")
	if err != nil || !removed {
		t.Fatalf("RemoveResidue = (%v, %v), want (true, nil)", removed, err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Errorf("home still present after RemoveResidue: %v", err)
	}

	// Second call has nothing to do, and must say so rather than claim it
	// cleaned something — that is what lets clean fall through to "does not
	// exist" for a name that truly never existed.
	removed, err = RemoveResidue(base, "leftover")
	if err != nil || removed {
		t.Fatalf("RemoveResidue on absent home = (%v, %v), want (false, nil)", removed, err)
	}
}
