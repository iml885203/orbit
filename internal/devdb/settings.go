package devdb

import (
	"encoding/json"
	"log/slog"

	"github.com/iml885203/orbit/daemon"
)

// settingsState is the extensions.devdb namespace in settings.json —
// the DB-workflow-owned keys, held by the codec closures registered
// with the daemon. The wire and CLI keep the flat key names; only the
// disk nests (spec D3/B6).
type settingsState struct {
	DBRoot              string `json:"db_root,omitempty"`
	SQLServerImage      string `json:"sql_server_image,omitempty"`
	SQLServerPullPolicy string `json:"sql_server_pull_policy,omitempty"`
	// rawFallback preserves an unparseable extensions.devdb blob so a
	// save never silently erases the user's settings; an explicit Set on
	// any devdb key intentionally replaces it.
	rawFallback json.RawMessage
}

func init() {
	daemon.RegisterSettingsNamespace("devdb", settingsCodec)
}

func settingsCodec() daemon.SettingsNamespaceCodec {
	st := &settingsState{}
	return daemon.SettingsNamespaceCodec{
		Hydrate:    st.hydrate,
		ToDisk:     st.toDisk,
		WireFlat:   st.wireFlat,
		Get:        st.get,
		Set:        st.set,
		EnvExports: st.envExports,
	}
}

func (st *settingsState) hydrate(raw json.RawMessage) {
	var m settingsState
	if err := json.Unmarshal(raw, &m); err != nil {
		slog.Error("failed to parse extensions.devdb settings — preserving the raw blob",
			"component", "settings", "err", err)
		st.rawFallback = raw
		return
	}
	st.DBRoot = m.DBRoot
	st.SQLServerImage = m.SQLServerImage
	st.SQLServerPullPolicy = m.SQLServerPullPolicy
}

func (st *settingsState) toDisk() json.RawMessage {
	snapshot := *st
	snapshot.rawFallback = nil
	if snapshot.empty() {
		if st.rawFallback != nil {
			return st.rawFallback
		}
		return nil
	}
	// Marshal of an all-string struct cannot fail; the nil-on-error arm
	// exists only to satisfy the signature.
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil
	}
	return data
}

func (st *settingsState) wireFlat() map[string]any {
	out := map[string]any{}
	for k, v := range map[string]string{
		"db_root":                st.DBRoot,
		"sql_server_image":       st.SQLServerImage,
		"sql_server_pull_policy": st.SQLServerPullPolicy,
	} {
		if v != "" { // wire parity: omitempty semantics of the old fields
			out[k] = v
		}
	}
	return out
}

func (st *settingsState) get(key string) (string, bool) {
	switch key {
	case "db_root":
		return st.DBRoot, true
	case "sql_server_image":
		return st.SQLServerImage, true
	case "sql_server_pull_policy":
		return st.SQLServerPullPolicy, true
	}
	return "", false
}

func (st *settingsState) set(key, value string) (bool, error) {
	switch key {
	case "db_root":
		st.DBRoot = value
	case "sql_server_image":
		st.SQLServerImage = value
	case "sql_server_pull_policy":
		st.SQLServerPullPolicy = value
	default:
		return false, nil
	}
	return true, nil
}

func (st *settingsState) envExports() map[string]string {
	out := map[string]string{}
	if st.DBRoot != "" {
		out["ORBIT_DB_ROOT"] = st.DBRoot
	}
	if st.SQLServerImage != "" {
		out["SQL_SERVER_IMAGE"] = st.SQLServerImage
	}
	if st.SQLServerPullPolicy != "" {
		out["SQL_SERVER_PULL_POLICY"] = st.SQLServerPullPolicy
	}
	return out
}

// empty reports whether every persisted key is unset (rawFallback aside).
func (st *settingsState) empty() bool {
	return st.DBRoot == "" && st.SQLServerImage == "" &&
		st.SQLServerPullPolicy == ""
}
