package daemon

import "os"

// Host-tool doctor checks — plumbing for the doctor runner, not wire
// data (excluded from TS generation).

// HostToolCheck describes a host-side CLI dependency that orbit shells out to.
// Binary is resolved via exec.LookPath; when Version is non-nil and the binary
// is present, it is invoked for a version string appended to the pass message.
type HostToolCheck struct {
	Name     string
	Binary   string
	Critical bool
	Hint     string
	Version  func(path string) (string, error)
}

// WorkspaceRootCheck renders a workspace root value as a DoctorCheck.
// Exported because the CLI's no-daemon doctor fallback reports the same
// check — one owner keeps the name and hint strings from drifting.
// ok=false when the root is unset or missing on disk.
func WorkspaceRootCheck(root string) (DoctorCheck, bool) {
	if root == "" {
		return DoctorCheck{Name: "Workspace Root", Status: CheckWarn, Message: "Not set", Hint: "Set WORKSPACE_ROOT env var or run 'orbit init'"}, false
	}
	if _, err := os.Stat(root); err != nil {
		return DoctorCheck{Name: "Workspace Root", Status: CheckFail, Message: root + " (path not found)", Hint: "Update WORKSPACE_ROOT (or workspace_root in settings) to an existing checkout directory"}, false
	}
	return DoctorCheck{Name: "Workspace Root", Status: CheckPass, Message: root}, true
}
