package bigquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/shellquote"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"time"
)

func Command() *cobra.Command {
	root := &cobra.Command{Use: "bq", Short: "Inspect BigQuery profiles and manage isolated test datasets"}
	var timeout time.Duration
	root.PersistentFlags().DurationVar(&timeout, "timeout", 2*time.Minute, "operation deadline (includes authentication and polling)")
	inspect := &cobra.Command{Use: "inspect PROFILE", Short: "Read shared dataset metadata with the invoking CLI identity", Args: cobra.ExactArgs(1)}
	inspect.RunE = func(cmd *cobra.Command, args []string) error {
		cfg, p, _, err := selectedProfile(cmd, args[0])
		if err != nil {
			return output(cmd, nil, err)
		}
		if p.Mode != "shared" {
			return output(cmd, nil, failure("shared_profile_required", nil))
		}
		if timeout <= 0 {
			return output(cmd, nil, failure("invalid_timeout", nil))
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()
		// Shared inspection has no recovery state and creates no instance marker.
		api, err := inspectionClient(ctx)
		if err != nil {
			return output(cmd, nil, err)
		}
		_ = cfg
		ds, err := api.Datasets.Get(p.DataProject, p.Dataset).Context(ctx).Do()
		if err != nil {
			return output(cmd, nil, failure("dataset_inspect_failed", err))
		}
		if ds.Location != p.Location {
			return output(cmd, nil, failure("location_mismatch", nil))
		}
		return output(cmd, map[string]any{"dataset": p.DataProject + "." + p.Dataset, "job_project": p.JobProject, "location": ds.Location, "verified": []string{"dataset_metadata_read"}, "auth_context": "invoking_cli_adc", "app_runtime": "unverified", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "app_config": appConfig(p, p.Dataset)}, nil)
	}
	sandbox := &cobra.Command{Use: "sandbox", Short: "Manage canonical local sandbox records"}
	list := &cobra.Command{Use: "list", Short: "Read local recovery records without contacting GCP", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, _, err := selectedConfig(cmd)
		if err != nil {
			return output(cmd, nil, err)
		}
		records, err := readRecords(storeDirectory(cfg.Identity))
		return output(cmd, records, err)
	}}
	sandbox.AddCommand(list)
	for _, action := range []string{"prepare", "inspect", "clean"} {
		var run string
		var yes, stopped, exclusive bool
		cmd := &cobra.Command{Use: action + " PROFILE", Short: action + " a sandbox using its stable run key", Args: cobra.ExactArgs(1)}
		cmd.Flags().StringVar(&run, "run", "", "stable, nonsecret run key (required)")
		if action == "clean" {
			cmd.Flags().BoolVar(&yes, "yes", false, "confirm recursive deletion of the recorded dataset")
			cmd.Flags().BoolVar(&stopped, "app-stopped", false, "attest all application and external jobs are stopped")
			cmd.Flags().BoolVar(&exclusive, "restricted-writers", false, "attest restricted writers, no dataset replacement and no name reuse")
		}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			cfg, p, path, err := selectedProfile(cmd, args[0])
			if err != nil {
				return output(cmd, nil, err)
			}
			if p.Mode != "sandbox" || run == "" || len(run) > 256 {
				return output(cmd, nil, failure("sandbox_profile_and_run_required", nil))
			}
			if action == "inspect" {
				records, err := readRecords(storeDirectory(cfg.Identity))
				if err != nil {
					return output(cmd, nil, err)
				}
				for _, rec := range records {
					if rec.Scope == scopeFor(p, run) {
						return output(cmd, rec, nil)
					}
				}
				return output(cmd, nil, failure("receipt_unavailable", os.ErrNotExist))
			}
			if action == "clean" && (!yes || !stopped || !exclusive) {
				return output(cmd, nil, failure("cleanup_requires_confirmation_and_quiescence", nil))
			}
			if timeout <= 0 {
				return output(cmd, nil, failure("invalid_timeout", nil))
			}
			var input inputs
			if action == "prepare" {
				input, err = readInputs(filepath.Dir(path), p)
				if err != nil {
					return output(cmd, nil, err)
				}
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			runner, err := newRunner(ctx, cfg.Identity, action == "prepare")
			if err != nil {
				return output(cmd, nil, err)
			}
			var rec *Receipt
			if action == "prepare" {
				rec, err = runner.prepare(ctx, p, run, input)
			} else {
				rec, err = runner.clean(ctx, p, run, true)
			}
			return output(cmd, rec, err)
		}
		sandbox.AddCommand(cmd)
	}
	root.AddCommand(inspect, sandbox)
	return cli.WithDaemonAffinity(root, cli.DaemonMatchingConfig)
}

func selectedConfig(cmd *cobra.Command) (*BQConfig, string, error) {
	path, err := cmd.Root().PersistentFlags().GetString("config")
	if err != nil {
		return nil, "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, "", err
	}
	c := From(cfg)
	if c == nil {
		return nil, "", failure("bq_not_configured", nil)
	}
	return c, path, nil
}
func selectedProfile(cmd *cobra.Command, name string) (*BQConfig, Profile, string, error) {
	cfg, path, err := selectedConfig(cmd)
	if err != nil {
		return nil, Profile{}, "", err
	}
	p, ok := cfg.Profiles[name]
	if !ok {
		return nil, Profile{}, "", failure("profile_unknown", nil)
	}
	return cfg, p, path, nil
}
func output(cmd *cobra.Command, data any, err error) error {
	if cli.JSONOutput {
		if err != nil {
			err = cli.WithJSONReplacementActions(err, recoveryActions(cmd))
			writeErr := cli.WriteJSONFailure(cmd.OutOrStdout(), cmd.CommandPath(), data, err, nil)
			return cli.MarkJSONErrorRendered(errors.Join(err, writeErr))
		}
		return cli.WriteJSONSuccess(cmd.OutOrStdout(), cmd.CommandPath(), data, nil)
	}
	if data != nil {
		raw, encodeErr := json.MarshalIndent(data, "", "  ")
		if encodeErr != nil {
			return encodeErr
		}
		if _, writeErr := fmt.Fprintln(cmd.OutOrStdout(), string(raw)); writeErr != nil {
			return writeErr
		}
	}
	return err
}

func recoveryActions(cmd *cobra.Command) []cli.JSONAction {
	path, _ := cmd.Root().PersistentFlags().GetString("config")
	if path == "" {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	base := "orbit --config " + shellquote.Quote(abs)
	run, _ := cmd.Flags().GetString("run")
	args := cmd.Flags().Args()
	action := base + " bq sandbox list --json"
	if run != "" && len(args) == 1 {
		action = base + " bq sandbox inspect " + shellquote.Quote(args[0]) + " --run " + shellquote.Quote(run) + " --json"
	}
	return []cli.JSONAction{{Command: action, Reason: "Read local recovery evidence without contacting GCP or changing cloud resources.", Destructive: false}}
}
