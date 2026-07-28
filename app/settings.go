package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/spf13/cobra"
)

// settingsKeyMap maps user-facing kebab-case keys to daemon JSON keys.
// Each entry also records the value's expected type for coercion.
var settingsKeyMap = map[string]struct {
	jsonKey string
	kind    string // "string" | "bool"
}{
	"workspace-root": {"workspace_root", "string"},
	"show-history":   {"show_history", "bool"},
}

func translateSettingsKey(cliKey string) (string, error) {
	entry, ok := settingsKeyMap[cliKey]
	if !ok {
		allowed := make([]string, 0, len(settingsKeyMap))
		for k := range settingsKeyMap {
			allowed = append(allowed, k)
		}
		sort.Strings(allowed)
		return "", fmt.Errorf("unknown settings key %q (allowed: %v)", cliKey, allowed)
	}
	return entry.jsonKey, nil
}

func coerceSettingsValue(jsonKey, raw string) (any, error) {
	for _, entry := range settingsKeyMap {
		if entry.jsonKey != jsonKey {
			continue
		}
		switch entry.kind {
		case "bool":
			return parseOnOff(raw)
		case "string":
			return raw, nil
		}
	}
	return raw, nil
}

func settingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "settings",
		Short:  "Manage user settings persisted in ~/.orbit/settings.json",
		Hidden: true,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a settings key (use 'orbit settings list' to see allowed keys)",
		Args:  cobra.ExactArgs(2),
		RunE:  runSettingsSet,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Show current settings values and allowed keys",
		RunE:  runSettingsList,
	})
	return cmd
}

func runSettingsSet(_ *cobra.Command, args []string) error {
	jsonKey, err := translateSettingsKey(args[0])
	if err != nil {
		return err
	}
	if jsonKey == "workspace_root" {
		args[1], err = normalizeWorkspaceRoot(args[1])
		if err != nil {
			return err
		}
	}
	value, err := coerceSettingsValue(jsonKey, args[1])
	if err != nil {
		return err
	}

	client := daemon.NewClient(daemon.DefaultSocketPath())
	if client.Health() == nil {
		if err := client.UpdateSettings(map[string]any{jsonKey: value}); err != nil {
			return err
		}
	} else {
		if jsonKey != "workspace_root" {
			return daemon.ErrDaemonUnreachable
		}
		settings := daemon.LoadSettings(daemon.DefaultSettingsPath())
		if err := settings.Set(jsonKey, value.(string)); err != nil {
			return fmt.Errorf("saving %s: %w", args[0], err)
		}
	}
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), map[string]any{
			"operation": "settings_set",
			"key":       args[0],
			"value":     value,
		}, []cli.JSONAction{cli.DoctorAction()})
	}
	fmt.Printf("✓ %s = %v\n", args[0], value)
	return nil
}

func normalizeWorkspaceRoot(raw string) (string, error) {
	path, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root %q: %w", raw, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("workspace root %s is not available: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace root %s is not a directory", path)
	}
	return filepath.Clean(path), nil
}

func runSettingsList(_ *cobra.Command, _ []string) error {
	client := daemon.NewClient(daemon.DefaultSocketPath())
	var current map[string]any
	if client.Health() == nil {
		var err error
		current, err = client.GetSettings()
		if err != nil {
			return err
		}
	} else {
		settings := daemon.LoadSettings(daemon.DefaultSettingsPath())
		current = map[string]any{
			"workspace_root": settings.WorkspaceRoot,
			"show_history":   settings.ShowHistory,
		}
	}

	cliKeys := make([]string, 0, len(settingsKeyMap))
	for k := range settingsKeyMap {
		cliKeys = append(cliKeys, k)
	}
	sort.Strings(cliKeys)
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), map[string]any{
			"operation": "settings_list",
			"settings":  current,
		}, []cli.JSONAction{})
	}

	for _, cliKey := range cliKeys {
		jsonKey := settingsKeyMap[cliKey].jsonKey
		val, ok := current[jsonKey]
		if !ok {
			fmt.Printf("  %s = <unset>\n", cliKey)
			continue
		}
		out, _ := json.Marshal(val)
		fmt.Printf("  %s = %s\n", cliKey, string(out))
	}
	return nil
}
