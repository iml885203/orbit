package app

import (
	"os"
	"testing"

	"github.com/iml885203/orbit/cli"

	"github.com/fatih/color"
)

func init() {
	// Disable colors in tests for deterministic output
	color.NoColor = true
}

func TestStateIcon(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{"healthy", "●"},
		{"starting", "◐"},
		{"degraded", "◑"},
		{"stopping", "◔"},
		{"stopped", "○"},
		{"pending", "○"},
		{"unknown", "?"},
		{"", "?"},
	}
	for _, tt := range tests {
		got := cli.StateIcon(tt.state)
		if got != tt.want {
			t.Errorf("cli.StateIcon(%q) = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestIsTerminal_NoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if isTerminal() {
		t.Error("isTerminal() should return false when NO_COLOR is set")
	}
}

func TestIsTerminal_Pipe(t *testing.T) {
	// With NO_COLOR unset, stdout in tests is typically not a TTY
	_ = os.Unsetenv("NO_COLOR")
	if isTerminal() {
		t.Skip("stdout is a TTY in this test environment")
	}
}

func TestFormatPorts(t *testing.T) {
	tests := []struct {
		name  string
		ports map[string]int
		want  string
	}{
		{"nil", nil, ""},
		{"empty", map[string]int{}, ""},
		{"single http", map[string]int{"http": 8080}, "http://localhost:8080"},
		{"single raw", map[string]int{"redis": 6379}, ":6379"},
		{"multiple sorted", map[string]int{"http": 8080, "grpc": 9090}, ":9090 http://localhost:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatPorts(tt.ports)
			if got != tt.want {
				t.Errorf("formatPorts(%v) = %q, want %q", tt.ports, got, tt.want)
			}
		})
	}
}
