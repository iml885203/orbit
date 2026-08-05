package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/envsource"
)

func TestSourceHelpTeachesTheCommonWorkflowAndHidesCompatibilityAliases(t *testing.T) {
	command := sourceCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--help"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	help := output.String()
	for _, want := range []string{
		"A source is a Git repository or local directory",
		"orbit source add company",
		"orbit source sync --all",
		"orbit switch company/development",
		"Advanced configuration uses \"orbit source update\"",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("source help missing %q:\n%s", want, help)
		}
	}
	for _, alias := range []string{"set-default", "set-workspace", "clear-workspace"} {
		if strings.Contains(help, "  "+alias+" ") {
			t.Errorf("compatibility alias %q appears as a primary command:\n%s", alias, help)
		}
		found, _, err := command.Find([]string{alias})
		if err != nil {
			t.Fatalf("compatibility alias %q is unavailable: %v", alias, err)
		}
		if !found.Hidden || found.Deprecated == "" {
			t.Errorf("compatibility alias %q hidden=%v deprecated=%q", alias, found.Hidden, found.Deprecated)
		}
	}
}

func TestSourcePrimarySubcommandHelpExplainsBehaviorBeforeExecution(t *testing.T) {
	for _, test := range []struct {
		name string
		want []string
	}{
		{name: "add", want: []string{"Exactly one of --url or --path", "validates and synchronizes", "first source"}},
		{name: "sync", want: []string{"default source", "--all", "--dry"}},
		{name: "update", want: []string{"metadata only", "do not access the source", "Conflicting flags"}},
		{name: "remove", want: []string{"running environment", "clears that selection", "never deletes a local source"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := sourceCmd()
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&output)
			command.SetArgs([]string{test.name, "--help"})
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			for _, want := range test.want {
				if !strings.Contains(output.String(), want) {
					t.Errorf("%s help missing %q:\n%s", test.name, want, output.String())
				}
			}
		})
	}
}

func TestSourceUpdateMetadataDoesNotRefreshContent(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	registry, err := envsource.Load(envsource.RegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	firstPath := t.TempDir()
	targetPath := t.TempDir()
	if err := registry.Add(envsource.Source{Name: "first", Type: envsource.TypeLocal, Path: firstPath}, false); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "target", Type: envsource.TypeLocal, Path: targetPath}, false); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(targetPath); err != nil {
		t.Fatal(err)
	}

	command := sourceUpdateCmd()
	command.SetArgs([]string{"target", "--workspace", workspace, "--default"})
	if err := command.Execute(); err != nil {
		t.Fatalf("metadata-only update accessed unavailable source content: %v", err)
	}

	reloaded, err := envsource.Load(envsource.RegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := reloaded.Get("target")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Workspace != workspace || !updated.Default {
		t.Fatalf("updated source = %#v", updated)
	}
}

func TestSourceUpdateRejectsConflictsBeforeMutation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	registry, err := envsource.Load(envsource.RegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := t.TempDir()
	if err := registry.Add(envsource.Source{Name: "local", Type: envsource.TypeLocal, Path: sourcePath}, false); err != nil {
		t.Fatal(err)
	}

	command := sourceUpdateCmd()
	command.SetArgs([]string{"local", "--workspace", t.TempDir(), "--clear-workspace"})
	if err := command.Execute(); err == nil {
		t.Fatal("conflicting workspace flags unexpectedly succeeded")
	}
	reloaded, err := envsource.Load(envsource.RegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := reloaded.Get("local")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Workspace != "" {
		t.Fatalf("workspace mutated despite conflict: %q", unchanged.Workspace)
	}
}

func TestSourceRemoveDefaultReturnsExecutableDiscoveryAction(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	registry, err := envsource.Load(envsource.RegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "first", Type: envsource.TypeLocal, Path: t.TempDir()}, false); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "second", Type: envsource.TypeLocal, Path: t.TempDir()}, false); err != nil {
		t.Fatal(err)
	}

	command := sourceRemoveCmd()
	command.SetArgs([]string{"first"})
	err = command.Execute()
	if err == nil {
		t.Fatal("default source removal unexpectedly succeeded")
	}
	withActions, ok := err.(interface{ CLIJSONReplacementActions() []cli.JSONAction })
	if !ok {
		t.Fatalf("error has no replacement action: %T %v", err, err)
	}
	actions := withActions.CLIJSONReplacementActions()
	if len(actions) != 1 || actions[0].Command != "orbit source list --json" {
		t.Fatalf("replacement actions = %#v", actions)
	}
}

func TestEnvSyncWithoutRepositoryFlagsSynchronizesDefaultSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	resetEnvSyncFlags(t)
	sourceRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sourceRoot, "envs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "envs", "development.yaml"), []byte("version: \"3\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := envsource.Load(envsource.RegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "local", Type: envsource.TypeLocal, Path: sourceRoot}, false); err != nil {
		t.Fatal(err)
	}

	command := envSyncCmd()
	var stderr bytes.Buffer
	command.SetErr(&stderr)
	if err := command.RunE(command, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "deprecated") || !strings.Contains(stderr.String(), "orbit source sync") {
		t.Fatalf("deprecation warning = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(envsource.EnvsDir(home, "local"), "development.yaml")); err != nil {
		t.Fatalf("legacy command did not perform a real sync: %v", err)
	}
}

func TestLegacySourceMigrationAddsOneStructuredNotice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	settings := daemon.LoadSettings(daemon.DefaultSettingsPath())
	if err := settings.Set("env_repo_url", "https://example.com/environments.git"); err != nil {
		t.Fatal(err)
	}
	legacyEnvs := envsDestDir()
	if err := os.MkdirAll(legacyEnvs, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyEnvs, "development.yaml"), []byte("version: \"3\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cli.JSONOutput = true
	t.Cleanup(func() { cli.JSONOutput = false })

	if _, err := sourceRegistry(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := cli.WriteJSONSuccess(&output, "orbit source list --json", map[string]any{"operation": "source_list"}, nil); err != nil {
		t.Fatal(err)
	}
	envelope := decodeEnvelope(t, output.Bytes())
	if len(envelope.Notices) != 1 || envelope.Notices[0].Code != "environment_source_migrated" {
		t.Fatalf("migration notices = %#v", envelope.Notices)
	}
	data, ok := envelope.Notices[0].Data.(map[string]any)
	if !ok || data["source_name"] != "default" || data["offline"] != true || data["cached_environments"] != float64(1) {
		t.Fatalf("migration notice data = %#v", envelope.Notices[0].Data)
	}

	if _, err := sourceRegistry(); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := cli.WriteJSONSuccess(&output, "orbit source list --json", map[string]any{"operation": "source_list"}, nil); err != nil {
		t.Fatal(err)
	}
	if repeated := decodeEnvelope(t, output.Bytes()).Notices; len(repeated) != 0 {
		t.Fatalf("repeated migration notices = %#v", repeated)
	}
}

func TestEnvSyncJSONKeepsDeprecationMetadataInsideTheEnvelope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	resetEnvSyncFlags(t)
	cli.JSONOutput = true
	sourceRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sourceRoot, "envs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "envs", "development.yaml"), []byte("version: \"3\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := envsource.Load(envsource.RegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "local", Type: envsource.TypeLocal, Path: sourceRoot}, false); err != nil {
		t.Fatal(err)
	}

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previousStdout := os.Stdout
	os.Stdout = write
	t.Cleanup(func() { os.Stdout = previousStdout })
	command := envSyncCmd()
	var stderr bytes.Buffer
	command.SetErr(&stderr)
	runErr := command.RunE(command, nil)
	if closeErr := write.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	os.Stdout = previousStdout
	if runErr != nil {
		t.Fatal(runErr)
	}
	var output bytes.Buffer
	if _, err := output.ReadFrom(read); err != nil {
		t.Fatal(err)
	}
	envelope := decodeEnvelope(t, output.Bytes())
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %#v", envelope.Data)
	}
	if data["deprecated_command"] != "orbit env sync" || data["replacement_command"] != "orbit source sync" {
		t.Fatalf("deprecation metadata = %#v", data)
	}
	if !strings.Contains(stderr.String(), "deprecated") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestLegacyEnvSyncFlagsFailWithExecutableAction(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	resetEnvSyncFlags(t)
	registry, err := envsource.Load(envsource.RegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "company", Type: envsource.TypeGit, URL: "https://example.com/old.git"}, false); err != nil {
		t.Fatal(err)
	}
	envSyncURL = "https://example.com/new repo.git"
	envSyncRef = "release candidate"
	err = legacyEnvSyncSourceChange(true, false, true)
	if err == nil {
		t.Fatal("legacy repository flags unexpectedly succeeded")
	}
	var output bytes.Buffer
	if writeErr := cli.WriteJSONError(&output, "orbit env sync --url ... --json", err); writeErr != nil {
		t.Fatal(writeErr)
	}
	envelope := decodeEnvelope(t, output.Bytes())
	if len(envelope.RecommendedActions) != 1 {
		t.Fatalf("recommended actions = %+v", envelope.RecommendedActions)
	}
	action := envelope.RecommendedActions[0].Command
	if strings.Contains(action, "<") || strings.Contains(action, ">") {
		t.Fatalf("recommended action contains unresolved placeholder: %q", action)
	}
	for _, want := range []string{"orbit source update company", "--url", "--ref", "--json"} {
		if !strings.Contains(action, want) {
			t.Errorf("recommended action %q missing %q", action, want)
		}
	}
}

func TestEnvSyncHelpIsOnlyAMigrationBridge(t *testing.T) {
	command := envSyncCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--help"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	help := output.String()
	for _, want := range []string{"Deprecated", "orbit source sync", "Old to new", "no longer mutate"} {
		if !strings.Contains(help, want) {
			t.Errorf("env sync help missing %q:\n%s", want, help)
		}
	}
	for _, removed := range []string{"use and remember", "pin and remember", "without remembering"} {
		if strings.Contains(help, removed) {
			t.Fatalf("env sync help retains removed behavior %q:\n%s", removed, help)
		}
	}
}

func resetEnvSyncFlags(t *testing.T) {
	t.Helper()
	previous := []any{envSyncURL, envSyncRef, envSyncPath, envSyncDryRun, envSyncYes, envSyncNoApply, cli.JSONOutput}
	envSyncURL, envSyncRef, envSyncPath = "", "", ""
	envSyncDryRun, envSyncYes, envSyncNoApply, cli.JSONOutput = false, false, false, false
	t.Cleanup(func() {
		envSyncURL = previous[0].(string)
		envSyncRef = previous[1].(string)
		envSyncPath = previous[2].(string)
		envSyncDryRun = previous[3].(bool)
		envSyncYes = previous[4].(bool)
		envSyncNoApply = previous[5].(bool)
		cli.JSONOutput = previous[6].(bool)
	})
}

func TestLegacyEnvSyncConflictsAreInvalidArguments(t *testing.T) {
	resetEnvSyncFlags(t)
	envSyncURL = "https://example.com/environments.git"
	envSyncPath = "/tmp/environments"
	err := legacyEnvSyncSourceChange(true, true, false)
	if !errors.Is(err, cli.ErrInvalidArgument) {
		t.Fatalf("error = %v, want invalid argument", err)
	}
}

func TestLegacyEnvSyncDoesNotSilentlyDropEmptyOrSyncModifierFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--url", ""},
		{"--url", "https://example.com/new.git", "--dry"},
		{"--ref", "release", "--no-apply"},
		{"--path", t.TempDir(), "--yes"},
	} {
		resetEnvSyncFlags(t)
		t.Setenv("ORBIT_HOME", t.TempDir())
		command := envSyncCmd()
		command.SetArgs(args)
		if err := command.Execute(); err == nil {
			t.Fatalf("legacy args %q unexpectedly succeeded", args)
		}
	}
}
