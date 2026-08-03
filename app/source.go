package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/envsource"
	"github.com/iml885203/orbit/internal/envsync"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

func sourceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "source", Short: "Manage shared environment sources"}
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
	return migrateLegacyEnvironmentSource(registry)
}

func migrateLegacyEnvironmentSource(registry *envsource.Registry) (*envsource.Registry, error) {
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
		return registry, nil
	}
	return envsource.LoadMigratingLegacy(daemon.OrbitDir(), envsource.LegacyMigration{
		URL: url, Ref: ref, Workspace: settings.Get("workspace_root"), EnvsDir: envsDestDir(),
		Selection: legacySelection, SelectionFile: filepath.Join(daemon.OrbitDir(), "current"),
		Clear: settings.ClearLegacyEnvironmentSettings,
	})
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
		Args:  cobra.ExactArgs(1),
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
			if source.Commit != "" {
				fmt.Printf("Commit: %s\n", source.Commit)
			}
			if source.Workspace != "" {
				fmt.Printf("Workspace: %s\n", source.Workspace)
			} else {
				fmt.Println("Workspace: not set")
			}
			fmt.Printf("Environments: %s\n", strings.Join(environments, ", "))
			return nil
		},
	}
}

func sourceSyncCmd() *cobra.Command {
	var all, dry, yes, noApply bool
	cmd := &cobra.Command{
		Use:   "sync [name]",
		Short: "Synchronize one or all environment sources",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if all && len(args) > 0 {
				return cli.NewInvalidArgumentError("source name and --all are mutually exclusive")
			}
			registry, err := sourceRegistry()
			if err != nil {
				return err
			}
			var sources []envsource.Source
			switch {
			case all:
				sources = registry.List()
			case len(args) == 1:
				source, getErr := registry.Get(args[0])
				if getErr != nil {
					return getErr
				}
				sources = []envsource.Source{source}
			default:
				source, getErr := registry.Default()
				if getErr != nil {
					return cli.WithJSONReplacementActions(errors.New("no default environment source configured"), []cli.JSONAction{{Command: "orbit source add <name> --url <git-url> --json", Reason: "Add the first managed environment source."}})
				}
				sources = []envsource.Source{source}
			}
			type syncRecord struct {
				Source  string   `json:"source"`
				Written []string `json:"written"`
				Error   string   `json:"error,omitempty"`
			}
			records := make([]syncRecord, 0, len(sources))
			var failures []error
			for _, source := range sources {
				updated, result, syncErr := envsource.Refresh(registry, source, daemon.OrbitDir(), dry, true)
				record := syncRecord{Source: source.Name, Written: result.Written}
				if syncErr != nil {
					record.Error = syncErr.Error()
					failures = append(failures, sourceSyncError(source, syncErr))
				} else if !dry {
					source = updated
				}
				records = append(records, record)
			}
			if cli.JSONOutput {
				if len(failures) > 0 {
					joined := errors.Join(failures...)
					if err := cli.WriteJSONFailure(os.Stdout, commandString(), map[string]any{"operation": "source_sync", "dry_run": dry, "sources": records}, joined, nil); err != nil {
						return err
					}
					return errCLIJSONAlreadyRendered{err: joined}
				}
				return cli.WriteJSONSuccess(os.Stdout, commandString(), map[string]any{"operation": "source_sync", "dry_run": dry, "sources": records}, nil)
			}
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
			envSyncDryRun, envSyncYes, envSyncNoApply = dry, yes, noApply
			return offerEnvironmentApply()
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "synchronize every source")
	cmd.Flags().BoolVar(&dry, "dry", false, "validate and preview without persistent changes")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "confirm applying active updates without prompting")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "synchronize without applying active updates")
	return cmd
}

func sourceUpdateCmd() *cobra.Command {
	var url, path, ref string
	var clearRef bool
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a source location or Git ref",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := sourceRegistry()
			if err != nil {
				return err
			}
			source, err := registry.Get(args[0])
			if err != nil {
				return err
			}
			if url != "" && source.Type != envsource.TypeGit || path != "" && source.Type != envsource.TypeLocal {
				return cli.NewInvalidArgumentError("changing source type requires adding a new source")
			}
			if ref != "" && clearRef {
				return cli.NewInvalidArgumentError("--ref and --clear-ref are mutually exclusive")
			}
			if url != "" {
				source.URL = url
			}
			if path != "" {
				normalized, normalizeErr := envsource.ValidateLocalSource(path)
				if normalizeErr != nil {
					return normalizeErr
				}
				source.Path = normalized
			}
			if cmd.Flags().Changed("ref") {
				source.Ref = ref
			}
			if clearRef {
				source.Ref = ""
			}
			source, result, err := envsource.Refresh(registry, source, daemon.OrbitDir(), false, true)
			if err != nil {
				return sourceSyncError(source, err)
			}
			return writeSourceResult("source_update", source, result.Written)
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "replacement Git URL")
	cmd.Flags().StringVar(&path, "path", "", "replacement local source path")
	cmd.Flags().StringVar(&ref, "ref", "", "replacement Git branch, tag, or commit")
	cmd.Flags().BoolVar(&clearRef, "clear-ref", false, "follow the Git repository default branch")
	return cmd
}

func sourceSetDefaultCmd() *cobra.Command {
	return &cobra.Command{Use: "set-default <name>", Short: "Set the default environment source", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
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
	return &cobra.Command{Use: "set-workspace <name> <path>", Short: "Bind a source workspace", Args: cobra.ExactArgs(2), RunE: func(_ *cobra.Command, args []string) error {
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
	return &cobra.Command{Use: "clear-workspace <name>", Short: "Clear a source workspace", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
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
	cmd := &cobra.Command{Use: "remove <name>", Short: "Remove an environment source", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
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
				return fmt.Errorf("source %q owns running environment %s; switch or run orbit down first", source.Name, status.Context.Identity)
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
		return writeSourceMutation("source_remove", source.Name)
	}}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "confirm clearing a selected stopped environment")
	return cmd
}

func writeSourceResult(operation string, source envsource.Source, written []string) error {
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), map[string]any{"operation": operation, "source": source, "written": written}, nil)
	}
	fmt.Printf("%s: %s [%s]\n", operation, source.Name, source.Type)
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
