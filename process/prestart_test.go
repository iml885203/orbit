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

func TestPreStartStreamsOneCoherentLifecycle(t *testing.T) {
	t.Parallel()
	skipOnWindows(t)
	m := NewManager()

	var mu sync.Mutex
	var got []string
	var actions []string
	firstOutput := make(chan struct{})
	var signalFirstOutput sync.Once
	m.OnOutput = func(name, line string) {
		mu.Lock()
		got = append(got, line)
		mu.Unlock()
		if line == "first" {
			signalFirstOutput.Do(func() { close(firstOutput) })
		}
	}
	m.OnAction = func(name, message string) {
		mu.Lock()
		defer mu.Unlock()
		actions = append(actions, message)
	}

	gate := filepath.Join(t.TempDir(), "continue")
	first := writePreStartScript(t, "echo first\nwhile [ ! -f \""+gate+"\" ]; do sleep 0.01; done\necho second")
	second := writePreStartScript(t, "echo third")
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

	select {
	case <-firstOutput:
	case <-time.After(10 * time.Second):
		t.Fatal("first pre_start output was buffered until command completion")
	}
	if err := os.WriteFile(gate, nil, 0o644); err != nil {
		t.Fatalf("release pre_start gate: %v", err)
	}
	if err := <-started; err != nil {
		t.Fatalf("Start: %v", err)
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
