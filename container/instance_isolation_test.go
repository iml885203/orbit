package container

import (
	"reflect"
	"testing"
)

func TestNamespaceVolumeBindsIsolatesNamedVolumesOnly(t *testing.T) {
	binds := []string{"data:/var/lib/data", "/host/config:/etc/config", "./src:/app"}
	want := []string{"orbit-instance-a-data:/var/lib/data", "/host/config:/etc/config", "./src:/app"}
	if got := namespaceVolumeBinds("instance-a", binds); !reflect.DeepEqual(got, want) {
		t.Fatalf("binds = %#v, want %#v", got, want)
	}
	if got := namespaceVolumeBinds("", binds); !reflect.DeepEqual(got, binds) {
		t.Fatalf("default binds changed: %#v", got)
	}
}

func TestNetworkNameKeepsDefaultBackwardCompatible(t *testing.T) {
	if got := NetworkName(""); got != "orbit" {
		t.Fatalf("default network = %q", got)
	}
	if got := NetworkName("instance-a"); got != "orbit-instance-a" {
		t.Fatalf("named network = %q", got)
	}
}
