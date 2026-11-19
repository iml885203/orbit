package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/logging"
)

type dotnetOutputFixture struct {
	*App
	final sync.Map
}

func outputTestDotnet(t *testing.T) *dotnetOutputFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake dotnet executable uses a POSIX shell")
	}
	bin := t.TempDir()
	script := `#!/bin/sh
case "$2" in
 stderr-pressure|mixed-pressure)
  i=0
  while [ "$i" -lt 20000 ]; do
   printf 'stderr diagnostic %s\n' "$i" >&2
   if [ "$2" = mixed-pressure ]; then printf 'stdout diagnostic %s\n' "$i"; fi
   i=$((i+1))
  done
  ;;
 stdout-longline|stderr-longline|stdout-unterminated|stderr-unterminated)
  i=0
  while [ "$i" -lt 12000 ]; do
   if [ "$2" = stdout-longline ] || [ "$2" = stdout-unterminated ]; then printf 'abcdefghijklmno'; else printf 'abcdefghijklmno' >&2; fi
   i=$((i+1))
  done
  case "$2" in *-longline) printf '\n'; printf '\n' >&2;; esac
  ;;
 cancel)
  printf started > "$ORBIT_OUTPUT_TEST_STARTED"
  exec sleep 30
  ;;
 inherited-pipe)
  sleep 30 &
  printf '%s' "$!" > "$ORBIT_OUTPUT_TEST_CHILD"
  ;;
esac
printf 'stdout final'
printf 'stderr final' >&2
if [ "$2" = failure ]; then exit 7; fi
`
	if err := os.WriteFile(filepath.Join(bin, "dotnet"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	fixture := &dotnetOutputFixture{App: &App{Logs: logging.NewMultiplexer(), dotnetBuilds: newDotnetBuildGate()}}
	unsubscribe := fixture.Logs.Subscribe(func(service, line string) {
		for _, marker := range []string{"stdout final", "stderr final"} {
			if strings.HasSuffix(line, marker) {
				fixture.final.Store(service+":"+marker, true)
			}
		}
	})
	t.Cleanup(unsubscribe)
	return fixture
}

func requireBuildFinalOutput(t *testing.T, app *dotnetOutputFixture, name string) {
	t.Helper()
	for _, line := range []string{"stdout final", "stderr final"} {
		if _, ok := app.final.Load(name + ":" + line); !ok {
			t.Errorf("missing %s diagnostics for %s", line, name)
		}
	}
}

func TestDotnetBuildDrainsBothStreamsAndLongLines(t *testing.T) {
	for _, project := range []string{"stderr-pressure", "mixed-pressure", "stdout-longline", "stderr-longline", "stdout-unterminated", "stderr-unterminated"} {
		t.Run(project, func(t *testing.T) {
			app := outputTestDotnet(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := app.buildDotnetProject(ctx, project, t.TempDir(), project, os.Environ()); err != nil {
				t.Fatal(err)
			}
			requireBuildFinalOutput(t, app, project)
		})
	}
}

func TestDotnetBuildFailureFlushesOutputAndReleasesGate(t *testing.T) {
	app := outputTestDotnet(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.buildDotnetProject(ctx, "failed", t.TempDir(), "failure", os.Environ()); err == nil {
		t.Fatal("failed build succeeded")
	}
	requireBuildFinalOutput(t, app, "failed")
	if err := app.buildDotnetProject(ctx, "next", t.TempDir(), "quick", os.Environ()); err != nil {
		t.Fatal(err)
	}
	requireBuildFinalOutput(t, app, "next")
}

func TestDotnetBuildCancellationReleasesGate(t *testing.T) {
	app := outputTestDotnet(t)
	started := filepath.Join(t.TempDir(), "started")
	t.Setenv("ORBIT_OUTPUT_TEST_STARTED", started)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.buildDotnetProject(ctx, "canceled", t.TempDir(), "cancel", os.Environ()) }()
	waitForPath(t, started)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled build succeeded")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("canceled build kept output readers or gate blocked")
	}
	nextCtx, nextCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer nextCancel()
	if err := app.buildDotnetProject(nextCtx, "next", t.TempDir(), "quick", os.Environ()); err != nil {
		t.Fatal(err)
	}
	requireBuildFinalOutput(t, app, "next")
}

func TestDotnetBuildDoesNotWaitForeverForInheritedPipes(t *testing.T) {
	app := outputTestDotnet(t)
	childPath := filepath.Join(t.TempDir(), "child")
	t.Setenv("ORBIT_OUTPUT_TEST_CHILD", childPath)
	t.Cleanup(func() {
		data, err := os.ReadFile(childPath)
		if err != nil {
			t.Error(err)
			return
		}
		pid, err := strconv.Atoi(string(data))
		if err != nil {
			t.Error(err)
			return
		}
		child, err := os.FindProcess(pid)
		if err == nil {
			_ = child.Kill()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	err := app.buildDotnetProject(ctx, "inherited", t.TempDir(), "inherited-pipe", os.Environ())
	if err == nil {
		t.Fatal("inherited output descriptors were silently abandoned")
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("inherited pipes exceeded drain window: %s", elapsed)
	}
	nextCtx, nextCancel := context.WithTimeout(context.Background(), time.Second)
	defer nextCancel()
	if err := app.buildDotnetProject(nextCtx, "next", t.TempDir(), "quick", os.Environ()); err != nil {
		t.Fatal(err)
	}
}
