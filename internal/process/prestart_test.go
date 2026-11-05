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

// writePreStartScript returns a pre_start command that runs body.
//
// It returns `/bin/sh <path>` rather than the script path alone, so the exec
// target is a binary that has existed since boot instead of a file this process
// just created. Exec'ing a just-written file races with any concurrent write in
// the same process: `open` deliberately does not take syscall.ForkLock (it can
// block arbitrarily), so a sibling test's fork can inherit the writer's
// descriptor and the exec then fails with ETXTBSY — "text file busy". These
// tests run in parallel and one of them writes a gate file from an output
// callback, which is exactly that shape, and it flaked on Linux CI.
func writePreStartScript(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pre.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return "/bin/sh " + path
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

// bufferedPreStartGrace bounds the deadlock a buffered implementation would
// otherwise cause: it delivers no output until the script exits, and the script
// waits on a gate that only delivered output opens. Only a failing
// implementation ever waits this long — a streaming one opens the gate the
// moment its first line lands — so this is generous on purpose. It replaces a
// 10s bound that doubled as the assertion and flaked on loaded CI runners.
const bufferedPreStartGrace = 60 * time.Second

func TestPreStartStreamsOneCoherentLifecycle(t *testing.T) {
	t.Parallel()
	skipOnWindows(t)
	m := NewManager()

	var mu sync.Mutex
	var got []string
	var actions []string

	gate := filepath.Join(t.TempDir(), "continue")
	openGate := func() {
		if err := os.WriteFile(gate, nil, 0o644); err != nil {
			t.Errorf("open pre_start gate: %v", err)
		}
	}
	// Whoever opens the gate first decides the verdict, and sync.Once picks
	// exactly one winner. Streaming means the callback opens it; buffering
	// means the grace timer does, leaving streamedWhileRunning unclosed. The
	// test then asserts an ordering that already happened rather than racing a
	// deadline.
	var gateOpened sync.Once
	streamedWhileRunning := make(chan struct{})

	m.OnOutput = func(name, line string) {
		mu.Lock()
		got = append(got, line)
		mu.Unlock()
		if line != "first" {
			return
		}
		gateOpened.Do(func() {
			close(streamedWhileRunning)
			openGate()
		})
	}
	m.OnAction = func(name, message string) {
		mu.Lock()
		defer mu.Unlock()
		actions = append(actions, message)
	}

	first := writePreStartScript(t, "echo first\nwhile [ ! -f \""+gate+"\" ]; do sleep 0.01; done\necho second")
	second := writePreStartScript(t, "echo third")
	// Never leave `sleep 30` behind for the rest of the package to compete
	// with, however this test exits.
	t.Cleanup(func() { _ = m.Stop("svc", time.Second) })
	started := make(chan error, 1)
	go func() {
		started <- m.Start(
			context.Background(),
			"svc",
			".",
			"sleep 30",
			nil,
			[]string{first, second},
			0,
		)
	}()

	grace := time.AfterFunc(bufferedPreStartGrace, func() { gateOpened.Do(openGate) })
	defer grace.Stop()

	if err := <-started; err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-streamedWhileRunning:
	default:
		t.Fatal("pre_start output was buffered until the script exited")
	}
	if err := m.Stop("svc", 200*time.Millisecond); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{
		"[pre_start] $ " + first,
		"first",
		"second",
		"[pre_start] exit 0",
		"[pre_start] $ " + second,
		"third",
		"[pre_start] exit 0",
	}
	if !containsInOrder(&got, want) {
		t.Fatalf("log lines missing or out of order.\nwant (in order): %v\ngot: %v", want, got)
	}
	if !containsInOrder(&actions, []string{
		"running pre_start: " + first,
		"pre_start ok: " + first,
		"running pre_start: " + second,
		"pre_start ok: " + second,
	}) {
		t.Fatalf("lifecycle narration missing or out of order: %v", actions)
	}
}

func TestPreStartFailureStopsLifecycle(t *testing.T) {
	t.Parallel()
	skipOnWindows(t)
	m := NewManager()
	got, mu := collectOutput(m)

	script := writePreStartScript(t, "echo before\nexit 7")

	err := m.Start(
		context.Background(),
		"svc",
		".",
		"sleep 30",
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
