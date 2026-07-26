package cmdmap

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/iml885203/orbit/internal/shellquote"
)

func init() {
	rules = defaultRules()
}

func defaultRules() []Rule {
	return []Rule{
		{Method: "POST", Pattern: "/api/up", Build: func(_ map[string]string, body []byte) Entry {
			services := pluckStringSlice(body, "services")
			cmd := "orbit up"
			if pluckBool(body, "infra_only") {
				cmd += " --infra"
			}
			if len(services) > 0 {
				cmd += " " + stringsJoinShell(services)
			}
			return Entry{Command: cmd, Summary: "start services", HasCLI: true, UserAction: true}
		}},
		{Method: "POST", Pattern: "/api/down", Build: func(_ map[string]string, _ []byte) Entry {
			return Entry{Command: "orbit down", Summary: "stop everything", HasCLI: true, UserAction: true}
		}},
		{Method: "POST", Pattern: "/api/restart/:name", Build: func(p map[string]string, _ []byte) Entry {
			n := p["name"]
			return Entry{Command: "orbit restart " + n, Summary: "restart service " + n, HasCLI: true, UserAction: true}
		}},
		{Method: "POST", Pattern: "/api/stop/:name", Build: func(p map[string]string, _ []byte) Entry {
			n := p["name"]
			return Entry{Command: "orbit down " + n, Summary: "stop service " + n, HasCLI: true, UserAction: true}
		}},
		{Method: "PUT", Pattern: "/api/service-mode/:name", Build: func(p map[string]string, body []byte) Entry {
			mode := pluckString(body, "mode")
			return Entry{
				Command:    "orbit service mode " + p["name"] + " " + mode,
				Summary:    "set service mode of " + p["name"] + " to " + mode,
				HasCLI:     true,
				UserAction: true,
			}
		}},
		{Method: "PUT", Pattern: "/api/edges/:from/:to", Build: func(p map[string]string, body []byte) Entry {
			return edgeEntry(p["from"], p["to"], pluckBool(body, "detached"))
		}},
		{Method: "PUT", Pattern: "/api/envs/current", Build: func(_ map[string]string, body []byte) Entry {
			env := pluckString(body, "env")
			return Entry{Command: "orbit switch " + env, Summary: "switch env to " + env, HasCLI: true, UserAction: true}
		}},
		{Method: "PUT", Pattern: "/api/env-toggles", Build: func(_ map[string]string, body []byte) Entry {
			svc := pluckString(body, "service")
			v := pluckString(body, "var")
			state := "off"
			if pluckBool(body, "enabled") {
				state = "on"
			}
			return Entry{
				Command:    "orbit env toggle " + svc + " " + v + " " + state,
				Summary:    "toggle env var " + v + " on " + svc + " " + state,
				HasCLI:     true,
				UserAction: true,
			}
		}},
		{Method: "PUT", Pattern: "/api/settings", Build: func(_ map[string]string, body []byte) Entry {
			cmd := settingsCommand(body)
			return Entry{Command: cmd, Summary: "update settings", HasCLI: cmd != "", UserAction: true}
		}},
		{Method: "GET", Pattern: "/api/doctor", Build: func(_ map[string]string, _ []byte) Entry {
			return Entry{Command: "orbit doctor", Summary: "run diagnostics", HasCLI: true, UserAction: false}
		}},
	}
}

func edgeEntry(from, to string, detached bool) Entry {
	verb := "attach"
	if detached {
		verb = "detach"
	}
	return Entry{
		Command:    "orbit edge " + verb + " " + from + " " + to,
		Summary:    verb + " edge " + from + " -> " + to,
		HasCLI:     true,
		UserAction: true,
	}
}

// settingsJSONKeyToCLI maps daemon JSON keys to user-facing CLI keys for
// Action History replay. Must match settingsKeyMap in cmd/orbit/settings.go.
var settingsJSONKeyToCLI = map[string]string{
	"sql_server_image":       "sql-server-image",
	"sql_server_pull_policy": "sql-server-pull-policy",
	"show_history":           "show-history",
}

// settingsCommand renders one or more `orbit settings set ...` invocations
// joined with `&&` so the Action History entry replays the full multi-key
// update as a single shell line.
func settingsCommand(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, jsonKey := range keys {
		cliKey, ok := settingsJSONKeyToCLI[jsonKey]
		if !ok {
			continue
		}
		parts = append(parts, "orbit settings set "+cliKey+" "+formatSettingsValue(m[jsonKey]))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " && ")
}

func formatSettingsValue(v any) string {
	switch x := v.(type) {
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string:
		return shellquote.Quote(x)
	default:
		out, _ := json.Marshal(v)
		return string(out)
	}
}

func pluckString(body []byte, key string) string {
	if len(body) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func pluckBool(body []byte, key string) bool {
	if len(body) == 0 {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return false
	}
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func pluckStringSlice(body []byte, key string) []string {
	if len(body) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil
	}
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func stringsJoinShell(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += " "
		}
		out += shellquote.Quote(item)
	}
	return out
}
