package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/spf13/cobra"
)

// Claude Code gets its marketplace plugin because the marketplace is also
// its update channel; every other agent reads one copy in the universal
// ~/.agents/skills directory, which refreshes with the CLI. Both routes are
// idempotent so the skill can name this command as a setup step.

const agentSkillCheckName = "Agent skill"

// skillMarkerFile records the digest of what Orbit wrote, so a later Orbit
// can tell its own outdated copy from a user's edits.
const skillMarkerFile = ".orbit-skill.json"

const (
	skillDirCurrent  = "current"
	skillDirOutdated = "outdated"
	skillDirModified = "modified"
	skillDirMissing  = "missing"

	skillActionNone      = "none"
	skillActionInstall   = "install"
	skillActionUninstall = "uninstall"
	skillActionWrite     = "write"
	skillActionReplace   = "replace"
	skillActionRemove    = "remove"
)

type skillOptions struct {
	yes bool
}

type skillData struct {
	Operation       string          `json:"operation"`
	Applied         bool            `json:"applied"`
	ClaudePlugin    skillPluginData `json:"claude_plugin"`
	AgentsDirectory skillDirData    `json:"agents_directory"`
}

type skillPluginData struct {
	ClaudeCodeDetected bool     `json:"claude_code_detected"`
	Installed          bool     `json:"installed"`
	Action             string   `json:"action"`
	Reason             string   `json:"reason,omitempty"`
	Commands           []string `json:"commands,omitempty"`
}

type skillDirData struct {
	Path   string `json:"path"`
	State  string `json:"state"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

// runAgentCommand executes one coding-agent CLI step. Its output goes to
// stderr so plugin progress stays visible while Orbit's stdout carries only
// the JSON envelope. Tests replace it.
var runAgentCommand = func(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func skillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install the Orbit agent skill into the coding agents on this machine",
		Long: "Put the Orbit agent skill where coding agents load it: the Claude Code\n" +
			"marketplace plugin when Claude Code is present, and one copy in\n" +
			"~/.agents/skills/orbit for every agent that reads the universal skills\n" +
			"directory. A newly installed plugin loads in the next agent session.",
	}
	cmd.AddCommand(skillInstallCmd(), skillUninstallCmd())
	return cmd
}

func skillInstallCmd() *cobra.Command {
	var opts skillOptions
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install or refresh the Orbit agent skill for every agent on this machine",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSkill(opts, (*skillRoutes).installPlan, (*skillRoutes).applyInstall, "orbit skill install --yes")
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "apply the displayed plan instead of previewing it")
	return cmd
}

func skillUninstallCmd() *cobra.Command {
	var opts skillOptions
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the Orbit plugin and the universal skill copy",
		Long: "Remove the Claude Code plugin and the copy in ~/.agents/skills/orbit that\n" +
			"'orbit skill install' wrote. A copy edited by hand is left in place, and the\n" +
			"marketplace registration stays.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSkill(opts, (*skillRoutes).uninstallPlan, (*skillRoutes).applyUninstall, "orbit skill uninstall --yes")
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "apply the displayed plan instead of previewing it")
	return cmd
}

func runSkill(
	opts skillOptions,
	plan func(*skillRoutes) (skillData, error),
	apply func(*skillRoutes, *skillData) error,
	applyCommand string,
) error {
	routes, err := newSkillRoutes()
	if err != nil {
		return err
	}
	data, err := plan(routes)
	if err != nil {
		return err
	}
	if opts.yes {
		if err := apply(routes, &data); err != nil {
			// The directory route has already run by the time the plugin
			// route can fail; keep that partial result visible either way.
			if cli.JSONOutput {
				if writeErr := cli.WriteJSONFailure(os.Stdout, commandString(), data, err, nil); writeErr != nil {
					return writeErr
				}
				return errCLIJSONAlreadyRendered{err: err}
			}
			if directoryRouteDone(data.AgentsDirectory) {
				fmt.Fprintf(os.Stderr, "%s: %s\n", data.AgentsDirectory.Path, skillActionDone[data.AgentsDirectory.Action])
			}
			return err
		}
	}
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), data, nil)
	}
	printSkillResult(os.Stdout, data, applyCommand)
	return nil
}

type skillRoutes struct {
	skill       fs.FS
	marketplace string
	pluginID    string
	home        string
}

func newSkillRoutes() (*skillRoutes, error) {
	if distribution.AgentSkill == nil {
		return nil, errors.New("this build bundles no agent skill")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	return &skillRoutes{
		skill:       distribution.AgentSkill,
		marketplace: distribution.PluginMarketplace,
		pluginID:    distribution.PluginID,
		home:        home,
	}, nil
}

func (r *skillRoutes) pluginRouteAvailable() bool {
	return r.marketplace != "" && r.pluginID != ""
}

func (r *skillRoutes) dir() string {
	return filepath.Join(r.home, ".agents", "skills", "orbit")
}

// claudeConfigDir honors CLAUDE_CONFIG_DIR the way Claude Code does.
func (r *skillRoutes) claudeConfigDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(r.home, ".claude")
}

// claudeCode reports whether Claude Code is present and whether its CLI can
// be run from this shell. Agent shells do not always carry the PATH the
// user's terminal has, so the configuration directory counts as presence.
func (r *skillRoutes) claudeCode() (present, runnable bool) {
	if _, err := exec.LookPath("claude"); err == nil {
		return true, true
	}
	return pathExists(r.claudeConfigDir()), false
}

func (r *skillRoutes) pluginInstallSteps() [][]string {
	return [][]string{
		{"claude", "plugin", "marketplace", "add", r.marketplace},
		{"claude", "plugin", "install", r.pluginID},
	}
}

func (r *skillRoutes) pluginUninstallSteps() [][]string {
	return [][]string{{"claude", "plugin", "uninstall", r.pluginID}}
}

func commandLines(steps [][]string) []string {
	lines := make([]string, 0, len(steps))
	for _, step := range steps {
		lines = append(lines, strings.Join(step, " "))
	}
	return lines
}

// pluginPlan decides the plugin route for either direction. It reads Claude
// Code's installed-plugin record rather than spawning its CLI, so planning
// works wherever the record does; an unreadable record is reported as
// unknown and handed over with the commands instead of guessed at.
func (r *skillRoutes) pluginPlan(steps func() [][]string, actionWhenDue string, dueWhenInstalled bool) skillPluginData {
	present, runnable := r.claudeCode()
	plugin := skillPluginData{ClaudeCodeDetected: present, Action: skillActionNone}
	switch {
	case !present:
		plugin.Reason = "Claude Code not detected"
		return plugin
	case !r.pluginRouteAvailable():
		plugin.Reason = "this build names no plugin marketplace"
		return plugin
	}
	installed, known := claudePluginRecorded(r.claudeConfigDir(), r.pluginID)
	plugin.Installed = installed
	due := installed == dueWhenInstalled
	switch {
	case !known:
		plugin.Reason = "Claude Code plugin state unknown; run the commands yourself if needed"
		plugin.Commands = commandLines(steps())
	case !due && dueWhenInstalled:
		plugin.Reason = "not installed"
	case !due:
		plugin.Reason = "already installed"
	case !runnable:
		plugin.Reason = "'claude' is not on PATH in this shell; run the commands where it is"
		plugin.Commands = commandLines(steps())
	default:
		plugin.Action, plugin.Commands = actionWhenDue, commandLines(steps())
	}
	return plugin
}

func (r *skillRoutes) installPlan() (skillData, error) {
	data := skillData{Operation: "skill_install"}
	data.ClaudePlugin = r.pluginPlan(r.pluginInstallSteps, skillActionInstall, false)
	state, err := r.dirState()
	if err != nil {
		return data, err
	}
	directory := skillDirData{Path: r.dir(), State: state}
	switch state {
	case skillDirMissing:
		directory.Action = skillActionWrite
	case skillDirCurrent:
		directory.Action, directory.Reason = skillActionNone, "already current"
	default:
		directory.Action = skillActionReplace
	}
	data.AgentsDirectory = directory
	return data, nil
}

func (r *skillRoutes) uninstallPlan() (skillData, error) {
	data := skillData{Operation: "skill_uninstall"}
	data.ClaudePlugin = r.pluginPlan(r.pluginUninstallSteps, skillActionUninstall, true)
	state, err := r.dirState()
	if err != nil {
		return data, err
	}
	directory := skillDirData{Path: r.dir(), State: state}
	switch state {
	case skillDirCurrent, skillDirOutdated:
		directory.Action = skillActionRemove
	case skillDirModified:
		directory.Action, directory.Reason = skillActionNone, "modified locally, left in place"
	default:
		directory.Action, directory.Reason = skillActionNone, "not installed"
	}
	data.AgentsDirectory = directory
	return data, nil
}

// The directory route runs first in both directions: it cannot fail on an
// agent CLI, so a plugin failure never leaves the universal copy untouched.
func (r *skillRoutes) applyInstall(data *skillData) error {
	if data.AgentsDirectory.Action != skillActionNone {
		if err := r.writeDir(); err != nil {
			return fmt.Errorf("write %s: %w", r.dir(), err)
		}
		data.AgentsDirectory.State = skillDirCurrent
	}
	if data.ClaudePlugin.Action == skillActionInstall {
		if err := runPluginSteps(r.pluginInstallSteps()); err != nil {
			return err
		}
		data.ClaudePlugin.Installed = true
	}
	data.Applied = true
	return nil
}

func (r *skillRoutes) applyUninstall(data *skillData) error {
	if data.AgentsDirectory.Action == skillActionRemove {
		if err := os.RemoveAll(r.dir()); err != nil {
			return fmt.Errorf("remove %s: %w", r.dir(), err)
		}
		// Empty parents left behind would read as an agent to the doctor
		// check; removing a non-empty one fails, which is the intent.
		_ = os.Remove(filepath.Dir(r.dir()))
		_ = os.Remove(filepath.Dir(filepath.Dir(r.dir())))
		data.AgentsDirectory.State = skillDirMissing
	}
	if data.ClaudePlugin.Action == skillActionUninstall {
		if err := runPluginSteps(r.pluginUninstallSteps()); err != nil {
			return err
		}
		data.ClaudePlugin.Installed = false
	}
	data.Applied = true
	return nil
}

func runPluginSteps(steps [][]string) error {
	for i, step := range steps {
		if err := runAgentCommand(step); err != nil {
			return agentPluginError{step: step, remaining: steps[i:], err: err}
		}
	}
	return nil
}

func (r *skillRoutes) dirState() (string, error) {
	if !pathExists(r.dir()) {
		return skillDirMissing, nil
	}
	want, err := readSkillTree(r.skill)
	if err != nil {
		return "", fmt.Errorf("read bundled skill: %w", err)
	}
	got, err := readSkillTree(os.DirFS(r.dir()))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", r.dir(), err)
	}
	if skillDigest(want) == skillDigest(got) {
		return skillDirCurrent, nil
	}
	if marker, err := os.ReadFile(filepath.Join(r.dir(), skillMarkerFile)); err == nil {
		var recorded struct {
			Digest string `json:"digest"`
		}
		if json.Unmarshal(marker, &recorded) == nil && recorded.Digest == skillDigest(got) {
			return skillDirOutdated, nil
		}
	}
	return skillDirModified, nil
}

// readSkillTree skips dotfiles: the marker is Orbit's own, and anything else
// a desktop drops in (.DS_Store) is not part of the skill.
func readSkillTree(tree fs.FS) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(tree, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			return err
		}
		data, readErr := fs.ReadFile(tree, path)
		if readErr != nil {
			return readErr
		}
		files[path] = data
		return nil
	})
	return files, err
}

func skillDigest(files map[string][]byte) string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	sum := sha256.New()
	for _, path := range paths {
		sum.Write([]byte(path))
		sum.Write([]byte{0})
		sum.Write(files[path])
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// writeDir stages the copy beside its destination and swaps it in by rename,
// so an agent session starting mid-write never loads a half-written skill
// and a failed swap keeps the previous copy.
func (r *skillRoutes) writeDir() error {
	dir := r.dir()
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if stale, _ := filepath.Glob(filepath.Join(parent, ".orbit-skill-*")); len(stale) > 0 {
		for _, path := range stale {
			_ = os.RemoveAll(path)
		}
	}
	staging, err := os.MkdirTemp(parent, ".orbit-skill-")
	if err != nil {
		return err
	}
	discard := func(err error) error {
		_ = os.RemoveAll(staging)
		return err
	}
	if err := os.Chmod(staging, 0o755); err != nil {
		return discard(err)
	}
	if err := os.CopyFS(staging, r.skill); err != nil {
		return discard(err)
	}
	files, err := readSkillTree(r.skill)
	if err != nil {
		return discard(err)
	}
	marker, err := json.Marshal(struct {
		Digest string `json:"digest"`
	}{Digest: skillDigest(files)})
	if err != nil {
		return discard(err)
	}
	if err := os.WriteFile(filepath.Join(staging, skillMarkerFile), append(marker, '\n'), 0o644); err != nil {
		return discard(err)
	}
	previous := filepath.Join(parent, ".orbit-skill-previous")
	hadPrevious := pathExists(dir)
	if hadPrevious {
		if err := os.Rename(dir, previous); err != nil {
			return discard(err)
		}
	}
	if err := os.Rename(staging, dir); err != nil {
		if hadPrevious {
			_ = os.Rename(previous, dir)
		}
		return discard(err)
	}
	if hadPrevious {
		_ = os.RemoveAll(previous)
	}
	return nil
}

func printSkillResult(w io.Writer, data skillData, applyCommand string) {
	if data.Applied {
		fmt.Fprintln(w, "Orbit agent skill:")
	} else {
		fmt.Fprintln(w, "Orbit agent skill plan:")
	}
	plugin := data.ClaudePlugin
	switch {
	case plugin.Action == skillActionNone && len(plugin.Commands) > 0:
		fmt.Fprintf(w, "  Claude Code plugin: %s (%s)\n", plugin.Reason, strings.Join(plugin.Commands, "; "))
	case plugin.Action == skillActionNone:
		fmt.Fprintf(w, "  Claude Code plugin: %s\n", plugin.Reason)
	case data.Applied && plugin.Action == skillActionInstall:
		fmt.Fprintln(w, "  Claude Code plugin: installed; it loads in the next Claude Code session")
	case data.Applied && plugin.Action == skillActionUninstall:
		fmt.Fprintln(w, "  Claude Code plugin: uninstalled")
	default:
		fmt.Fprintf(w, "  Claude Code plugin: %s (%s)\n", plugin.Action, strings.Join(plugin.Commands, "; "))
	}
	directory := data.AgentsDirectory
	switch {
	case directory.Action == skillActionNone:
		fmt.Fprintf(w, "  %s: %s\n", directory.Path, directory.Reason)
	case data.Applied:
		fmt.Fprintf(w, "  %s: %s\n", directory.Path, skillActionDone[directory.Action])
	default:
		fmt.Fprintf(w, "  %s: %s (%s)\n", directory.Path, directory.Action, directory.State)
	}
	if !data.Applied {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Preview only. Run '%s' to apply.\n", applyCommand)
	}
}

var skillActionDone = map[string]string{
	skillActionWrite:   "written",
	skillActionReplace: "replaced",
	skillActionRemove:  "removed",
}

// directoryRouteDone reports whether apply already carried out the
// directory action, which the state reflects before the plugin route runs.
func directoryRouteDone(directory skillDirData) bool {
	switch directory.Action {
	case skillActionWrite, skillActionReplace:
		return directory.State == skillDirCurrent
	case skillActionRemove:
		return directory.State == skillDirMissing
	}
	return false
}

// agentPluginError names the failed agent command and the steps from it on,
// so the JSON error's replacement actions are exactly the commands still to
// run by hand rather than Orbit's own diagnostics.
type agentPluginError struct {
	step      []string
	remaining [][]string
	err       error
}

func (e agentPluginError) Error() string {
	return fmt.Sprintf("%s failed: %v", strings.Join(e.step, " "), e.err)
}

func (e agentPluginError) Unwrap() error { return e.err }

func (e agentPluginError) ErrorCode() string { return "agent_plugin_failed" }

func (e agentPluginError) CLIJSONHint() string {
	return "The plugin command ran and failed; see its output above, then run the remaining commands by hand and start a new agent session."
}

func (e agentPluginError) CLIJSONReplacementActions() []cli.JSONAction {
	actions := make([]cli.JSONAction, 0, len(e.remaining))
	for _, command := range commandLines(e.remaining) {
		actions = append(actions, cli.JSONAction{Command: command, Reason: "Run the plugin step Orbit could not complete."})
	}
	return actions
}

// claudePluginRecorded reads Claude Code's installed-plugin record. A record
// that does not exist means no plugin was ever installed; one that cannot be
// read or parsed is Claude Code's own business and reports unknown.
func claudePluginRecorded(configDir, pluginID string) (installed, known bool) {
	raw, err := os.ReadFile(filepath.Join(configDir, "plugins", "installed_plugins.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, true
	}
	if err != nil {
		return false, false
	}
	var record struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &record); err != nil || record.Plugins == nil {
		return false, false
	}
	_, installed = record.Plugins[pluginID]
	return installed, true
}
