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
