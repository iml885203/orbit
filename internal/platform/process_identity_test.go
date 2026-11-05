package platform

import (
	"encoding/binary"
	"slices"
	"testing"
)

func procArgs2Fixture(argc uint32, body string) []byte {
	raw := binary.NativeEndian.AppendUint32(nil, argc)
	return append(raw, body...)
}

func TestParseProcArgs2(t *testing.T) {
	tests := map[string]struct {
		raw  []byte
		want []string
	}{
		"argv after padded exec path": {
			procArgs2Fixture(2, "/usr/bin/python3\x00\x00\x00\x00python3\x00server.py\x00"),
			[]string{"python3", "server.py"},
		},
		"stops at argc before the environment": {
			procArgs2Fixture(1, "/bin/sleep\x00\x00sleep\x00PATH=/bin\x00HOME=/tmp\x00"),
			[]string{"sleep"},
		},
		"empty argument mid-list": {
			procArgs2Fixture(3, "/bin/sh\x00\x00sh\x00\x00-x\x00"),
			[]string{"sh", "", "-x"},
		},
		"no arguments": {procArgs2Fixture(0, "/bin/true\x00\x00"), []string{}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := parseProcArgs2(tt.raw)
			if err != nil || !slices.Equal(got, tt.want) {
				t.Fatalf("parseProcArgs2 = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestParseProcArgs2RejectsTruncatedInput(t *testing.T) {
	for name, raw := range map[string][]byte{
		"shorter than argc": {1, 0},
		"no exec path end":  procArgs2Fixture(1, "/bin/sleep"),
	} {
		if _, err := parseProcArgs2(raw); err == nil {
			t.Errorf("%s: parseProcArgs2 accepted %q", name, raw)
		}
	}
}

func TestParseProcCmdline(t *testing.T) {
	tests := map[string]struct {
		raw  string
		want []string
	}{
		"argv":                        {"python3\x00server.py\x00", []string{"python3", "server.py"}},
		"empty while exec is pending": {"", nil},
		"renamed title with padding":  {"npm run dev\x00\x00\x00\x00", []string{"npm", "run", "dev"}},
		"one argument with spaces":    {"/opt/my app/server\x00", []string{"/opt/my app/server"}},
		"empty argument kept":         {"sh\x00\x00-x\x00", []string{"sh", "", "-x"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := parseProcCmdline([]byte(tt.raw)); !slices.Equal(got, tt.want) {
				t.Fatalf("parseProcCmdline = %q; want %q", got, tt.want)
			}
		})
	}
}
