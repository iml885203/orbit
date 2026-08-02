package instance

import (
	"path/filepath"
	"testing"
)

func TestResolveProvidesStableIsolatedRuntime(t *testing.T) {
	got, err := Resolve("/tmp/orbit", "checkout-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Home != filepath.Join("/tmp/orbit", "instances", "checkout-a") {
		t.Fatalf("home = %q", got.Home)
	}
	if got.Namespace == "" || got.Namespace == "checkout-a" {
		t.Fatalf("namespace = %q", got.Namespace)
	}
	again, _ := Resolve("/tmp/orbit", "checkout-a")
	if again.Namespace != got.Namespace {
		t.Fatalf("namespace is unstable: %q != %q", again.Namespace, got.Namespace)
	}
}

func TestValidateNameRejectsPaths(t *testing.T) {
	for _, name := range []string{"", "../other", "/tmp/x", "white space", "-leading"} {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) succeeded", name)
		}
	}
}
