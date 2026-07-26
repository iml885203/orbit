package tunnel

import (
	"testing"

	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/extension"
	"github.com/iml885203/orbit/process"
	"github.com/iml885203/tunlease/pkg/tunnelclient"
)

type testHost struct {
	holder *config.Holder
	pm     *process.Manager
}

func newTestHost(cfg *config.Config) *testHost {
	return &testHost{config.NewHolder(cfg), process.NewManager()}
}
func (h *testHost) Config() *config.Config                            { return h.holder.Load() }
func (h *testHost) UpdateConfig(func(extension.ConfigTx) error) error { panic("not used") }
func (h *testHost) ProcessMgr() *process.Manager                      { return h.pm }

func TestViewsExposeGatewayAndSessions(t *testing.T) {
	cfg := (&config.Config{}).WithExtension("claim", &ClaimConfig{Gateway: "https://tunlease.example"})
	tm := NewTunnelManager(newTestHost(cfg), NewAccessLogHub(10))
	tm.tunnels[8080] = &Tunnel{LocalPort: 8080, ProxyPort: 4567, Paths: []string{"/callbacks/x/*"}, Status: statusHealthy}
	got := tm.Views()
	if got.Gateway != "https://tunlease.example" || len(got.Tunnels) != 1 || got.Tunnels[0].ProxyPort != 4567 {
		t.Fatalf("views = %#v", got)
	}
}

func TestClientRequiresGateway(t *testing.T) {
	tm := NewTunnelManager(newTestHost(&config.Config{}), NewAccessLogHub(10))
	if _, err := tm.client(nil); err == nil {
		t.Fatal("expected missing gateway error")
	}
}

func TestClientClaimOverridesEnvironmentConfig(t *testing.T) {
	cfg := (&config.Config{}).WithExtension("claim", &ClaimConfig{Gateway: "https://env.example"})
	tm := NewTunnelManager(newTestHost(cfg), NewAccessLogHub(10))
	client, err := tm.client(&ClientAPIOptions{Gateway: "http://override.example", Insecure: true})
	if err != nil {
		t.Fatal(err)
	}
	if client.Gateway() != "http://override.example/_tunlease" {
		t.Fatalf("gateway = %q", client.Gateway())
	}
}

func TestRemoveTerminalSessionStopsEmptyTunnel(t *testing.T) {
	tm := NewTunnelManager(newTestHost(&config.Config{}), NewAccessLogHub(10))
	session := &tunnelclient.Session{}
	stopped := false
	tm.tunnels[8080] = &Tunnel{
		LocalPort: 8080,
		Paths:     []string{"/foo"},
		sessions:  map[string]*tunnelclient.Session{"/foo": session},
		stopProxy: func() { stopped = true },
	}

	tm.removeTerminalSession(8080, "/foo", session)

	if tm.tunnels[8080] != nil {
		t.Fatal("empty tunnel was not removed")
	}
	if !stopped {
		t.Fatal("empty tunnel proxy was not stopped")
	}
}
