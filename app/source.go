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

A source is a Git repository or local directory containing envs/. Environments
are the runnable configurations inside it. The first source becomes the default,
so bare environment names resolve there; use <source>/<environment> to select
from another source.

Common workflow:
  orbit source add company --url https://example.com/environments.git
  orbit switch company/development
  orbit source info company
  orbit source remove company

Refresh later:
  orbit source sync                 # sync the default source
  orbit source sync company         # sync one named source
  orbit source sync --all           # explicitly sync every source

Local quick start:
  orbit source add local --path /path/to/environment-repo
  orbit switch local/development

Advanced configuration uses "orbit source update". Compatibility aliases
set-default, set-workspace, and clear-workspace remain available but are
deprecated and hidden from the primary command list.`,
	}
	cmd.AddCommand(sourceAddCmd())
	cmd.AddCommand(sourceListCmd())
	cmd.AddCommand(sourceInfoCmd())
	cmd.AddCommand(sourceSyncCmd())
	cmd.AddCommand(sourceUpdateCmd())
	cmd.AddCommand(sourceSetDefaultCmd())
	cmd.AddCommand(sourceSetWorkspaceCmd())
	cmd.AddCommand(sourceClearWorkspaceCmd())
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
			Message: "Migrated the legacy environment repository to source default without a network sync.",
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
	return fmt.Sprintf("Migrated %s to source %q offline.\n  Preserved %s; no network sync was performed.\n  Next: orbit source info %s; orbit source sync %s",
		revision, migration.SourceName, strings.Join(preserved, ", "), migration.SourceName, migration.SourceName)
}

func sourceEnvironmentFiles(directory string) []string {
	files, _ := filepath.Glob(filepath.Join(directory, "*.yaml"))
	return files
}

func sourceAddCmd() *cobra.Command {
	var url, path, ref, workspace string
	var makeDefault bool
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add and validate an environment source",
		Long: `Add a Git repository or persistent local directory as a named source.

Exactly one of --url or --path is required. --ref is valid only with --url.
Orbit validates and synchronizes the source immediately. The first source is
automatically the default. --workspace binds the application checkout used by
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
			if err := registry.Add(source, makeDefault); err != nil {
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
	cmd.Flags().BoolVar(&makeDefault, "default", false, "make this the default source")
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
				labels := []string{source.Type}
				if source.Default {
					labels = append(labels, "Default")
				}
				fmt.Printf("%s  [%s]\n", source.Name, strings.Join(labels, "] ["))
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

func sourceInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <name>",
		Short: "Inspect an environment source",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			registry, err := sourceRegistry()
			if err != nil {
				return err
			}
			source, err := registry.Get(args[0])
			if err != nil {
				return err
			}
			environments := sourceEnvironmentNames(source.Name)
			if cli.JSONOutput {
				return cli.WriteJSONSuccess(os.Stdout, commandString(), map[string]any{"operation": "source_info", "source": source, "environments": environments}, nil)
			}
			fmt.Printf("Source: %s [%s]\nLocation: %s\n", source.Name, source.Type, source.Location())
			if source.Default {
				fmt.Println("Default: yes")
			}
			if source.Ref != "" {
				fmt.Printf("Ref: %s\n", source.Ref)
			}
			if source.ResolvedRef != "" {
				fmt.Printf("Resolved ref: %s\n", source.ResolvedRef)
			}
			if source.Commit != "" {
				fmt.Printf("Commit: %s\n", source.Commit)
			}
			if source.Workspace != "" {
				fmt.Printf("Workspace: %s\n", source.Workspace)
			} else {
				fmt.Println("Workspace: not set")
			}
			if source.LastSyncError != "" {
				fmt.Printf("Last sync: failed: %s\n", source.LastSyncError)
			} else if !source.LastSyncAt.IsZero() {
				fmt.Printf("Last sync: %s\n", source.LastSyncAt.Format(time.RFC3339))
			}
			fmt.Printf("Environments: %s\n", strings.Join(environments, ", "))
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

With no name, Orbit synchronizes the default source. Pass a name to target one
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
	source, err := registry.Default()
	if err != nil {
		return nil, cli.WithJSONReplacementActions(errors.New("no default environment source configured; run 'orbit source --help' to add one"), []cli.JSONAction{{Command: "orbit source --help", Reason: "Choose a copyable Git or local first-source command."}})
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

func sourceUpdateCmd() *cobra.Command {
	var url, path, ref, workspace string
	var clearRef, clearWorkspace, makeDefault bool
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update an environment source",
		Long: `Update a source through one consistent command.

Changing --url, --path, --ref, or --clear-ref validates and synchronizes source
content. --workspace, --clear-workspace, and --default update metadata only and
do not access the source or apply an environment. Conflicting flags are rejected
before any change is saved. Changing between Git and local source types requires
adding a new source.`,
		Example: `  orbit source update company --ref release/2026.08
  orbit source update company --clear-ref
  orbit source update company --workspace /work/company
  orbit source update company --clear-workspace
  orbit source update company --default`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSourceUpdate(args[0], sourceUpdateOptions{
				url: url, path: path, ref: ref, workspace: workspace,
				urlChanged: cmd.Flags().Changed("url"), pathChanged: cmd.Flags().Changed("path"),
				refChanged: cmd.Flags().Changed("ref"), workspaceChanged: cmd.Flags().Changed("workspace"),
				clearRef: clearRef, clearWorkspace: clearWorkspace, makeDefault: makeDefault,
			})
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "replacement Git URL")
	cmd.Flags().StringVar(&path, "path", "", "replacement local source path")
	cmd.Flags().StringVar(&ref, "ref", "", "replacement Git branch, tag, or commit")
	cmd.Flags().BoolVar(&clearRef, "clear-ref", false, "follow the Git repository default branch")
	cmd.Flags().StringVar(&workspace, "workspace", "", "local application workspace shared by this source")
	cmd.Flags().BoolVar(&clearWorkspace, "clear-workspace", false, "remove the source workspace binding")
	cmd.Flags().BoolVar(&makeDefault, "default", false, "make this the default source")
	return cmd
}

type sourceUpdateOptions struct {
	url, path, ref, workspace                  string
	urlChanged, pathChanged, refChanged        bool
	workspaceChanged, clearRef, clearWorkspace bool
	makeDefault                                bool
}

func (options sourceUpdateOptions) locationChanged() bool {
	return options.urlChanged || options.pathChanged || options.refChanged || options.clearRef
}

func (options sourceUpdateOptions) metadataChanged() bool {
	return options.workspaceChanged || options.clearWorkspace || options.makeDefault
}

func runSourceUpdate(name string, options sourceUpdateOptions) error {
	normalizedWorkspace, err := validateSourceUpdateOptions(options)
	if err != nil {
		return err
	}
	registry, err := sourceRegistry()
	if err != nil {
		return err
	}
	source, err := registry.Get(name)
	if err != nil {
		return err
	}
	source, err = applySourceUpdateOptions(source, options, normalizedWorkspace)
	if err != nil {
		return err
	}
	written := []string{}
	if options.locationChanged() {
		var result envsource.SyncResult
		source, result, err = envsource.ApplyProposedUpdate(registry, source, daemon.OrbitDir(), true)
		if err != nil {
			return sourceSyncError(source, err)
		}
		written = result.Written
	} else if options.metadataChanged() {
		if _, _, err := envsource.ApplyProposedUpdate(registry, source, daemon.OrbitDir(), false); err != nil {
			return err
		}
	}
	source, err = registry.Get(source.Name)
	if err != nil {
		return err
	}
	return writeSourceResult("source_update", source, written)
}

func validateSourceUpdateOptions(options sourceUpdateOptions) (string, error) {
	if !options.locationChanged() && !options.metadataChanged() {
		return "", cli.NewInvalidArgumentError("at least one update flag is required")
	}
	if options.urlChanged && options.pathChanged {
		return "", cli.NewInvalidArgumentError("--url and --path are mutually exclusive")
	}
	if options.ref != "" && options.clearRef {
		return "", cli.NewInvalidArgumentError("--ref and --clear-ref are mutually exclusive")
	}
	if options.workspaceChanged && options.clearWorkspace {
		return "", cli.NewInvalidArgumentError("--workspace and --clear-workspace are mutually exclusive")
	}
	if options.urlChanged && strings.TrimSpace(options.url) == "" {
		return "", cli.NewInvalidArgumentError("--url cannot be empty")
	}
	if options.pathChanged && strings.TrimSpace(options.path) == "" {
		return "", cli.NewInvalidArgumentError("--path cannot be empty")
	}
	if !options.workspaceChanged {
		return "", nil
	}
	workspace, err := envsource.NormalizeExistingDirectory(options.workspace)
	if err != nil {
		return "", cli.NewInvalidArgumentError(err.Error())
	}
	return workspace, nil
}

func applySourceUpdateOptions(source envsource.Source, options sourceUpdateOptions, workspace string) (envsource.Source, error) {
	if options.urlChanged && source.Type != envsource.TypeGit || options.pathChanged && source.Type != envsource.TypeLocal {
		return source, cli.NewInvalidArgumentError("changing source type requires adding a new source")
	}
	if (options.refChanged || options.clearRef) && source.Type != envsource.TypeGit {
		return source, cli.NewInvalidArgumentError("--ref and --clear-ref are valid only with a Git source")
	}
	if options.urlChanged {
		source.URL = options.url
	}
	if options.pathChanged {
		path, err := envsource.ValidateLocalSource(options.path)
		if err != nil {
			return source, err
		}
		source.Path = path
	}
	if options.refChanged {
		source.Ref = options.ref
	} else if options.clearRef {
		source.Ref = ""
	}
	if options.workspaceChanged {
		source.Workspace = workspace
	} else if options.clearWorkspace {
		source.Workspace = ""
	}
	if options.makeDefault {
		source.Default = true
	}
	return source, nil
}

func sourceSetDefaultCmd() *cobra.Command {
	return &cobra.Command{Use: "set-default <name>", Short: "Set the default environment source", Hidden: true, Deprecated: "use 'orbit source update <name> --default'", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		registry, err := sourceRegistry()
		if err != nil {
			return err
		}
		if err := registry.SetDefault(args[0]); err != nil {
			return err
		}
		return writeSourceMutation("source_set_default", args[0])
	}}
}

func sourceSetWorkspaceCmd() *cobra.Command {
	return &cobra.Command{Use: "set-workspace <name> <path>", Short: "Bind a source workspace", Hidden: true, Deprecated: "use 'orbit source update <name> --workspace <path>'", Args: cobra.ExactArgs(2), RunE: func(_ *cobra.Command, args []string) error {
		registry, err := sourceRegistry()
		if err != nil {
			return err
		}
		source, err := registry.Get(args[0])
		if err != nil {
			return err
		}
		workspace, err := envsource.NormalizeExistingDirectory(args[1])
		if err != nil {
			return cli.NewInvalidArgumentError(err.Error())
		}
		source.Workspace = workspace
		if err := registry.Replace(source); err != nil {
			return err
		}
		return writeSourceMutation("source_set_workspace", source.Name)
	}}
}

func sourceClearWorkspaceCmd() *cobra.Command {
	return &cobra.Command{Use: "clear-workspace <name>", Short: "Clear a source workspace", Hidden: true, Deprecated: "use 'orbit source update <name> --clear-workspace'", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		registry, err := sourceRegistry()
		if err != nil {
			return err
		}
		source, err := registry.Get(args[0])
		if err != nil {
			return err
		}
		source.Workspace = ""
		if err := registry.Replace(source); err != nil {
			return err
		}
		return writeSourceMutation("source_clear_workspace", source.Name)
	}}
}

func sourceRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{Use: "remove <name>", Short: "Remove an environment source", Long: `Remove a source from Orbit.

A source that owns the running environment cannot be removed; switch or run
orbit down first. Removing the selected stopped source clears that selection
and requires confirmation. A default source cannot be removed while other
sources exist until another source is made default. Orbit removes Git caches,
but never deletes a local source's user-owned directory.`, Example: `  orbit source remove local
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
		if source.Default && len(registry.List()) > 1 {
			return cli.WithJSONReplacementActions(
				errors.New("the default source cannot be removed while other sources exist; run 'orbit source list', then make another source default"),
				[]cli.JSONAction{{Command: "orbit source list --json", Reason: "Choose the source that should become default."}},
			)
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
		return writeSourceMutation("source_remove", source.Name)
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
	if source.Default {
		fmt.Println("  Default: yes")
	}
	if source.ResolvedRef != "" || source.Commit != "" {
		fmt.Printf("  Revision: %s @ %s\n", source.ResolvedRef, source.Commit)
	}
	fmt.Printf("  Environments: %d\n", len(environments))
	if len(environments) > 0 {
		fmt.Printf("  Next: orbit switch %s\n", shellquote.Quote(source.Name+"/"+environments[0]))
	}
	return nil
}

func writeSourceMutation(operation, name string) error {
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), map[string]any{"operation": operation, "source": name}, nil)
	}
	fmt.Printf("%s: %s\n", operation, name)
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
