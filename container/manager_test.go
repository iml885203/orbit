package container

import (
	"bytes"
	"encoding/binary"
	"strings"
	"sync"
	"testing"

	"github.com/iml885203/orbit/logging"
	"github.com/moby/moby/api/pkg/stdcopy"
)

// frame builds a Docker multiplexed log frame (stream=1 stdout, 2 stderr).
func frame(stream byte, payload string) []byte {
	out := make([]byte, 8, 8+len(payload))
	out[0] = stream
	binary.BigEndian.PutUint32(out[4:8], uint32(len(payload)))
	return append(out, []byte(payload)...)
}

// decodePipeline runs the exact stack that streamLogs uses:
// stdcopy demux → LineBuffer line assembly → Multiplexer.
func decodePipeline(t *testing.T, framed []byte) []string {
	t.Helper()
	mux := logging.NewMultiplexer()

	var mu sync.Mutex
	var got []string
	unsub := mux.Subscribe(func(_ string, line string) {
		mu.Lock()
		got = append(got, line)
		mu.Unlock()
	})
	defer unsub()

	emit := func(line string) { mux.Write("svc", line) }
	stdoutLB := logging.NewLineBuffer(emit)
	stderrLB := logging.NewLineBuffer(emit)
	_, _ = stdcopy.StdCopy(stdoutLB, stderrLB, bytes.NewReader(framed))
	stdoutLB.Flush()
	stderrLB.Flush()

	return got
}

func TestDecodePipeline_SingleLine(t *testing.T) {
	got := decodePipeline(t, frame(1, "hello\n"))
	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestDecodePipeline_MultipleFramesPerRead(t *testing.T) {
	combined := append(frame(1, "first\n"), frame(1, "second\n")...)
	got := decodePipeline(t, combined)
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Errorf("got %q", got)
	}
}

func TestDecodePipeline_LongPayload(t *testing.T) {
	line := strings.Repeat("X", 5000)
	got := decodePipeline(t, frame(1, line+"\n"))
	if len(got) != 1 || got[0] != line {
		t.Errorf("got %d lines, first len=%d (want 1 line, len=5000)", len(got), len(got[0]))
	}
}

func TestDecodePipeline_LineSplitAcrossFrames(t *testing.T) {
	buf := bytes.Buffer{}
	buf.Write(frame(1, "par"))
	buf.Write(frame(1, "t\n"))
	got := decodePipeline(t, buf.Bytes())
	if len(got) != 1 || got[0] != "part" {
		t.Errorf("got %q, want one line 'part'", got)
	}
}

func TestDecodePipeline_StdoutAndStderrInterleaved(t *testing.T) {
	buf := bytes.Buffer{}
	buf.Write(frame(1, "out-a\n"))
	buf.Write(frame(2, "err-a\n"))
	buf.Write(frame(1, "out-b\n"))
	got := decodePipeline(t, buf.Bytes())
	want := []string{"out-a", "err-a", "out-b"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDecodePipeline_TrailingPayloadWithoutNewline(t *testing.T) {
	got := decodePipeline(t, frame(1, "unterminated"))
	if len(got) != 1 || got[0] != "unterminated" {
		t.Errorf("got %q", got)
	}
}
