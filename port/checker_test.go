package port

import (
	"fmt"
	"net"
	"testing"
)

func TestFindFree_ReturnsBindablePort(t *testing.T) {
	p, err := FindFree()
	if err != nil {
		t.Fatalf("FindFree returned error: %v", err)
	}
	if p <= 0 {
		t.Fatalf("FindFree returned %d", p)
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", p))
	if err != nil {
		t.Fatalf("returned port %d not bindable: %v", p, err)
	}
	_ = ln.Close()
}

func TestCheckPortsDetectsIPv4LoopbackListener(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	portNumber := listener.Addr().(*net.TCPAddr).Port

	conflicts := CheckPorts(map[string][]int{"api": {portNumber}})
	if len(conflicts) != 1 || conflicts[0].Port != portNumber || conflicts[0].Service != "api" {
		t.Fatalf("conflicts = %+v", conflicts)
	}
}
