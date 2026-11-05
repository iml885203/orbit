package platform

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// ErrProcessIdentityUnsupported means this platform cannot report a live
// process's argv or working directory, so callers cannot prove who owns it.
var ErrProcessIdentityUnsupported = errors.New("process identity is not readable on this platform")

// parseProcArgs2 decodes kern.procargs2: a native-endian int32 argc, the
// exec path, NUL padding, then argc NUL-terminated arguments.
func parseProcArgs2(raw []byte) ([]string, error) {
	if len(raw) < 4 {
		return nil, fmt.Errorf("procargs2 too short")
	}
	argc := int(binary.NativeEndian.Uint32(raw[:4]))
	rest := raw[4:]
	end := bytes.IndexByte(rest, 0)
	if end < 0 {
		return nil, fmt.Errorf("procargs2 missing exec path")
	}
	rest = bytes.TrimLeft(rest[end:], "\x00")
	args := make([]string, 0, argc)
	for len(args) < argc && len(rest) > 0 {
		end = bytes.IndexByte(rest, 0)
		if end < 0 {
			end = len(rest)
		}
		args = append(args, string(rest[:end]))
		if end == len(rest) {
			break
		}
		rest = rest[end+1:]
	}
	return args, nil
}

// parseProcCmdline decodes /proc/<pid>/cmdline. A program that sets its
// process title (npm, nginx) overwrites the argv area with one space-joined
// string and NUL padding, so that shape is split back into words. A single
// real argument ends in one NUL and keeps its spaces.
//
// The file is briefly empty while exec replaces the process image, and stays
// empty for zombies and kernel threads. That is no argv rather than one empty
// argument, so a caller waiting for exec does not mistake it for a program.
func parseProcCmdline(raw []byte) []string {
	trimmed := strings.TrimRight(string(raw), "\x00")
	if trimmed == "" {
		return nil
	}
	args := strings.Split(trimmed, "\x00")
	if len(args) == 1 && bytes.HasSuffix(raw, []byte("\x00\x00")) {
		return strings.Fields(args[0])
	}
	return args
}
