package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/envsource"
	"github.com/iml885203/orbit/internal/envsync"
	"github.com/iml885203/orbit/internal/shellquote"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

func sourceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "source",
		Short: "Manage shared environment sources",
		Long: `Manage sources that provide shared environment configurations.

A source is a Git repository or local directory containing envs/. Add it once,
then sync it whenever you want the latest environments. Bare environment names
come from the first source; use <source>/<environment> for another source.

Common workflow:
  orbit source add company --url https://example.com/environments.git
  orbit switch company/development
  orbit source list
  orbit source remove company

Refresh later:
  orbit source sync                 # sync the first source
  orbit source sync company         # sync one named source
  orbit source sync --all           # explicitly sync every source

Local quick start:
  orbit source add local --path /path/to/environment-repo
  orbit switch local/development`,
	}
	cmd.AddCommand(sourceAddCmd())
	cmd.AddCommand(sourceListCmd())
	cmd.AddCommand(sourceSyncCmd())
	cmd.AddCommand(sourceRemoveCmd())
	return cmd
}

func sourceRegistry() (*envsource.Registry, error) {
	registry, err := envsource.Load(envsource.RegistryPath(daemon.OrbitDir()))
	if err != nil || len(registry.List()) > 0 {
		return registry, err
	}
	registry, migration, err := migrateLegacyEnvironmentSource(registry)
	if err != nil || migration == nil {
		return registry, err
	}
	if cli.JSONOutput {
		cli.AddJSONNotice(cli.JSONNotice{
			Code:    "environment_source_migrated",
			Message: "Migrated the legacy environment repository to the source named \"default\" without a network sync.",
			Data:    migration,
		})
	} else {
		fmt.Fprintln(os.Stderr, sourceMigrationSummary(migration))
	}
	return registry, nil
}

func migrateLegacyEnvironmentSource(registry *envsource.Registry) (*envsource.Registry, *envsource.LegacyMigrationResult, error) {
	settings := daemon.LoadSettings(daemon.DefaultSettingsPath())
	legacySelection := readCurrentEnv()
	url := settings.Get(settingKeyEnvRepoURL)
	ref := settings.Get(settingKeyEnvRepoRef)
	provenance, _ := envsync.ReadRepositorySource(envsDestDir())
	if url == "" {
		url = provenance.URL
	}
	if ref == "" {
		ref = provenance.Ref
	}
	if url == "" {
		url = os.Getenv("ORBIT_ENV_REPO_URL")
	}
	if url == "" {
		return registry, nil, nil
	}
	return envsource.LoadMigratingLegacyWithResult(daemon.OrbitDir(), envsource.LegacyMigration{
		URL: url, Ref: ref, Workspace: settings.Get("workspace_root"), EnvsDir: envsDestDir(),
		Selection: legacySelection, SelectionFile: filepath.Join(daemon.OrbitDir(), "current"),
		Clear: settings.ClearLegacyEnvironmentSettings,
	})
}

func sourceMigrationSummary(migration *envsource.LegacyMigrationResult) string {
	preserved := []string{fmt.Sprintf("%d cached environment(s)", migration.CachedEnvironments)}
	if migration.SelectionPreserved {
		preserved = append(preserved, "the current selection")
	}
	if migration.WorkspacePreserved {
		preserved = append(preserved, "the workspace")
	}
	revision := migration.Location
	if migration.Ref != "" {
		revision += " at " + migration.Ref
	}
	return fmt.Sprintf("Migrated %s to source %q offline.\n  Preserved %s; no network sync was performed.\n  Next: orbit source list; orbit source sync %s",
		revision, migration.SourceName, strings.Join(preserved, ", "), migration.SourceName)
}

func sourceEnvironmentFiles(directory string) []string {
	files, _ := filepath.Glob(filepath.Join(directory, "*.yaml"))
	return files
}

func sourceAddCmd() *cobra.Command {
	var url, path, ref, workspace string
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add and validate an environment source",
		Long: `Add a Git repository or persistent local directory as a named source.

Exactly one of --url or --path is required. --ref is valid only with --url.
Orbit validates and synchronizes the source immediately. Bare environment names
come from the first source. --workspace binds the application checkout used by
environments from this source.`,
		Example: `  orbit source add company --url https://example.com/environments.git
  orbit source add company --url https://example.com/environments.git --ref main
  orbit source add local --path /work/environment-repo --workspace /work/app`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if (url == "") == (path == "") {
				return cli.NewInvalidArgumentError("exactly one of --url or --path is required")
			}
			if path != "" && ref != "" {
				return cli.NewInvalidArgumentError("--ref is valid only with --url")
			}
			source := envsource.Source{Name: args[0], URL: url, Path: path, Ref: ref, Workspace: workspace}
			if url != "" {
				source.Type = envsource.TypeGit
			} else {
				source.Type = envsource.TypeLocal
				normalized, err := envsource.ValidateLocalSource(path)
				if err != nil {
					return cli.NewInvalidArgumentError(err.Error())
				}
				source.Path = normalized
			}
			if workspace != "" {
				normalized, err := envsource.NormalizeExistingDirectory(workspace)
				if err != nil {
					return cli.NewInvalidArgumentError(err.Error())
				}
				source.Workspace = normalized
			}
			registry, err := sourceRegistry()
			if err != nil {
				return err
			}
			if _, err := registry.Get(source.Name); !errors.Is(err, envsource.ErrNotFound) {
				return fmt.Errorf("environment source %q already exists", source.Name)
			}
			source, result, err := envsource.Refresh(registry, source, daemon.OrbitDir(), false, false)
			if err != nil {
				return sourceSyncError(source, err)
			}
			if err := registry.Add(source); err != nil {
				_ = os.RemoveAll(envsource.SourceDir(daemon.OrbitDir(), source.Name))
				return err
			}
			source, err = registry.Get(source.Name)
			if err != nil {
				return err
			}
			return writeSourceResult("source_add", source, result.Written)
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "Git repository URL")
	cmd.Flags().StringVar(&path, "path", "", "local directory containing envs/")
	cmd.Flags().StringVar(&ref, "ref", "", "Git branch, tag, or commit")
	cmd.Flags().StringVar(&workspace, "workspace", "", "local application workspace shared by this source")
	return cmd
}

func sourceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List environment sources",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			registry, err := sourceRegistry()
			if err != nil {
				return err
			}
			sources := registry.List()
			if cli.JSONOutput {
				return cli.WriteJSONSuccess(os.Stdout, commandString(), map[string]any{"operation": "source_list", "sources": sources}, nil)
			}
			if len(sources) == 0 {
				fmt.Println("No environment sources configured.\n  Next: orbit source add <name> --url <git-url>")
				return nil
			}
			for _, source := range sources {
				fmt.Printf("%s  [%s]\n", source.Name, source.Type)
				fmt.Printf("  Location: %s\n", source.Location())
				if source.Workspace != "" {
					fmt.Printf("  Workspace: %s\n", source.Workspace)
				}
				if source.ResolvedRef != "" || source.Commit != "" {
					fmt.Printf("  Revision: %s @ %s\n", source.ResolvedRef, source.Commit)
				}
				if source.LastSyncError != "" {
					fmt.Printf("  Sync: failed: %s\n", source.LastSyncError)
				} else if !source.LastSyncAt.IsZero() {
					fmt.Printf("  Sync: %s\n", source.LastSyncAt.Format(time.RFC3339))
				}
			}
			return nil
		},
	}
}

func sourceSyncCmd() *cobra.Command {
	return sourceSyncCmdWithDeprecation("")
}

type sourceSyncOptions struct {
	all               bool
	dry               bool
	yes               bool
	noApply           bool
	deprecatedCommand string
}

type sourceSyncRecord struct {
	Source  string   `json:"source"`
	Written []string `json:"written"`
	Error   string   `json:"error,omitempty"`
}

func sourceSyncCmdWithDeprecation(deprecatedCommand string) *cobra.Command {
	var all, dry, yes, noApply bool
	cmd := &cobra.Command{
		Use:   "sync [name]",
		Short: "Synchronize one or all environment sources",
		Long: `Synchronize environment definitions from a source.

With no name, Orbit synchronizes the first source. Pass a name to target one
source or --all to explicitly target every source. --dry validates and previews
changes without persisting them. If the active environment changed, Orbit can
apply it unless --no-apply is set.`,
		Example: `  orbit source sync
  orbit source sync company --dry
  orbit source sync --all --no-apply`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runSourceSync(args, sourceSyncOptions{all: all, dry: dry, yes: yes, noApply: noApply, deprecatedCommand: deprecatedCommand})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "synchronize every source")
	cmd.Flags().BoolVar(&dry, "dry", false, "validate and preview without persistent changes")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "confirm applying active updates without prompting")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "synchronize without applying active updates")
	return cmd
}

func runSourceSync(args []string, options sourceSyncOptions) error {
	if options.all && len(args) > 0 {
		return cli.NewInvalidArgumentError("source name and --all are mutually exclusive")
	}
	registry, err := sourceRegistry()
	if err != nil {
		return err
	}
	sources, err := sourceSyncTargets(registry, args, options.all)
	if err != nil {
		return err
	}
	records, failures := synchronizeSources(registry, sources, options.dry)
	if cli.JSONOutput {
		return writeSourceSyncJSON(records, failures, options)
	}
	return writeSourceSyncHuman(records, failures, options)
}

func sourceSyncTargets(registry *envsource.Registry, args []string, all bool) ([]envsource.Source, error) {
	if all {
		return registry.List(), nil
	}
	if len(args) == 1 {
		source, err := registry.Get(args[0])
		return []envsource.Source{source}, err
	}
	source, err := registry.First()
	if err != nil {
		return nil, cli.WithJSONReplacementActions(errors.New("no environment source configured; run 'orbit source --help' to add one"), []cli.JSONAction{{Command: "orbit source --help", Reason: "Choose a copyable Git or local source command."}})
	}
	return []envsource.Source{source}, nil
}

func synchronizeSources(registry *envsource.Registry, sources []envsource.Source, dry bool) ([]sourceSyncRecord, []error) {
	records := make([]sourceSyncRecord, 0, len(sources))
	var failures []error
	for _, source := range sources {
		_, result, err := envsource.Refresh(registry, source, daemon.OrbitDir(), dry, true)
		record := sourceSyncRecord{Source: source.Name, Written: result.Written}
		if err != nil {
			record.Error = err.Error()
			failures = append(failures, sourceSyncError(source, err))
		}
		records = append(records, record)
	}
	return records, failures
}

func writeSourceSyncJSON(records []sourceSyncRecord, failures []error, options sourceSyncOptions) error {
	data := map[string]any{"operation": "source_sync", "dry_run": options.dry, "sources": records}
	if options.deprecatedCommand != "" {
		data["deprecated_command"] = options.deprecatedCommand
		data["replacement_command"] = "orbit source sync"
	}
	if len(failures) == 0 {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), data, nil)
	}
	joined := errors.Join(failures...)
	if err := cli.WriteJSONFailure(os.Stdout, commandString(), data, joined, nil); err != nil {
		return err
	}
	return errCLIJSONAlreadyRendered{err: joined}
}

func writeSourceSyncHuman(records []sourceSyncRecord, failures []error, options sourceSyncOptions) error {
	for _, record := range records {
		if record.Error != "" {
			fmt.Printf("%s: failed: %s\n", record.Source, record.Error)
		} else {
			fmt.Printf("%s: synchronized (%d changed files)\n", record.Source, len(record.Written))
		}
	}
	if len(failures) > 0 {
		return errCLIHumanAlreadyRendered{err: errors.Join(failures...)}
	}
	envSyncDryRun, envSyncYes, envSyncNoApply = options.dry, options.yes, options.noApply
	return offerEnvironmentApply()
}

func sourceRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{Use: "remove <name>", Short: "Remove an environment source", Long: `Remove a source from Orbit.

A source that owns the running environment cannot be removed; switch or run
orbit down first. Removing the selected stopped source clears that selection
and requires confirmation. If the first source is removed, Orbit chooses the
next source automatically. Orbit removes Git caches, but never deletes a local
source's user-owned directory.`, Example: `  orbit source remove local
  orbit source remove company --yes`, Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		registry, err := sourceRegistry()
		if err != nil {
			return err
		}
		source, err := registry.Get(args[0])
		if err != nil {
			return err
		}
		selection := readCurrentEnv()
		ownsSelection := envsource.ContainsPath(daemon.OrbitDir(), source.Name, selection)
		if client := daemon.NewClient(daemon.DefaultSocketPath()); client.Health() == nil {
			if status, statusErr := client.Status(); statusErr == nil && status.Context.Kind == "managed" &&
				strings.HasPrefix(status.Context.Identity, source.Name+"/") && status.Context.Running {
				return cli.WithJSONActions(
					fmt.Errorf("source %q owns running environment %s; switch or run orbit down first", source.Name, status.Context.Identity),
					[]cli.JSONAction{{Command: "orbit down --json", Reason: "Stop the running environment before removing its source."}},
				)
			}
		}
		if ownsSelection && !yes {
			if !cli.JSONOutput && isatty.IsTerminal(os.Stdin.Fd()) {
				yes = cli.Confirm("Removing " + source.Name + " clears selected environment " + daemonsrv.EnvShortName(selection) + ". Continue?")
			}
			if !yes {
				return cli.WithJSONActions(errors.New("removing this source clears the selected stopped environment; pass --yes to confirm"), []cli.JSONAction{{Command: "orbit source remove " + source.Name + " --yes --json", Reason: "Remove the source and clear its selected environment.", Destructive: true}})
			}
		}
		if _, err := envsource.RemoveOwned(registry, daemon.OrbitDir(), source.Name, filepath.Join(daemon.OrbitDir(), "current"), selection); err != nil {
			return err
		}
		if cli.JSONOutput {
			return cli.WriteJSONSuccess(os.Stdout, commandString(), map[string]any{"operation": "source_remove", "source": source.Name}, nil)
		}
		fmt.Printf("Removed source %s\n", source.Name)
		return nil
	}}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "confirm clearing a selected stopped environment")
	return cmd
}

func writeSourceResult(operation string, source envsource.Source, written []string) error {
	environments := sourceEnvironmentNames(source.Name)
	actions := []cli.JSONAction{}
	if len(environments) > 0 {
		actions = append(actions, cli.JSONAction{
			Command: "orbit switch " + shellquote.Quote(source.Name+"/"+environments[0]) + " --json",
			Reason:  "Select an environment discovered in this source.",
		})
	}
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), map[string]any{"operation": operation, "source": source, "written": written, "environments": environments}, actions)
	}
	verb := "Updated"
	if operation == "source_add" {
		verb = "Added"
	}
	fmt.Printf("%s source %s [%s]\n", verb, source.Name, source.Type)
	fmt.Printf("  Location: %s\n", source.Location())
	if source.ResolvedRef != "" || source.Commit != "" {
		fmt.Printf("  Revision: %s @ %s\n", source.ResolvedRef, source.Commit)
	}
	fmt.Printf("  Environments: %d\n", len(environments))
	if len(environments) > 0 {
		fmt.Printf("  Next: orbit switch %s\n", shellquote.Quote(source.Name+"/"+environments[0]))
	}
	return nil
}

func sourceEnvironmentNames(sourceName string) []string {
	files, _ := filepath.Glob(filepath.Join(envsource.EnvsDir(daemon.OrbitDir(), sourceName), "*.yaml"))
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)))
	}
	sort.Strings(names)
	return names
}

func sourceSyncError(source envsource.Source, err error) error {
	if source.Type == envsource.TypeGit {
		return fmt.Errorf("synchronize source %s: %w", source.Name, envRepoSyncError(err))
	}
	return fmt.Errorf("synchronize source %s: %w", source.Name, err)
}

func applySourceWorkspace(configPath string) error {
	registry, err := sourceRegistry()
	if err != nil {
		return err
	}
	if source, _, found := registry.SourceForPath(daemon.OrbitDir(), configPath); found {
		if source.Workspace == "" {
			_ = os.Unsetenv("WORKSPACE_ROOT")
			_ = os.Setenv("ORBIT_SOURCE_NAME", source.Name)
			return nil
		}
		_ = os.Setenv("ORBIT_SOURCE_NAME", source.Name)
		return os.Setenv("WORKSPACE_ROOT", source.Workspace)
	}
	_ = os.Unsetenv("ORBIT_SOURCE_NAME")
	return nil
}

func managedSourceForPath(configPath string) (envsource.Source, string, bool) {
	registry, err := sourceRegistry()
	if err != nil {
		return envsource.Source{}, "", false
	}
	return registry.SourceForPath(daemon.OrbitDir(), configPath)
}
