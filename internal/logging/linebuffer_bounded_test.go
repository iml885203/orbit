package logging

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundedLineBufferKeepsUnterminatedStreamBounded(t *testing.T) {
	const limit = 64 * 1024
	emitted := 0
	buffer := NewBoundedLineBuffer(limit, func(line string) {
		if !strings.HasSuffix(line, continuedLineMarker) {
			t.Errorf("missing continuation marker")
		}
		if len(strings.TrimSuffix(line, continuedLineMarker)) > limit {
			t.Error("oversized content chunk")
		}
		emitted++
	})
	chunk := bytes.Repeat([]byte{'x'}, 4096)
	for range 2048 {
		n, err := buffer.Write(chunk)
		if err != nil || n != len(chunk) {
			t.Fatalf("write=%d %v", n, err)
		}
		if buffer.buf.Len() > limit || buffer.buf.Cap() > 4*limit {
			t.Fatalf("unbounded parser: len=%d cap=%d", buffer.buf.Len(), buffer.buf.Cap())
		}
	}
	if emitted == 0 {
		t.Fatal("unterminated output never drained")
	}
}

func TestBoundedLineBufferPreservesUTF8AndTrailingContent(t *testing.T) {
	input := strings.Repeat("a界🙂", 31) + "tail"
	var lines []string
	buffer := NewBoundedLineBuffer(13, func(line string) {
		if !utf8.ValidString(line) {
			t.Errorf("split UTF-8 rune: %q", line)
		}
		lines = append(lines, strings.TrimSuffix(line, continuedLineMarker))
	})
	for _, b := range []byte(input) {
		_, _ = buffer.Write([]byte{b})
	}
	buffer.Flush()
	if got := strings.Join(lines, ""); got != input {
		t.Fatalf("lost output: %q", got)
	}
}

func TestBoundedLineBufferKeepsNormalCRLFLines(t *testing.T) {
	var lines []string
	buffer := NewBoundedLineBuffer(4, func(line string) { lines = append(lines, line) })
	_, _ = buffer.Write([]byte("abcd\r"))
	_, _ = buffer.Write([]byte("\nnext\nlast"))
	buffer.Flush()
	if got := strings.Join(lines, "|"); got != "abcd|next|last" {
		t.Fatal(got)
	}
}

func TestBoundedLineBufferMakesProgressWithInvalidUTF8(t *testing.T) {
	buffer := NewBoundedLineBuffer(0, nil)
	_, _ = buffer.Write(bytes.Repeat([]byte{0x80}, 64))
	if buffer.buf.Len() > utf8.UTFMax {
		t.Fatal("invalid byte stream remained buffered")
	}
	buffer.Flush()
}
