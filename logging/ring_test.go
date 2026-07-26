package logging

import "testing"

func TestRingBuffer_Basic(t *testing.T) {
	rb := NewRingBuffer(5)

	rb.Write("line1")
	rb.Write("line2")
	rb.Write("line3")

	lines := rb.Lines()
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	if lines[0] != "line1" || lines[2] != "line3" {
		t.Errorf("lines = %v", lines)
	}
}

func TestRingBuffer_Overflow(t *testing.T) {
	rb := NewRingBuffer(3)

	rb.Write("a")
	rb.Write("b")
	rb.Write("c")
	rb.Write("d") // overflows, drops "a"
	rb.Write("e") // overflows, drops "b"

	lines := rb.Lines()
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	if lines[0] != "c" || lines[1] != "d" || lines[2] != "e" {
		t.Errorf("expected [c d e], got %v", lines)
	}
}

func TestRingBuffer_Last(t *testing.T) {
	rb := NewRingBuffer(10)
	for i := 0; i < 7; i++ {
		rb.Write("line")
	}

	last3 := rb.Last(3)
	if len(last3) != 3 {
		t.Errorf("Last(3) = %d items, want 3", len(last3))
	}

	last100 := rb.Last(100)
	if len(last100) != 7 {
		t.Errorf("Last(100) = %d items, want 7", len(last100))
	}
}
