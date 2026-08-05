package container

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/logging"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

type imageAPIServer struct {
	mu          sync.Mutex
	available   bool
	pulls       int
	platforms   []string
	pullError   bool
	streamError bool
	pullStarted chan struct{}
	releasePull chan struct{}
}

func (s *imageAPIServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
		s.mu.Lock()
		available := s.available
		s.mu.Unlock()
		if !available {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"No such image"}`))
			return
		}
		_, _ = w.Write([]byte(`{"Id":"sha256:test"}`))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/create"):
		s.mu.Lock()
		s.pulls++
		s.platforms = append(s.platforms, r.URL.Query().Get("platform"))
		pullError := s.pullError
		streamError := s.streamError
		pullStarted := s.pullStarted
		releasePull := s.releasePull
		s.mu.Unlock()
		if pullStarted != nil {
			close(pullStarted)
		}
		if releasePull != nil {
			<-releasePull
		}
		if pullError {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"registry unavailable"}`))
			return
		}
		if streamError {
			_, _ = w.Write([]byte(`{"errorDetail":{"message":"registry unavailable"},"error":"registry unavailable"}`))
			return
		}
		s.mu.Lock()
		s.available = true
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "done"})
	default:
		http.Error(w, `{"message":"unexpected Docker API request"}`, http.StatusNotFound)
	}
}

func newImageTestManager(t *testing.T, api *imageAPIServer) *Manager {
	t.Helper()
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	httpClient := server.Client()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	opts := []client.Opt{
		client.WithHost("tcp://" + endpoint.Host),
		client.WithHTTPClient(httpClient),
		client.WithAPIVersion("1.52"),
	}
	cli, err := client.New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return &Manager{cli: cli, imagePulls: newImagePullCoordinator()}
}

func TestEnsureImageAvailableHonorsPullPoliciesAndPlatform(t *testing.T) {
	t.Run("if_not_present cached", func(t *testing.T) {
		api := &imageAPIServer{available: true}
		manager := newImageTestManager(t, api)
		cfg := &config.Container{Image: "redis:7", PullPolicy: "if_not_present"}
		if err := manager.ensureImageAvailable(context.Background(), "redis", cfg); err != nil {
			t.Fatal(err)
		}
		if api.pulls != 0 {
			t.Fatalf("pulls = %d, want 0", api.pulls)
		}
	})

	t.Run("never missing", func(t *testing.T) {
		api := &imageAPIServer{}
		manager := newImageTestManager(t, api)
		cfg := &config.Container{Image: "redis:7", PullPolicy: "never"}
		err := manager.ensureImageAvailable(context.Background(), "redis", cfg)
		if err == nil || !strings.Contains(err.Error(), "pull_policy is set to never") {
			t.Fatalf("error = %v", err)
		}
		if api.pulls != 0 {
			t.Fatalf("pulls = %d, want 0", api.pulls)
		}
	})

	t.Run("always passes platform and pulls again later", func(t *testing.T) {
		api := &imageAPIServer{}
		manager := newImageTestManager(t, api)
		cfg := &config.Container{Image: "redis:7", PullPolicy: "always", Platform: "linux/amd64"}
		for range 2 {
			if err := manager.ensureImageAvailable(context.Background(), "redis", cfg); err != nil {
				t.Fatal(err)
			}
		}
		if api.pulls != 2 {
			t.Fatalf("pulls = %d, want 2", api.pulls)
		}
		for _, platform := range api.platforms {
			if platform != "linux/amd64" {
				t.Fatalf("platform = %q, want linux/amd64", platform)
			}
		}
	})

	t.Run("pull failure identifies image and platform", func(t *testing.T) {
		api := &imageAPIServer{pullError: true}
		manager := newImageTestManager(t, api)
		cfg := &config.Container{Image: "private.example/app:1", PullPolicy: "always", Platform: "linux/arm64"}
		err := manager.ensureImageAvailable(context.Background(), "app", cfg)
		if err == nil || !strings.Contains(err.Error(), "private.example/app:1 (platform linux/arm64)") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("streamed pull failure is not masked by a cached image", func(t *testing.T) {
		api := &imageAPIServer{available: true, streamError: true}
		manager := newImageTestManager(t, api)
		cfg := &config.Container{Image: "private.example/app:1", PullPolicy: "always"}
		err := manager.ensureImageAvailable(context.Background(), "app", cfg)
		if err == nil || !strings.Contains(err.Error(), "registry unavailable") {
			t.Fatalf("error = %v, want streamed registry error", err)
		}
	})
}

func TestStartSidecarUsesCoordinatedPullAndPreservesInheritedPlatform(t *testing.T) {
	api := &imageAPIServer{streamError: true}
	manager := newImageTestManager(t, api)
	parent := &config.Container{Image: "app:1", PullPolicy: "always", Platform: "linux/arm64"}
	sidecar := &config.Sidecar{Name: "helper", Image: "private.example/helper:1"}

	err := manager.StartSidecar(context.Background(), "api", parent, sidecar)
	if err == nil || !strings.Contains(err.Error(), "private.example/helper:1 (platform linux/arm64)") {
		t.Fatalf("sidecar pull error = %v", err)
	}
	if api.pulls != 1 || len(api.platforms) != 1 || api.platforms[0] != "linux/arm64" {
		t.Fatalf("sidecar pulls = %d, platforms = %v", api.pulls, api.platforms)
	}
}

func TestEnsureImageAvailableNarratesSharedPullLifecycle(t *testing.T) {
	pullStarted := make(chan struct{})
	releasePull := make(chan struct{})
	api := &imageAPIServer{pullStarted: pullStarted, releasePull: releasePull}
	manager := newImageTestManager(t, api)
	var mu sync.Mutex
	var actions []string
	manager.OnAction = func(name, message string) {
		mu.Lock()
		actions = append(actions, name+": "+message)
		mu.Unlock()
	}
	cfg := &config.Container{Image: "redis:7", PullPolicy: "always"}

	first := make(chan error, 1)
	go func() {
		first <- manager.ensureImageAvailable(context.Background(), "redis-a", cfg)
	}()
	<-pullStarted
	second := make(chan error, 1)
	go func() {
		second <- manager.ensureImageAvailable(context.Background(), "redis-b", cfg)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		joined := false
		for _, action := range actions {
			joined = joined || action == "redis-b: waiting for in-flight pull redis:7"
		}
		mu.Unlock()
		if joined {
			break
		}
		if time.Now().After(deadline) {
			mu.Lock()
			observed := append([]string(nil), actions...)
			mu.Unlock()
			t.Fatalf("shared-pull narration not observed: %v", observed)
		}
		time.Sleep(time.Millisecond)
	}

	close(releasePull)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(actions, "\n")
	for _, want := range []string{
		"redis-a: pulling image redis:7",
		"redis-a: image ready redis:7",
		"redis-b: waiting for in-flight pull redis:7",
		"redis-b: image ready redis:7",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("actions missing %q:\n%s", want, joined)
		}
	}
	if api.pulls != 1 {
		t.Fatalf("pulls = %d, want 1", api.pulls)
	}
}

func TestEnsureImageAvailableReportsSharedFailureForEachConfiguredAlias(t *testing.T) {
	pullStarted := make(chan struct{})
	releasePull := make(chan struct{})
	api := &imageAPIServer{
		available: true, streamError: true, pullStarted: pullStarted, releasePull: releasePull,
	}
	manager := newImageTestManager(t, api)
	waiting := make(chan struct{})
	manager.OnAction = func(name, message string) {
		if name == "qualified" && strings.HasPrefix(message, "waiting for in-flight pull") {
			close(waiting)
		}
	}

	leader := make(chan error, 1)
	go func() {
		leader <- manager.ensureImageAvailable(context.Background(), "short", &config.Container{
			Image: "redis", PullPolicy: "always", Platform: "linux/amd64",
		})
	}()
	<-pullStarted
	follower := make(chan error, 1)
	go func() {
		follower <- manager.ensureImageAvailable(context.Background(), "qualified", &config.Container{
			Image: "docker.io/library/redis:latest", PullPolicy: "always", Platform: "LINUX/AMD64",
		})
	}()
	<-waiting
	close(releasePull)
	if err := <-leader; err == nil || !strings.Contains(err.Error(), "redis (platform linux/amd64)") {
		t.Fatalf("leader error = %v", err)
	}
	if err := <-follower; err == nil || !strings.Contains(err.Error(), "docker.io/library/redis:latest (platform LINUX/AMD64)") {
		t.Fatalf("follower error = %v", err)
	}
	if api.pulls != 1 {
		t.Fatalf("pulls = %d, want 1", api.pulls)
	}
}

type lifecycleAPIServer struct {
	mu             sync.Mutex
	available      map[string]bool
	pullStarted    chan string
	releasePull    map[string]chan struct{}
	inspectStarted chan string
	releaseInspect map[string]chan struct{}
	containerMade  chan string
}

func (s *lifecycleAPIServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
		image := strings.TrimSuffix(strings.Split(r.URL.Path, "/images/")[1], "/json")
		image, _ = url.PathUnescape(image)
		if strings.Contains(image, "first") {
			image = "first:latest"
		} else if strings.Contains(image, "second") {
			image = "second:latest"
		}
		s.mu.Lock()
		available := s.available[image]
		s.mu.Unlock()
		if !available {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"No such image"}`))
			return
		}
		if release := s.releaseInspect[image]; release != nil {
			s.inspectStarted <- image
			<-release
		}
		_, _ = w.Write([]byte(`{"Id":"sha256:test"}`))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/create"):
		image := r.URL.Query().Get("fromImage")
		if strings.Contains(image, "first") {
			image = "first:latest"
		} else if strings.Contains(image, "second") {
			image = "second:latest"
		}
		s.pullStarted <- image
		<-s.releasePull[image]
		s.mu.Lock()
		s.available[image] = true
		s.mu.Unlock()
		_, _ = w.Write([]byte("{\"status\":\"done\"}\n"))
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/containers/") && strings.HasSuffix(r.URL.Path, "/json"):
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"No such container"}`))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
		name := r.URL.Query().Get("name")
		s.containerMade <- name
		_, _ = w.Write([]byte(`{"Id":"` + name + `-id","Warnings":[]}`))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start"):
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/logs"):
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, `{"message":"unexpected Docker API request"}`, http.StatusNotFound)
	}
}

func TestImageVerificationDoesNotOccupyPullConcurrencySlot(t *testing.T) {
	firstPullRelease := make(chan struct{})
	secondPullRelease := make(chan struct{})
	firstInspectRelease := make(chan struct{})
	api := &lifecycleAPIServer{
		available:      make(map[string]bool),
		pullStarted:    make(chan string, 2),
		releasePull:    map[string]chan struct{}{"first:latest": firstPullRelease, "second:latest": secondPullRelease},
		inspectStarted: make(chan string, 1),
		releaseInspect: map[string]chan struct{}{"first:latest": firstInspectRelease},
		containerMade:  make(chan string, 2),
	}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cli, err := client.New(
		client.WithHost("tcp://"+endpoint.Host),
		client.WithHTTPClient(server.Client()),
		client.WithAPIVersion("1.52"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	manager := &Manager{cli: cli, imagePulls: newImagePullCoordinator()}
	manager.SetImagePullConcurrency(1)

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- manager.ensureImageAvailable(context.Background(), "first", &config.Container{
			Image: "first:latest", PullPolicy: "always",
		})
	}()
	if image := <-api.pullStarted; image != "first:latest" {
		t.Fatalf("first pull = %q", image)
	}
	close(firstPullRelease)
	if image := <-api.inspectStarted; image != "first:latest" {
		t.Fatalf("blocked inspect = %q", image)
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- manager.ensureImageAvailable(context.Background(), "second", &config.Container{
			Image: "second:latest", PullPolicy: "always",
		})
	}()
	select {
	case image := <-api.pullStarted:
		if image != "second:latest" {
			t.Fatalf("second pull = %q", image)
		}
	case <-time.After(time.Second):
		t.Fatal("image verification kept the pull concurrency slot occupied")
	}
	close(secondPullRelease)
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	close(firstInspectRelease)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestManagerStartsReadyContainerWithoutWaitingForQueuedPull(t *testing.T) {
	firstRelease := make(chan struct{})
	secondRelease := make(chan struct{})
	api := &lifecycleAPIServer{
		available:     make(map[string]bool),
		pullStarted:   make(chan string, 2),
		releasePull:   map[string]chan struct{}{"first:latest": firstRelease, "second:latest": secondRelease},
		containerMade: make(chan string, 2),
	}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cli, err := client.New(
		client.WithHost("tcp://"+endpoint.Host),
		client.WithHTTPClient(server.Client()),
		client.WithAPIVersion("1.52"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	manager := &Manager{cli: cli, imagePulls: newImagePullCoordinator()}
	manager.SetImagePullConcurrency(1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- manager.Start(ctx, "first", &config.Container{
			Image: "first:latest", PullPolicy: "always",
		})
	}()
	if image := <-api.pullStarted; image != "first:latest" {
		t.Fatalf("first pull = %q", image)
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- manager.Start(ctx, "second", &config.Container{
			Image: "second:latest", PullPolicy: "always",
		})
	}()
	select {
	case image := <-api.pullStarted:
		t.Fatalf("queued pull started too early: %s", image)
	case <-time.After(20 * time.Millisecond):
	}

	close(firstRelease)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if name := <-api.containerMade; !strings.Contains(name, "first") {
		t.Fatalf("first container created = %q", name)
	}
	select {
	case image := <-api.pullStarted:
		if image != "second:latest" {
			t.Fatalf("second pull = %q", image)
		}
	case <-time.After(time.Second):
		t.Fatal("queued pull did not start after the first image became ready")
	}
	select {
	case name := <-api.containerMade:
		t.Fatalf("second container created before its image was ready: %s", name)
	default:
	}
	close(secondRelease)
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	if name := <-api.containerMade; !strings.Contains(name, "second") {
		t.Fatalf("second container created = %q", name)
	}
	cancel()
}

func TestPollerDebouncesDockerOutageAndReportsItOnce(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.WithHost("tcp://127.0.0.1:1"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cli.Close() }()

	poller := NewPoller(cli, "test", time.Second)
	reports := 0
	poller.OnUnavailable = func(error) {
		reports++
	}

	poller.poll(context.Background())
	if reports != 0 {
		t.Fatalf("first transport failure reported an outage: %d", reports)
	}
	poller.poll(context.Background())
	poller.poll(context.Background())
	if reports != 1 {
		t.Fatalf("outage reports = %d, want one after consecutive failures", reports)
	}
}

func TestContainerPortConflictHidesContainerRuntimeError(t *testing.T) {
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	hostPort := listener.Addr().(*net.TCPAddr).Port

	conflict := containerPortConflict("redis", &config.Container{
		Ports: map[string]config.PortDef{"redis": {Host: hostPort, Target: 6379}},
	})
	if conflict == nil || conflict.Port != hostPort || conflict.Service != "redis" {
		t.Fatalf("conflict = %+v", conflict)
	}
	if strings.Contains(strings.ToLower(conflict.Error()), "docker") ||
		strings.Contains(strings.ToLower(conflict.Error()), "endpoint") {
		t.Fatalf("conflict leaked runtime internals: %v", conflict)
	}
}

func TestContainerConfigFingerprintIsStableAndCoversRuntimeIntent(t *testing.T) {
	base := &config.Container{
		Image:       "redis:7",
		Command:     []string{"redis-server"},
		Environment: map[string]string{"MODE": "dev", "CACHE": "on"},
		Ports:       map[string]config.PortDef{"redis": {Host: 6379, Target: 6379}},
	}
	same := &config.Container{
		Image:       "redis:7",
		Command:     []string{"redis-server"},
		Environment: map[string]string{"CACHE": "on", "MODE": "dev"},
		Ports:       map[string]config.PortDef{"redis": {Host: 6379, Target: 6379}},
	}
	if got, want := containerConfigFingerprint(base, ""), containerConfigFingerprint(same, ""); got != want {
		t.Fatalf("equivalent configs differ: %s != %s", got, want)
	}

	changed := *base
	changed.Image = "redis:8"
	if containerConfigFingerprint(base, "") == containerConfigFingerprint(&changed, "") {
		t.Fatal("image change did not change fingerprint")
	}
	if containerConfigFingerprint(base, "") == containerConfigFingerprint(base, "api") {
		t.Fatal("sidecar ownership did not change fingerprint")
	}
}

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
