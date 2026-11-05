package app

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/envsource"
	"github.com/iml885203/orbit/internal/envsync"
)

func TestSourceHelpTeachesTheSmallCommandSet(t *testing.T) {
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
	} {
		if !strings.Contains(help, want) {
			t.Errorf("source help missing %q:\n%s", want, help)
		}
	}
	for _, removed := range []string{"info", "update", "set-default", "set-workspace", "clear-workspace"} {
		if _, _, err := command.Find([]string{removed}); err == nil {
			t.Errorf("removed source command %q is still available", removed)
		}
	}
}

func TestSourcePrimarySubcommandHelpExplainsBehaviorBeforeExecution(t *testing.T) {
	for _, test := range []struct {
		name string
		want []string
	}{
		{name: "add", want: []string{"Exactly one of --url or --path", "validates and synchronizes", "first source"}},
		{name: "sync", want: []string{"first source", "--all", "--dry"}},
		{name: "remove", want: []string{"running environment", "clears that selection", "Orbit chooses the", "never deletes"}},
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

func TestSourceRemoveFirstMakesTheNextSourceFirst(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	registry, err := envsource.Load(envsource.RegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "first", Type: envsource.TypeLocal, Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(envsource.Source{Name: "second", Type: envsource.TypeLocal, Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}

	command := sourceRemoveCmd()
	command.SetArgs([]string{"first"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := envsource.Load(envsource.RegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	firstSource, err := reloaded.First()
	if err != nil || firstSource.Name != "second" {
		t.Fatalf("first after removal = %#v, %v", firstSource, err)
	}
}

func TestSourceSyncWithoutNameSynchronizesFirstSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ORBIT_HOME", home)
	useJSONOutput(t)
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
	if err := registry.Add(envsource.Source{Name: "local", Type: envsource.TypeLocal, Path: sourceRoot}); err != nil {
		t.Fatal(err)
	}

	finishCapture := captureLifecycleProcessStreams(t)
	command := sourceSyncCmd()
	if err := command.RunE(command, nil); err != nil {
		t.Fatal(err)
	}
	_, _ = finishCapture()
	if _, err := os.Stat(filepath.Join(envsource.EnvsDir(home, "local"), "development.yaml")); err != nil {
		t.Fatalf("source sync without a name did not sync the first source: %v", err)
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

func TestFailedSourceAddRetriesTheAddWithoutCredentials(t *testing.T) {
	source := envsource.Source{
		Name: "team",
		Type: envsource.TypeGit,
		URL:  "https://user:s3cret-token@github.com/example/private-env.git",
		Ref:  "main",
	}
	failure := firstSyncError(source, &envsync.CloneError{URL: source.URL, Err: errors.New("exit status 128")}, sourceAddCommand(source))

	var buf bytes.Buffer
	if err := cli.WriteJSONFailure(&buf, "orbit source add --json", nil, failure, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "s3cret-token") {
		t.Fatalf("credential leaked into JSON:\n%s", buf.String())
	}
	envelope := decodeEnvelope(t, buf.Bytes())
	if envelope.Error == nil || envelope.Error.Code != "env_repo_access" {
		t.Fatalf("error = %+v", envelope.Error)
	}
	retry := "orbit source add team --url https://github.com/example/private-env.git --ref main"
	commands := make([]string, 0, len(envelope.RecommendedActions))
	for _, action := range envelope.RecommendedActions {
		commands = append(commands, action.Command)
	}
	want := []string{retry + " --json", "gh auth login", "gh auth setup-git"}
	if strings.Join(commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("recommended_actions = %q, want %q", commands, want)
	}
	if envelope.Error.NextCommand != retry+" --json" {
		t.Fatalf("next_command = %q", envelope.Error.NextCommand)
	}
	if !strings.Contains(envelope.Error.Message, "retry `"+retry+"`") ||
		strings.Contains(envelope.Error.Message, "orbit source sync") {
		t.Fatalf("message = %q", envelope.Error.Message)
	}

	var human bytes.Buffer
	printExecutionError(&human, failure)
	if strings.Contains(human.String(), "s3cret-token") || strings.Contains(human.String(), "orbit source sync") ||
		!strings.Contains(human.String(), "Next: "+retry+"\n") {
		t.Fatalf("human output:\n%s", human.String())
	}
}

func TestFailedSourceAddWithoutGitRetriesTheAdd(t *testing.T) {
	source := envsource.Source{Name: "team", Type: envsource.TypeGit, URL: "https://git.example.com/env.git", Workspace: "/work/my app"}
	failure := firstSyncError(source, &envsync.CloneError{URL: source.URL, Err: exec.ErrNotFound}, sourceAddCommand(source))

	var buf bytes.Buffer
	if err := cli.WriteJSONFailure(&buf, "orbit source add --json", nil, failure, nil); err != nil {
		t.Fatal(err)
	}
	envelope := decodeEnvelope(t, buf.Bytes())
	retry := "orbit source add team --url https://git.example.com/env.git --workspace '/work/my app'"
	if len(envelope.RecommendedActions) != 1 || envelope.RecommendedActions[0].Command != retry+" --json" {
		t.Fatalf("recommended_actions = %+v", envelope.RecommendedActions)
	}
	if !strings.Contains(envelope.Error.Message, "Install Git") || !strings.Contains(envelope.Error.Message, "retry `"+retry+"`") {
		t.Fatalf("message = %q", envelope.Error.Message)
	}
}
