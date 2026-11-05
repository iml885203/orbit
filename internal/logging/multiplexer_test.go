package logging

import (
	"strconv"
	"testing"
)

func TestFollowUnderConcurrentWriterHasNoGapOrDuplicate(t *testing.T) {
	m := NewMultiplexer()
	stop := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			m.Write("api", strconv.Itoa(i))
			m.Write("web", "noise")
		}
	}()
	defer func() {
		close(stop)
		<-writerDone
	}()

	const followers = 2000
	for f := 0; f < followers; f++ {
		// Deterministic only while one writer feeds api and each follower
		// reads far fewer lines than live holds: drops then hit later lines.
		live := make(chan string, 64)
		backlog, unsubscribe := m.Follow("api", 2, func(line string) {
			select {
			case live <- line:
			default:
			}
		})
		got := append([]string(nil), backlog...)
		for len(got) < len(backlog)+3 {
			got = append(got, <-live)
		}
		unsubscribe()
		assertConsecutive(t, f, got)
	}
}

func assertConsecutive(t *testing.T, follower int, got []string) {
	t.Helper()
	first, err := strconv.Atoi(got[0])
	if err != nil {
		t.Fatalf("follower %d: line %q is not from api", follower, got[0])
	}
	for i, line := range got {
		if want := strconv.Itoa(first + i); line != want {
			t.Fatalf("follower %d: saw %q, want consecutive lines from %d", follower, got, first)
		}
	}
}

func TestFollowWithoutBacklogOnlySeesLaterLines(t *testing.T) {
	m := NewMultiplexer()
	m.Write("api", "before")

	var live []string
	backlog, unsubscribe := m.Follow("api", 0, func(line string) { live = append(live, line) })
	defer unsubscribe()
	m.Write("api", "after")

	if len(backlog) != 0 {
		t.Errorf("backlog = %q, want none", backlog)
	}
	if len(live) != 1 || live[0] != "after" {
		t.Errorf("live = %q, want [after]", live)
	}
}
