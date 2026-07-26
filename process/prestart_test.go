package process

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEmitLine_CallsOnOutput(t *testing.T) {
	m := NewManager()
	var mu sync.Mutex
	var got []string
	m.OnOutput = func(name, line string) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, name+"|"+line)
	}

	m.emitLine("svc", "hello")

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "svc|hello" {
		t.Fatalf("expected one line 'svc|hello', got %v", got)
	}
}

func TestEmitLine_NilCallbackIsNoOp(t *testing.T) {
	m := NewManager()
	// OnOutput is nil; must not panic.
	m.emitLine("svc", "hello")
}

// collectOutput wires m.OnOutput to a slice and returns the slice + its mutex.
// Callers must lock the mutex before reading.
func collectOutput(m *Manager) (*[]string, *sync.Mutex) {
	mu := &sync.Mutex{}
	lines := make([]string, 0)
	m.OnOutput = func(name, line string) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}
	return &lines, mu
}

func writePreStartScript(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pre.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

// containsInOrder reports whether want is a subsequence of *got
// (i.e. each want[i] appears in some line of *got, in order, possibly with other lines between).
func containsInOrder(got *[]string, want []string) bool {
	i := 0
	for _, line := range *got {
		if i < len(want) && strings.Contains(line, want[i]) {
			i++
		}
	}
	return i == len(want)
}

func TestPreStart_StreamsOutputToServiceLog(t *testing.T) {
	skipOnWindows(t)
	m := NewManager()
	got, mu := collectOutput(m)

	script := writePreStartScript(t, "echo hello\necho world")

	err := m.Start(
		context.Background(),
		"svc",
		".",
		"sleep 0.1",
		nil,
		[]string{script},
		0,
	)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Stop the main process; we only care about pre_start lines being captured.
	_ = m.Stop("svc", 200*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	want := []string{
		"[pre_start] $ " + script,
		"hello",
		"world",
		"[pre_start] exit 0",
	}
	if !containsInOrder(got, want) {
		t.Fatalf("log lines missing or out of order.\nwant (in order): %v\ngot: %v", want, *got)
	}
}

func TestPreStart_StreamsInRealTime(t *testing.T) {
	skipOnWindows(t)
	m := NewManager()

	mu := &sync.Mutex{}
	type stamped struct {
		line string
		at   time.Time
	}
	var got []stamped
	m.OnOutput = func(name, line string) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, stamped{line: line, at: time.Now()})
	}

	script := writePreStartScript(t, "echo a\nsleep 0.3\necho b")

	if err := m.Start(
		context.Background(),
		"svc",
		".",
		"sleep 0.1",
		nil,
		[]string{script},
		0,
	); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = m.Stop("svc", 200*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	var aAt, bAt time.Time
	for _, s := range got {
		if s.line == "a" && aAt.IsZero() {
			aAt = s.at
		}
		if s.line == "b" && bAt.IsZero() {
			bAt = s.at
		}
	}
	if aAt.IsZero() || bAt.IsZero() {
		t.Fatalf("did not observe both lines: %v", got)
	}
	gap := bAt.Sub(aAt)
	if gap < 200*time.Millisecond {
		t.Fatalf("expected b to arrive >=200ms after a (got %s); output was buffered, not streamed", gap)
	}
}

func TestPreStart_FailureWritesExitLine(t *testing.T) {
	skipOnWindows(t)
	m := NewManager()
	got, mu := collectOutput(m)

	script := writePreStartScript(t, "echo before\nexit 7")

	err := m.Start(
		context.Background(),
		"svc",
		".",
		"sleep 30", // would run forever if reached
		nil,
		[]string{script},
		0,
	)
	if err == nil {
		t.Fatal("Start should fail when pre_start exits non-zero")
	}
	if m.IsRunning("svc") {
		t.Error("service should not be registered when pre_start fails")
	}

	mu.Lock()
	defer mu.Unlock()

	want := []string{
		"[pre_start] $ " + script,
		"before",
		"[pre_start] exit 7",
	}
	if !containsInOrder(got, want) {
		t.Fatalf("log lines missing or out of order.\nwant (in order): %v\ngot: %v", want, *got)
	}
}

func TestPreStart_MultipleCommandsRunInOrder(t *testing.T) {
	skipOnWindows(t)
	m := NewManager()
	got, mu := collectOutput(m)

	one := writePreStartScript(t, "echo one")
	two := writePreStartScript(t, "echo two")

	if err := m.Start(
		context.Background(),
		"svc",
		".",
		"sleep 0.1",
		nil,
		[]string{one, two},
		0,
	); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = m.Stop("svc", 200*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	want := []string{
		"[pre_start] $ " + one,
		"one",
		"[pre_start] exit 0",
		"[pre_start] $ " + two,
		"two",
		"[pre_start] exit 0",
	}
	if !containsInOrder(got, want) {
		t.Fatalf("log lines missing or out of order.\nwant (in order): %v\ngot: %v", want, *got)
	}
}

func TestPreStart_NarrateEventsEmitted(t *testing.T) {
	skipOnWindows(t)
	m := NewManager()

	var amu sync.Mutex
	var actions []string
	m.OnAction = func(name, msg string) {
		amu.Lock()
		defer amu.Unlock()
		actions = append(actions, msg)
	}

	script := writePreStartScript(t, "echo ok")

	if err := m.Start(
		context.Background(),
		"svc",
		".",
		"sleep 0.1",
		nil,
		[]string{script},
		0,
	); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = m.Stop("svc", 200*time.Millisecond)

	amu.Lock()
	defer amu.Unlock()

	var sawRunning, sawOK bool
	for _, msg := range actions {
		if strings.Contains(msg, "running pre_start") {
			sawRunning = true
		}
		if strings.Contains(msg, "pre_start ok") {
			sawOK = true
		}
	}
	if !sawRunning || !sawOK {
		t.Fatalf("expected narrate events 'running pre_start' and 'pre_start ok'; got %v", actions)
	}
}
