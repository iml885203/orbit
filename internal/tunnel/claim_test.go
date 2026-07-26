package tunnel

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestTunnelClaimUsesTunleaseFlagAliases(t *testing.T) {
	cmd := tunnelClaimCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	if err := cmd.ParseFlags([]string{"-p", "8080", "-g", "gateway.example", "-t", "secret", "-k", "-d", "-o", "json"}); err != nil {
		t.Fatal(err)
	}
	if to, _ := cmd.Flags().GetInt("to"); to != 8080 {
		t.Fatalf("to = %d", to)
	}
	if gateway, _ := cmd.Flags().GetString("gateway"); gateway != "gateway.example" {
		t.Fatalf("gateway = %q", gateway)
	}
	if insecure, _ := cmd.Flags().GetBool("insecure"); !insecure {
		t.Fatal("insecure flag not set")
	}
	if detach, _ := cmd.Flags().GetBool("detach"); !detach {
		t.Fatal("detach flag not set")
	}
	if output, _ := cmd.Flags().GetString("output"); output != "json" {
		t.Fatalf("output = %q", output)
	}
	if !strings.Contains(cmd.Example, "orbit tunnel claim '/foo/**' -p 8080") {
		t.Fatalf("example = %q", cmd.Example)
	}
	if usage := cmd.Flags().Lookup("detach").Usage; !strings.Contains(usage, "orbit tunnel release") {
		t.Fatalf("detach usage = %q", usage)
	}
}

func TestTunnelClaimOutputJSONRendersArgumentErrorOnce(t *testing.T) {
	cmd := tunnelClaimCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"-o", "json"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected argument error")
	}
	var event map[string]any
	if decodeErr := json.Unmarshal(stderr.Bytes(), &event); decodeErr != nil {
		t.Fatalf("stderr = %q: %v", stderr.String(), decodeErr)
	}
	if event["type"] != "error" || event["schema_version"] != float64(1) {
		t.Fatalf("event = %#v", event)
	}
}

func TestTunnelClaimOutputJSONRendersFlagError(t *testing.T) {
	cmd := tunnelClaimCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"-o", "json", "--unknown"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected flag error")
	}
	if !strings.Contains(stderr.String(), `"type":"error"`) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestTunnelListAndReleaseUseTunleaseFlagAliases(t *testing.T) {
	list := tunnelListCmd()
	if err := list.ParseFlags([]string{"-a", "-g", "gateway.example", "-t", "secret", "-k", "-o", "json"}); err != nil {
		t.Fatal(err)
	}
	if all, _ := list.Flags().GetBool("all"); !all {
		t.Fatal("all flag not set")
	}

	release := tunnelReleaseCmd()
	if err := release.ParseFlags([]string{"-p", "8080", "-g", "gateway.example", "-t", "secret", "-k", "-o", "json"}); err != nil {
		t.Fatal(err)
	}
	if to, _ := release.Flags().GetInt("to"); to != 8080 {
		t.Fatalf("to = %d", to)
	}
}
