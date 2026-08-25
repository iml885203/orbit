package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/logging"
	"github.com/iml885203/orbit/process"
)

func TestDotnetBuildGateSerializesAndReleases(t *testing.T) {
	gate := newDotnetBuildGate()
	releaseFirst, err := gate.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan func(), 1)
	go func() {
		release, err := gate.acquire(context.Background())
		if err == nil {
			acquired <- release
		}
	}()

	select {
	case release := <-acquired:
		release()
		t.Fatal("second build acquired before first released")
	case <-time.After(50 * time.Millisecond):
	}

	releaseFirst()
	select {
	case release := <-acquired:
		release()
	case <-time.After(time.Second):
		t.Fatal("second build remained blocked after release")
	}
}

func TestDotnetBuildGateCanceledWaiterDoesNotConsumeSlot(t *testing.T) {
	gate := newDotnetBuildGate()
	release, err := gate.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gate.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire error = %v, want context canceled", err)
	}
	release()

	nextCtx, nextCancel := context.WithTimeout(context.Background(), time.Second)
	defer nextCancel()
	nextRelease, err := gate.acquire(nextCtx)
	if err != nil {
		t.Fatalf("canceled waiter consumed slot: %v", err)
	}
	nextRelease()
}

func TestDotnetServiceBuildsDoNotOverlap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake dotnet executable uses a POSIX shell")
	}
	app, cfg, services, probe := fakeDotnetServiceApp(t)

	errs := make(chan error, len(services))
	var wg sync.WaitGroup
	for name, service := range services {
		wg.Add(1)
		go func(name string, service *config.Service) {
			defer wg.Done()
			errs <- app.Orchestrator.OnStartProcess(context.Background(), name, 1, cfg, service)
		}(name, service)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("start process: %v", err)
		}
	}
	if _, err := os.Stat(probe + ".overlap"); err == nil {
		t.Fatal("dotnet builds overlapped")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	waitForFileLines(t, os.Getenv("ORBIT_TEST_RUN_ARGS"), len(services))
	runArgs, err := os.ReadFile(os.Getenv("ORBIT_TEST_RUN_ARGS"))
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range services {
		want := filepath.Join(filepath.Dir(service.Path), "bin", "Debug", "net8.0", strings.TrimSuffix(filepath.Base(service.Path), ".csproj")+".dll")
		if !strings.Contains(string(runArgs), want+"\n") {
			t.Errorf("runtime args %q do not contain quoted target path %q", runArgs, want)
		}
	}
}

func TestDotnetBuildFailureReleasesNextService(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake dotnet executable uses a POSIX shell")
	}
	app, cfg, services, _ := fakeDotnetServiceApp(t)
	t.Setenv("ORBIT_TEST_FAIL_PROJECT", "first.csproj")

	if err := app.Orchestrator.OnStartProcess(context.Background(), "first", 1, cfg, services["first"]); err == nil {
		t.Fatal("first build unexpectedly succeeded")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.Orchestrator.OnStartProcess(ctx, "second", 1, cfg, services["second"]); err != nil {
		t.Fatalf("second build remained blocked after failure: %v", err)
	}
}

func TestDotnetServiceCanceledWhileWaitingDoesNotBuildOrRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake dotnet executable uses a POSIX shell")
	}
	app, cfg, services, _ := fakeDotnetServiceApp(t)
	started := filepath.Join(t.TempDir(), "started")
	releaseBuild := filepath.Join(t.TempDir(), "release")
	t.Setenv("ORBIT_TEST_BLOCK_PROJECT", "first.csproj")
	t.Setenv("ORBIT_TEST_BUILD_STARTED", started)
	t.Setenv("ORBIT_TEST_RELEASE_BUILD", releaseBuild)

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- app.Orchestrator.OnStartProcess(context.Background(), "first", 1, cfg, services["first"])
	}()
	waitForPath(t, started)

	ctx, cancel := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- app.Orchestrator.OnStartProcess(ctx, "second", 1, cfg, services["second"])
	}()
	cancel()
	if err := <-secondDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting build error = %v, want context canceled", err)
	}
	setServiceState(app.Orchestrator, "second", StateStopped, 1)
	if err := os.WriteFile(releaseBuild, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	buildCalls, err := os.ReadFile(os.Getenv("ORBIT_TEST_BUILD_CALLS"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(buildCalls), "second.csproj") {
		t.Fatalf("canceled waiter launched build: %s", buildCalls)
	}
	if runArgs, err := os.ReadFile(os.Getenv("ORBIT_TEST_RUN_ARGS")); err == nil && strings.Contains(string(runArgs), "second.dll") {
		t.Fatalf("canceled waiter launched service process: %s", runArgs)
	}
	drainOrchestratorEvents(app.Orchestrator)
	if info, _ := app.Orchestrator.GetServiceInfo("second"); info.State != StateStopped {
		t.Fatalf("late build events changed canceled waiter to %s", info.State)
	}
}

func TestDotnetServiceActiveBuildCancellationReleasesNextService(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake dotnet executable uses a POSIX shell")
	}
	app, cfg, services, _ := fakeDotnetServiceApp(t)
	started := filepath.Join(t.TempDir(), "started")
	t.Setenv("ORBIT_TEST_SLEEP_PROJECT", "first.csproj")
	t.Setenv("ORBIT_TEST_BUILD_STARTED", started)

	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- app.Orchestrator.OnStartProcess(ctx, "first", 1, cfg, services["first"])
	}()
	waitForPath(t, started)
	cancel()
	if err := <-firstDone; err == nil {
		t.Fatal("active canceled build unexpectedly succeeded")
	}
	setServiceState(app.Orchestrator, "first", StateStopped, 1)
	t.Setenv("ORBIT_TEST_SLEEP_PROJECT", "")
	nextCtx, nextCancel := context.WithTimeout(context.Background(), time.Second)
	defer nextCancel()
	if err := app.Orchestrator.OnStartProcess(nextCtx, "second", 1, cfg, services["second"]); err != nil {
		t.Fatalf("next service remained blocked after active cancellation: %v", err)
	}
	runArgs := os.Getenv("ORBIT_TEST_RUN_ARGS")
	waitForFileLines(t, runArgs, 1)
	data, err := os.ReadFile(runArgs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "first.dll") {
		t.Fatalf("canceled build launched service process: %s", data)
	}
	drainOrchestratorEvents(app.Orchestrator)
	if info, _ := app.Orchestrator.GetServiceInfo("first"); info.State != StateStopped {
		t.Fatalf("late active-build events changed canceled service to %s", info.State)
	}
}

func fakeDotnetServiceApp(t *testing.T) (*App, *config.Config, map[string]*config.Service, string) {
	t.Helper()
	bin := t.TempDir()
	probe := filepath.Join(t.TempDir(), "building")
	buildCalls := filepath.Join(t.TempDir(), "build-calls")
	runArgs := filepath.Join(t.TempDir(), "run-args")
	script := `#!/bin/sh
case "$1" in
  build)
	printf '%s\n' "$(basename "$2")" >> "$ORBIT_TEST_BUILD_CALLS"
	if [ "$(basename "$2")" = "$ORBIT_TEST_BLOCK_PROJECT" ]; then
	  touch "$ORBIT_TEST_BUILD_STARTED"
	  while [ ! -f "$ORBIT_TEST_RELEASE_BUILD" ]; do sleep 0.01; done
	fi
	if [ "$(basename "$2")" = "$ORBIT_TEST_SLEEP_PROJECT" ]; then
	  touch "$ORBIT_TEST_BUILD_STARTED"
	  sleep 10
	fi
    mkdir "$ORBIT_TEST_BUILD_PROBE" 2>/dev/null || { touch "$ORBIT_TEST_BUILD_PROBE.overlap"; exit 1; }
	if [ "$(basename "$2")" = "$ORBIT_TEST_FAIL_PROJECT" ]; then
	  rmdir "$ORBIT_TEST_BUILD_PROBE"
	  exit 1
	fi
    sleep 0.1
    target="$PWD/bin/Debug/net8.0/$(basename "$2" .csproj).dll"
    mkdir -p "$(dirname "$target")"
    touch "$target"
    rmdir "$ORBIT_TEST_BUILD_PROBE"
    ;;
  msbuild)
    printf '%s/bin/Debug/net8.0/%s.dll\n' "$PWD" "$(basename "$2" .csproj)"
    ;;
	*)
	  printf '%s\n' "$1" >> "$ORBIT_TEST_RUN_ARGS"
	  ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "dotnet"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ORBIT_TEST_BUILD_PROBE", probe)
	t.Setenv("ORBIT_TEST_BUILD_CALLS", buildCalls)
	t.Setenv("ORBIT_TEST_RUN_ARGS", runArgs)

	services := make(map[string]*config.Service)
	for _, name := range []string{"first", "second"} {
		dir := filepath.Join(t.TempDir(), "project $cash's files")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		project := filepath.Join(dir, name+".csproj")
		if err := os.WriteFile(project, []byte("<Project />"), 0o644); err != nil {
			t.Fatal(err)
		}
		services[name] = &config.Service{Name: name, Type: "dotnet", Path: project}
	}
	cfg := &config.Config{Services: services, Containers: map[string]*config.Container{}}
	holder := config.NewHolder(cfg)
	orchestrator := NewOrchestrator(holder, nil, nil)
	manager := process.NewManager()
	app := &App{
		Holder:       holder,
		ProcessMgr:   manager,
		Orchestrator: orchestrator,
		Logs:         logging.NewMultiplexer(),
		dotnetBuilds: newDotnetBuildGate(),
	}
	app.wireProcessCallbacks(manager, holder)
	return app, cfg, services, probe
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func waitForFileLines(t *testing.T, path string, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && strings.Count(string(data), "\n") >= count {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d lines in %s", count, path)
}

func drainOrchestratorEvents(orchestrator *Orchestrator) {
	for {
		select {
		case event := <-orchestrator.events:
			_ = orchestrator.handleEvent(context.Background(), event)
		default:
			return
		}
	}
}

func TestResolveDotnetAssemblyPath_UsesProjectTargetFramework(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "MetisAPI.csproj")
	if err := os.WriteFile(project, []byte(`<Project Sdk="Microsoft.NET.Sdk.Web">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
</Project>`), 0644); err != nil {
		t.Fatal(err)
	}

	net6 := filepath.Join(dir, "bin", "Debug", "net6.0")
	net8 := filepath.Join(dir, "bin", "Debug", "net8.0")
	if err := os.MkdirAll(net6, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(net8, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(net6, "MetisAPI.dll"), []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(net8, "MetisAPI.dll"), []byte("current"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveDotnetAssemblyPath(context.Background(), dir, "MetisAPI.csproj", os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(net8, "MetisAPI.dll")
	gotInfo, gotErr := os.Stat(got)
	wantInfo, wantErr := os.Stat(want)
	if gotErr != nil || wantErr != nil || !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("assembly path = %q, want same file as %q", got, want)
	}
}

func TestResolveDotnetAssemblyPath_FallsBackToNewestBuildOutput(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "Api.csproj")
	if err := os.WriteFile(project, []byte(`<Project></Project>`), 0644); err != nil {
		t.Fatal(err)
	}

	oldDir := filepath.Join(dir, "bin", "Debug", "net6.0")
	newDir := filepath.Join(dir, "bin", "Debug", "net8.0")
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(oldDir, "Api.dll")
	newPath := filepath.Join(newDir, "Api.dll")
	if err := os.WriteFile(oldPath, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-time.Hour)
	newTime := time.Now()
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	got, err := resolveDotnetAssemblyPath(context.Background(), dir, "Api.csproj", os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	gotInfo, gotErr := os.Stat(got)
	wantInfo, wantErr := os.Stat(newPath)
	if gotErr != nil || wantErr != nil || !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("assembly path = %q, want same file as %q", got, newPath)
	}
}

func TestResolveDotnetAssemblyPath_UsesEvaluatedTargetPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake dotnet executable uses a POSIX shell")
	}
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "artifacts", "Api.dll")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("assembly"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' \"$ORBIT_TEST_TARGET_PATH\"\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "dotnet"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ORBIT_TEST_TARGET_PATH", target)
	environment := os.Environ()

	got, err := resolveDotnetAssemblyPath(context.Background(), dir, "Api.csproj", environment)
	if err != nil {
		t.Fatal(err)
	}
	if got != target {
		t.Fatalf("assembly path = %q, want evaluated target %q", got, target)
	}
}

func TestResolveDotnetAssemblyPath_MissingEvaluatedTargetFallsBackForMultiTargetProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake dotnet executable uses a POSIX shell")
	}
	dir := t.TempDir()
	project := filepath.Join(dir, "Api.csproj")
	if err := os.WriteFile(project, []byte(`<Project><PropertyGroup><TargetFrameworks>net8.0;net9.0</TargetFrameworks></PropertyGroup></Project>`), 0o644); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "bin", "Debug", "net8.0", "Api.dll")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("assembly"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' \"$PWD/bin/Debug/Api.dll\"\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "dotnet"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, err := resolveDotnetAssemblyPath(context.Background(), dir, "Api.csproj", os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("assembly path = %q, want legacy multi-target output %q", got, want)
	}
}

func TestResolveDotnetAssemblyPath_PreservesEvaluationFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake dotnet executable uses a POSIX shell")
	}
	fakeBin := t.TempDir()
	script := "#!/bin/sh\nprintf 'MSBuild evaluation detail\\n' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "dotnet"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := resolveDotnetAssemblyPath(context.Background(), t.TempDir(), "Api.csproj", os.Environ())
	if err == nil || !strings.Contains(err.Error(), "MSBuild evaluation detail") {
		t.Fatalf("error = %v, want MSBuild evaluation detail", err)
	}
}

func TestDotnetBuildEnvironment_PreservesNuGetAuditOverride(t *testing.T) {
	t.Setenv("NuGetAudit", "")
	environment := dotnetBuildEnvironment(map[string]string{"NuGetAudit": "true"})
	var values []string
	for _, entry := range environment {
		if strings.HasPrefix(entry, "NuGetAudit=") {
			values = append(values, entry)
		}
	}
	if len(values) == 0 || values[len(values)-1] != "NuGetAudit=true" || slices.Contains(values, "NuGetAudit=false") {
		t.Fatalf("NuGetAudit entries = %v, want final explicit true with no default false", values)
	}
}

func TestResolveDotnetAssemblyPath_UseArtifactsOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("requires the installed .NET SDK")
	}
	if _, err := exec.LookPath("dotnet"); err != nil {
		t.Skip("dotnet not installed")
	}
	versionOutput, err := exec.Command("dotnet", "--version").Output()
	if err != nil {
		t.Skipf("dotnet SDK version unavailable: %v", err)
	}
	majorText, _, _ := strings.Cut(strings.TrimSpace(string(versionOutput)), ".")
	major, err := strconv.Atoi(majorText)
	if err != nil || major < 8 {
		t.Skipf("UseArtifactsOutput requires .NET SDK 8 or newer; found %q", strings.TrimSpace(string(versionOutput)))
	}
	dir := t.TempDir()
	files := map[string]string{
		"Directory.Build.props": `<Project><PropertyGroup><UseArtifactsOutput>true</UseArtifactsOutput></PropertyGroup></Project>`,
		"Api/Api.csproj":        fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net%d.0</TargetFramework></PropertyGroup></Project>`, major),
		"Api/Program.cs":        `System.Console.WriteLine("ok");`,
	}
	for name, contents := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	projectDir := filepath.Join(dir, "Api")
	environment := dotnetBuildEnvironment(nil)
	build := exec.Command("dotnet", "build", "Api.csproj", "-v", "minimal")
	build.Dir = projectDir
	build.Env = environment
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("dotnet build: %v\n%s", err, output)
	}

	got, err := resolveDotnetAssemblyPath(context.Background(), projectDir, "Api.csproj", environment)
	if err != nil {
		t.Fatal(err)
	}
	wantFragment := filepath.Join("artifacts", "bin", "Api", "debug", "Api.dll")
	if !strings.HasSuffix(got, wantFragment) {
		t.Fatalf("assembly path = %q, want centralized output ending in %q", got, wantFragment)
	}
}
