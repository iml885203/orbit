package app

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/iml885203/orbit/daemon"
	"github.com/spf13/cobra"
)

// settingsKeyMap maps user-facing kebab-case keys to daemon JSON keys.
// Each entry also records the value's expected type for coercion.
var settingsKeyMap = map[string]struct {
	jsonKey string
	kind    string // "string" | "bool"
}{
	"workspace-root":         {"workspace_root", "string"},
	"sql-server-image":       {"sql_server_image", "string"},
	"sql-server-pull-policy": {"sql_server_pull_policy", "string"},
	"show-history":           {"show_history", "bool"},
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
	value, err := coerceSettingsValue(jsonKey, args[1])
	if err != nil {
		return err
	}
	client, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return err
	}
	patch := map[string]any{jsonKey: value}
	if err := client.UpdateSettings(patch); err != nil {
		return err
	}
	fmt.Printf("✓ %s = %v\n", args[0], value)
	return nil
}

func runSettingsList(_ *cobra.Command, _ []string) error {
	client, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return err
	}
	current, err := client.GetSettings()
	if err != nil {
		return err
	}

	cliKeys := make([]string, 0, len(settingsKeyMap))
	for k := range settingsKeyMap {
		cliKeys = append(cliKeys, k)
	}
	sort.Strings(cliKeys)

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
