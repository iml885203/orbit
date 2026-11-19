//go:build e2e && !windows

package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/autoupdate"
)

func TestE2E_UpdateNotificationInteractiveCommandBoundary(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("interactive terminal fixture requires Python 3")
	}
	root := t.TempDir()
	launch := filepath.Join(root, "Cellar", "orbit", "1.3.0", "bin", "orbit")
	if err := os.MkdirAll(filepath.Dir(launch), 0o700); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-ldflags=-X main.version=v1.3.0", "-o", launch, "./cmd/orbit")
	build.Dir = filepath.Dir(findRepoFile(t, "go.mod"))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build installed fixture: %v\n%s", err, out)
	}
	t.Setenv("ORBIT_HOME", filepath.Join(root, "home"))
	t.Setenv("ORBIT_UPDATE_HOME", filepath.Join(root, "updates"))
	t.Setenv(autoupdate.EnvLaunchPath, "")
	for _, key := range []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "TF_BUILD", "JENKINS_URL", "BUILD_BUILDID", backgroundUpdateEnv} {
		t.Setenv(key, "")
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		<-r.Context().Done()
	}))
	defer func() { server.CloseClientConnections(); server.Close() }()
	t.Setenv("ORBIT_RELEASE_API_URL", server.URL)
	config := filepath.Join(root, "orbit.yaml")
	if err := os.WriteFile(config, []byte("version: \"3\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := autoupdate.Load(launch)
	if err != nil {
		t.Fatal(err)
	}
	s.CurrentVersion, s.TargetVersion, s.Phase = "v1.3.0", "v999.0.0", "available"
	s.DisclosureShown = true
	next := time.Now().Add(24 * time.Hour)
	s.NextCheckAt = &next
	if err := autoupdate.Save(s); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name           string
		args           []string
		ci, pipe, want bool
		wantFailure    bool
	}{
		{name: "long help does not consume", args: []string{"status", "--help"}},
		{name: "short help does not consume", args: []string{"up", "-h"}},
		{name: "redirect does not consume", args: []string{"status"}, pipe: true},
		{name: "JSON does not consume", args: []string{"status", "--json"}},
		{name: "CI does not consume", args: []string{"status"}, ci: true},
		{name: "error does not consume", args: []string{"status", "--unknown-notice-fixture"}, wantFailure: true},
		{name: "interactive notice", args: []string{"status"}, want: true},
		{name: "next process deduped", args: []string{"status"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			args := append([]string{"-c", notificationPTY, launch, "-c", config}, tc.args...)
			cmd := exec.CommandContext(ctx, python, args...)
			cmd.Env = environmentWithValue(os.Environ(), "CI", "false")
			cmd.Env = environmentWithValue(cmd.Env, "ORBIT_NOTICE_PIPE_STDOUT", "")
			if tc.pipe {
				cmd.Env = environmentWithValue(cmd.Env, "ORBIT_NOTICE_PIPE_STDOUT", "1")
			}
			if tc.ci {
				cmd.Env = environmentWithValue(cmd.Env, "CI", "true")
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("terminal fixture: %v\n%s", err, out)
			}
			var result struct {
				Stdout string `json:"stdout"`
				Stderr string `json:"stderr"`
				Code   int    `json:"code"`
			}
			if err := json.Unmarshal(out, &result); err != nil {
				t.Fatalf("terminal output: %v\n%s", err, out)
			}
			if (result.Code != 0) != tc.wantFailure {
				t.Fatalf("exit=%d stdout=%q stderr=%q", result.Code, result.Stdout, result.Stderr)
			}
			if strings.Contains(result.Stderr, "Orbit v999.0.0 is available") != tc.want || strings.Contains(result.Stdout, "Orbit v999.0.0 is available") {
				t.Fatalf("wrong notification stream/state: %+v", result)
			}
			if tc.want && !strings.Contains(result.Stderr, "brew upgrade iml885203/tap/orbit") {
				t.Fatalf("missing owner command: %s", result.Stderr)
			}
			if len(tc.args) > 1 && tc.args[1] == "--json" && !json.Valid([]byte(result.Stdout)) {
				t.Fatalf("invalid JSON stdout: %q", result.Stdout)
			}
		})
	}
	direct := filepath.Join(root, "direct", "orbit")
	if err := os.MkdirAll(filepath.Dir(direct), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(launch, direct); err != nil {
		t.Fatal(err)
	}
	directState, err := autoupdate.Load(direct)
	if err != nil {
		t.Fatal(err)
	}
	directState.CurrentVersion, directState.TargetVersion, directState.Phase = "v1.3.0", "v999.0.0", "ready"
	directState.DisclosureShown, directState.ApplyEligible = true, true
	directState.StagedBinary, directState.StagedEvidence = "fixture-staged", &autoupdate.VerificationRecord{Tag: "v999.0.0"}
	directState.NextCheckAt = &next
	if err := autoupdate.Save(directState); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(os.Getenv("ORBIT_UPDATE_HOME"), "update-registry.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", notificationPTY, direct, "-c", config, "status")
	started := time.Now()
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "verified and ready") || time.Since(started) > 2*time.Second {
		t.Fatalf("direct staged command waited for registry: elapsed=%v, err=%v, out=%s", time.Since(started), err, out)
	}
	if requests.Load() != 0 {
		t.Fatalf("notice made %d release requests", requests.Load())
	}
}

const notificationPTY = `
import errno,json,os,pty,selectors,subprocess,sys
sel=selectors.DefaultSelector(); outputs={'stdout':bytearray(),'stderr':bytearray()}; slaves=[]
for key in outputs:
 master,slave=os.pipe() if key=='stdout' and os.environ.get('ORBIT_NOTICE_PIPE_STDOUT') else pty.openpty()
 sel.register(master,selectors.EVENT_READ,key);slaves.append(slave)
p=subprocess.Popen(sys.argv[1:],stdin=subprocess.DEVNULL,stdout=slaves[0],stderr=slaves[1])
for slave in slaves:os.close(slave)
while sel.get_map():
 for entry,_ in sel.select(1):
  try:data=os.read(entry.fd,65536)
  except OSError as e:
   if e.errno!=errno.EIO:raise
   data=b''
  if data:outputs[entry.data].extend(data)
  else:sel.unregister(entry.fd);os.close(entry.fd)
print(json.dumps({**{k:v.decode(errors='replace') for k,v in outputs.items()},'code':p.wait()}))
`
