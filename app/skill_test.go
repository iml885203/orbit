package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/extension"
)

var testSkill = fstest.MapFS{
	"SKILL.md":                {Data: []byte("---\nname: orbit\n---\n# Orbit\n")},
	"references/workflows.md": {Data: []byte("# Workflows\n")},
}

// skillFixture isolates the home directory (HOME and USERPROFILE so the
// Windows smoke job isolates too), starts with an empty PATH, and records
// every agent command instead of running it.
type skillFixture struct {
	home     string
	path     string
	commands []string
	fail     map[string]error
}

func newSkillFixture(t *testing.T) *skillFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	path := t.TempDir()
	t.Setenv("PATH", path)
	previous := distribution
	distribution = extension.Distribution{AgentSkill: testSkill, PluginMarketplace: "example/orbit", PluginID: "orbit@orbit"}
	t.Cleanup(func() { distribution = previous })

	f := &skillFixture{home: home, path: path, fail: map[string]error{}}
	previousRun := runAgentCommand
	runAgentCommand = func(argv []string) error {
		command := strings.Join(argv, " ")
		f.commands = append(f.commands, command)
		return f.fail[command]
	}
	t.Cleanup(func() { runAgentCommand = previousRun })
	return f
}

// withClaudeCode makes Claude Code present through its configuration
// directory only, the shape an agent shell without the user's PATH has.
func (f *skillFixture) withClaudeCode(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(f.home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// withClaudeCLI also puts a `claude` on PATH; the stub never runs because
// the fixture replaces runAgentCommand.
func (f *skillFixture) withClaudeCLI(t *testing.T) {
	t.Helper()
	f.withClaudeCode(t)
	for _, name := range []string{"claude", "claude.cmd"} {
		if err := os.WriteFile(filepath.Join(f.path, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *skillFixture) recordPlugin(t *testing.T, installed bool) {
	t.Helper()
	record := `{"version":2,"plugins":{}}`
	if installed {
		record = `{"version":2,"plugins":{"orbit@orbit":[{"scope":"user"}]}}`
	}
	f.writePluginRecord(t, record)
}

func (f *skillFixture) writePluginRecord(t *testing.T, record string) {
	t.Helper()
	dir := filepath.Join(f.home, ".claude", "plugins")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "installed_plugins.json"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *skillFixture) skillDir() string {
	return filepath.Join(f.home, ".agents", "skills", "orbit")
}

func (f *skillFixture) install(t *testing.T, yes bool) skillData {
	t.Helper()
	return captureSkillJSON(t, func() error {
		return runSkill(skillOptions{yes: yes}, (*skillRoutes).installPlan, (*skillRoutes).applyInstall, "orbit skill install --yes")
	})
}

func (f *skillFixture) uninstall(t *testing.T) skillData {
	t.Helper()
	return captureSkillJSON(t, func() error {
		return runSkill(skillOptions{yes: true}, (*skillRoutes).uninstallPlan, (*skillRoutes).applyUninstall, "orbit skill uninstall --yes")
	})
}

func useJSONOutput(t *testing.T) {
	t.Helper()
	previous := cli.JSONOutput
	cli.JSONOutput = true
	t.Cleanup(func() { cli.JSONOutput = previous })
}

func captureSkillJSON(t *testing.T, run func() error) skillData {
	t.Helper()
	useJSONOutput(t)
	output := captureStdout(t, run)
	var envelope struct {
		OK   bool      `json:"ok"`
		Data skillData `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("parse envelope: %v\n%s", err, output)
	}
	if !envelope.OK {
		t.Fatalf("envelope not ok:\n%s", output)
	}
	return envelope.Data
}

func captureStdout(t *testing.T, run func() error) string {
	t.Helper()
	output, err := captureStdoutAndError(t, run)
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, output)
	}
	return output
}

func captureStdoutAndError(t *testing.T, run func() error) (string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	runErr := run()
	os.Stdout = previous
	writer.Close()
	var output bytes.Buffer
	if _, err := io.Copy(&output, reader); err != nil {
		t.Fatal(err)
	}
	return output.String(), runErr
}

func TestSkillInstallWritesUniversalDirectoryWithoutClaudeCode(t *testing.T) {
	f := newSkillFixture(t)

	data := f.install(t, true)

	if data.Operation != "skill_install" || !data.Applied {
		t.Fatalf("data = %+v", data)
	}
	if data.ClaudePlugin.ClaudeCodeDetected || data.ClaudePlugin.Action != skillActionNone {
		t.Fatalf("plugin route ran without Claude Code: %+v", data.ClaudePlugin)
	}
	if data.AgentsDirectory.Action != skillActionWrite || data.AgentsDirectory.State != skillDirCurrent {
		t.Fatalf("directory = %+v", data.AgentsDirectory)
	}
	got, err := os.ReadFile(filepath.Join(f.skillDir(), "references", "workflows.md"))
	if err != nil || string(got) != "# Workflows\n" {
		t.Fatalf("installed reference = %q, %v", got, err)
	}
	if len(f.commands) != 0 {
		t.Fatalf("agent commands ran without Claude Code: %v", f.commands)
	}
}

func TestSkillInstallAddsClaudePluginAndIsIdempotent(t *testing.T) {
	f := newSkillFixture(t)
	f.withClaudeCLI(t)

	first := f.install(t, true)
	if first.ClaudePlugin.Action != skillActionInstall || !first.ClaudePlugin.Installed {
		t.Fatalf("first plugin = %+v", first.ClaudePlugin)
	}
	want := "claude plugin marketplace add example/orbit\nclaude plugin install orbit@orbit"
	if strings.Join(f.commands, "\n") != want {
		t.Fatalf("commands = %v", f.commands)
	}

	f.commands = nil
	f.recordPlugin(t, true)
	second := f.install(t, true)
	if second.ClaudePlugin.Action != skillActionNone || second.AgentsDirectory.Action != skillActionNone {
		t.Fatalf("second run was not a no-op: %+v", second)
	}
	if len(f.commands) != 0 {
		t.Fatalf("second run ran %v", f.commands)
	}
}

// Claude Code's configuration directory exists but its CLI is not on PATH,
// the shape an agent shell often has: the plan must say so and hand over the
// commands, and the universal copy must still be written.
func TestSkillInstallHandsOverPluginCommandsWhenClaudeIsNotOnPath(t *testing.T) {
	f := newSkillFixture(t)
	f.withClaudeCode(t)

	data := f.install(t, true)

	plugin := data.ClaudePlugin
	if !plugin.ClaudeCodeDetected || plugin.Action != skillActionNone || !strings.Contains(plugin.Reason, "not on PATH") || len(plugin.Commands) != 2 {
		t.Fatalf("plugin = %+v", plugin)
	}
	if !data.Applied || data.AgentsDirectory.State != skillDirCurrent {
		t.Fatalf("directory = %+v", data.AgentsDirectory)
	}
	if len(f.commands) != 0 {
		t.Fatalf("agent commands ran without claude on PATH: %v", f.commands)
	}
}

func TestSkillInstallReplacesOutdatedCopyAndPreviewChangesNothing(t *testing.T) {
	f := newSkillFixture(t)
	if err := os.MkdirAll(f.skillDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(f.skillDir(), "SKILL.md")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	preview := f.install(t, false)
	if preview.Applied || preview.AgentsDirectory.State != skillDirModified || preview.AgentsDirectory.Action != skillActionReplace {
		t.Fatalf("preview = %+v", preview)
	}
	if got, _ := os.ReadFile(stale); string(got) != "old" {
		t.Fatal("preview rewrote the skill directory")
	}

	applied := f.install(t, true)
	if !applied.Applied || applied.AgentsDirectory.State != skillDirCurrent {
		t.Fatalf("applied = %+v", applied)
	}
	if got, _ := os.ReadFile(stale); !strings.Contains(string(got), "name: orbit") {
		t.Fatalf("SKILL.md after install = %q", got)
	}
}

// A copy written by an older Orbit is outdated, not modified: a newer CLI
// replaces it, uninstall still removes it, and a desktop's dotfiles do not
// make it look edited.
func TestSkillOutdatedCopyIsRecognizedAndStillRemovable(t *testing.T) {
	f := newSkillFixture(t)
	f.install(t, true)
	if err := os.WriteFile(filepath.Join(f.skillDir(), ".DS_Store"), []byte("finder"), 0o644); err != nil {
		t.Fatal(err)
	}
	distribution = extension.Distribution{AgentSkill: fstest.MapFS{
		"SKILL.md": {Data: []byte("---\nname: orbit\n---\n# Orbit, newer\n")},
	}}

	preview := f.install(t, false)
	if preview.AgentsDirectory.State != skillDirOutdated || preview.AgentsDirectory.Action != skillActionReplace {
		t.Fatalf("older copy = %+v", preview.AgentsDirectory)
	}
	removed := f.uninstall(t)
	if removed.AgentsDirectory.Action != skillActionRemove || pathExists(f.skillDir()) {
		t.Fatalf("outdated copy was not removed: %+v", removed.AgentsDirectory)
	}
}

func TestSkillInstallWritesDirectoryBeforePluginFailureAndReportsCommands(t *testing.T) {
	f := newSkillFixture(t)
	f.withClaudeCLI(t)
	f.fail["claude plugin marketplace add example/orbit"] = errors.New("exit status 1")

	useJSONOutput(t)
	output, err := captureStdoutAndError(t, func() error {
		return runSkill(skillOptions{yes: true}, (*skillRoutes).installPlan, (*skillRoutes).applyInstall, "orbit skill install --yes")
	})
	if err == nil {
		t.Fatal("plugin failure was swallowed")
	}
	if !pathExists(filepath.Join(f.skillDir(), "SKILL.md")) {
		t.Fatal("universal copy was not written before the plugin step failed")
	}

	var result struct {
		OK    bool      `json:"ok"`
		Data  skillData `json:"data"`
		Error struct {
			Code        string `json:"code"`
			Hint        string `json:"hint"`
			NextCommand string `json:"next_command"`
		} `json:"error"`
		Actions []cli.JSONAction `json:"recommended_actions"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("parse failure envelope: %v\n%s", err, output)
	}
	if result.OK || result.Error.Code != "agent_plugin_failed" || !strings.Contains(result.Error.Hint, "ran and failed") {
		t.Fatalf("envelope = %s", output)
	}
	if result.Data.AgentsDirectory.State != skillDirCurrent || result.Data.ClaudePlugin.Installed {
		t.Fatalf("partial result not reported: %+v", result.Data)
	}
	if len(result.Actions) != 2 || result.Actions[1].Command != "claude plugin install orbit@orbit" || result.Error.NextCommand != result.Actions[0].Command {
		t.Fatalf("recommended actions = %+v, next = %q", result.Actions, result.Error.NextCommand)
	}
}

func TestSkillPluginFailureRecommendsOnlyTheStepsLeft(t *testing.T) {
	f := newSkillFixture(t)
	f.withClaudeCLI(t)
	f.fail["claude plugin install orbit@orbit"] = errors.New("exit status 1")

	err := runSkill(skillOptions{yes: true}, (*skillRoutes).installPlan, (*skillRoutes).applyInstall, "orbit skill install --yes")
	var pluginErr agentPluginError
	if !errors.As(err, &pluginErr) {
		t.Fatalf("err = %v", err)
	}
	actions := pluginErr.CLIJSONReplacementActions()
	if len(actions) != 1 || actions[0].Command != "claude plugin install orbit@orbit" {
		t.Fatalf("a completed marketplace step was recommended again: %+v", actions)
	}
}

func TestSkillUninstallReversesInstallAndLeavesModifiedCopy(t *testing.T) {
	f := newSkillFixture(t)
	f.withClaudeCLI(t)
	f.install(t, true)
	f.recordPlugin(t, true)
	f.commands = nil

	removed := f.uninstall(t)
	if removed.Operation != "skill_uninstall" || removed.ClaudePlugin.Action != skillActionUninstall || removed.AgentsDirectory.State != skillDirMissing {
		t.Fatalf("uninstall = %+v", removed)
	}
	if strings.Join(f.commands, "\n") != "claude plugin uninstall orbit@orbit" {
		t.Fatalf("commands = %v", f.commands)
	}
	if pathExists(f.skillDir()) {
		t.Fatal("skill directory still exists after uninstall")
	}

	if err := os.MkdirAll(f.skillDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.skillDir(), "SKILL.md"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.recordPlugin(t, false)
	kept := f.uninstall(t)
	if kept.AgentsDirectory.Action != skillActionNone || kept.AgentsDirectory.State != skillDirModified {
		t.Fatalf("modified copy = %+v", kept.AgentsDirectory)
	}
	if !pathExists(f.skillDir()) {
		t.Fatal("uninstall removed a locally modified copy")
	}
}

func TestSkillUninstallLeavesNoEmptyParentsForDoctorToMisread(t *testing.T) {
	f := newSkillFixture(t)
	f.install(t, true)
	f.uninstall(t)

	if pathExists(filepath.Join(f.home, ".agents")) {
		t.Fatal("uninstall left an empty ~/.agents behind")
	}
	if check := agentSkillDoctorCheck(); check.Status != daemon.CheckInfo {
		t.Fatalf("doctor after uninstall on a machine without agents: %+v", check)
	}
}

func TestSkillUninstallHandsOverThePluginWhenItsRecordIsUnreadable(t *testing.T) {
	f := newSkillFixture(t)
	f.withClaudeCLI(t)
	f.writePluginRecord(t, "{not json")

	data := f.uninstall(t)

	plugin := data.ClaudePlugin
	if plugin.Action != skillActionNone || !strings.Contains(plugin.Reason, "unknown") || len(plugin.Commands) != 1 {
		t.Fatalf("plugin = %+v", plugin)
	}
	if len(f.commands) != 0 {
		t.Fatalf("uninstall acted on a guessed plugin state: %v", f.commands)
	}
}

func TestSkillRoutesSkipThePluginWhenTheBuildNamesNoMarketplace(t *testing.T) {
	f := newSkillFixture(t)
	f.withClaudeCLI(t)
	f.recordPlugin(t, true)
	distribution = extension.Distribution{AgentSkill: testSkill}

	for _, data := range []skillData{f.install(t, true), f.uninstall(t)} {
		if data.ClaudePlugin.Action != skillActionNone || !strings.Contains(data.ClaudePlugin.Reason, "no plugin marketplace") {
			t.Fatalf("%s plugin = %+v", data.Operation, data.ClaudePlugin)
		}
	}
	if len(f.commands) != 0 {
		t.Fatalf("agent commands ran without a marketplace: %v", f.commands)
	}
}

func TestSkillPreviewPrintsPlanForHumans(t *testing.T) {
	f := newSkillFixture(t)
	f.withClaudeCLI(t)

	output := captureStdout(t, func() error {
		return runSkill(skillOptions{}, (*skillRoutes).installPlan, (*skillRoutes).applyInstall, "orbit skill install --yes")
	})
	for _, want := range []string{
		"Claude Code plugin: install (claude plugin marketplace add example/orbit; claude plugin install orbit@orbit)",
		f.skillDir() + ": write (missing)",
		"Preview only. Run 'orbit skill install --yes' to apply.",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("preview lacks %q:\n%s", want, output)
		}
	}
	if pathExists(f.skillDir()) {
		t.Fatal("preview wrote the skill directory")
	}
}

func TestSkillCommandWiresYesThroughCobra(t *testing.T) {
	f := newSkillFixture(t)

	data := captureSkillJSON(t, func() error {
		cmd := skillCmd()
		cmd.SetArgs([]string{"install", "--yes"})
		return cmd.Execute()
	})

	if !data.Applied || !pathExists(filepath.Join(f.skillDir(), "SKILL.md")) {
		t.Fatalf("cobra wiring did not apply: %+v", data)
	}
}

func TestSkillInstallWithoutBundledSkillExplains(t *testing.T) {
	newSkillFixture(t)
	distribution = extension.Distribution{}

	err := runSkill(skillOptions{yes: true}, (*skillRoutes).installPlan, (*skillRoutes).applyInstall, "orbit skill install --yes")
	if err == nil || !strings.Contains(err.Error(), "bundles no agent skill") {
		t.Fatalf("err = %v", err)
	}
}

func TestAgentSkillDoctorCheck(t *testing.T) {
	f := newSkillFixture(t)

	if check := agentSkillDoctorCheck(); check.Status != daemon.CheckInfo {
		t.Fatalf("no agent present: %+v", check)
	}

	// Never having installed the plugin is information with the remedy,
	// not a warning that turns a healthy doctor yellow.
	f.withClaudeCode(t)
	check := agentSkillDoctorCheck()
	if check.Status != daemon.CheckInfo || !strings.Contains(check.Message, "plugin not installed ('claude' is not on PATH") || check.Hint != "run: orbit skill install --yes" {
		t.Fatalf("plugin never installed: %+v", check)
	}

	f.recordPlugin(t, true)
	check = agentSkillDoctorCheck()
	if check.Status != daemon.CheckPass || !strings.Contains(check.Message, "plugin installed") {
		t.Fatalf("plugin installed, no universal agent: %+v", check)
	}

	// An unreadable record is context: the remedy could never clear it.
	f.writePluginRecord(t, "{not json")
	check = agentSkillDoctorCheck()
	if check.Status != daemon.CheckPass || !strings.Contains(check.Message, "plugin state unknown") {
		t.Fatalf("record unreadable: %+v", check)
	}

	// A universal directory other tools populated is a nudge, not drift:
	// nothing of Orbit's is there to be stale.
	f.recordPlugin(t, true)
	if err := os.MkdirAll(filepath.Join(f.home, ".agents", "skills", "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	check = agentSkillDoctorCheck()
	if check.Status != daemon.CheckInfo || !strings.Contains(check.Message, "is missing") || check.Hint != "run: orbit skill install --yes" {
		t.Fatalf("universal directory without Orbit's copy: %+v", check)
	}
	f.install(t, true)
	if check = agentSkillDoctorCheck(); check.Status != daemon.CheckPass {
		t.Fatalf("installed everywhere: %+v", check)
	}
	if err := os.WriteFile(filepath.Join(f.skillDir(), "SKILL.md"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	check = agentSkillDoctorCheck()
	if check.Status != daemon.CheckWarn || !strings.Contains(check.Message, "is modified") {
		t.Fatalf("edited copy: %+v", check)
	}
}

func TestSkillHonorsClaudeConfigDir(t *testing.T) {
	f := newSkillFixture(t)
	configDir := filepath.Join(t.TempDir(), "claude-config")
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	if err := os.MkdirAll(filepath.Join(configDir, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := `{"version":2,"plugins":{"orbit@orbit":[{"scope":"user"}]}}`
	if err := os.WriteFile(filepath.Join(configDir, "plugins", "installed_plugins.json"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}

	data := f.install(t, false)

	if !data.ClaudePlugin.ClaudeCodeDetected || !data.ClaudePlugin.Installed {
		t.Fatalf("CLAUDE_CONFIG_DIR ignored: %+v", data.ClaudePlugin)
	}
}

func TestSkillUninstallPreviewChangesNothing(t *testing.T) {
	f := newSkillFixture(t)
	f.withClaudeCLI(t)
	f.install(t, true)
	f.recordPlugin(t, true)
	f.commands = nil

	preview := captureSkillJSON(t, func() error {
		return runSkill(skillOptions{}, (*skillRoutes).uninstallPlan, (*skillRoutes).applyUninstall, "orbit skill uninstall --yes")
	})

	if preview.Applied || preview.ClaudePlugin.Action != skillActionUninstall || preview.AgentsDirectory.Action != skillActionRemove {
		t.Fatalf("preview = %+v", preview)
	}
	if len(f.commands) != 0 || !pathExists(filepath.Join(f.skillDir(), "SKILL.md")) {
		t.Fatalf("uninstall preview acted: commands=%v", f.commands)
	}
}

func TestDoctorActionsNeverPromoteTheAgentSkillRemedy(t *testing.T) {
	resp := &daemon.DoctorResponse{Checks: []daemon.DoctorCheck{
		{Name: "Daemon", Status: daemon.CheckPass, Message: "All healthy (1/1)"},
		{Name: agentSkillCheckName, Status: daemon.CheckWarn, Message: "missing", Hint: "run: orbit skill install --yes"},
	}}
	for _, action := range doctorRecommendedActions(resp) {
		if strings.Contains(action.Command, "orbit skill") {
			t.Fatalf("agent skill hint was promoted to an action: %+v", action)
		}
	}
}
