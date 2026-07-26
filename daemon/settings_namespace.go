package daemon

import (
	"encoding/json"
	"sync"
)

// Settings namespaces let a feature set own keys inside settings.json
// without the core Settings struct knowing them: on disk the namespace
// nests under extensions.<name> (spec D3), while Get/Set, ApplyToEnv,
// and the /api/settings wire shape keep exposing the keys flat — the
// codec supplies each surface.
//
// Registration is a factory so every Settings instance (tests load many)
// gets fresh, isolated namespace state. Like config's extension
// sections, factories register from package init in the owning feature
// package: settings load on CLI startup paths, so registration must
// precede every LoadSettings.
type SettingsNamespaceCodec struct {
	// Hydrate consumes the extensions.<name> blob from disk.
	Hydrate func(raw json.RawMessage)
	// ToDisk returns the namespace blob, nil when every key is empty
	// (an untouched install writes no extensions entry at all).
	ToDisk func() json.RawMessage
	// WireFlat returns the key→value pairs merged flat into the
	// /api/settings GET payload — the wire shape predates the disk
	// namespace and must not change.
	WireFlat func() map[string]any
	// Get/Set route the flat key names (orbit settings get/set, the PUT
	// handler). Set reports whether it handled the key.
	Get func(key string) (string, bool)
	Set func(key, value string) (handled bool, err error)
	// EnvExports returns env vars exported by ApplyToEnv.
	EnvExports func() map[string]string
}

var settingsNamespaces = struct {
	mu        sync.RWMutex
	factories map[string]func() SettingsNamespaceCodec
}{factories: map[string]func() SettingsNamespaceCodec{}}

func RegisterSettingsNamespace(name string, factory func() SettingsNamespaceCodec) {
	settingsNamespaces.mu.Lock()
	defer settingsNamespaces.mu.Unlock()
	settingsNamespaces.factories[name] = factory
}

// newNamespaceCodecs instantiates one codec per registered namespace —
// called by LoadSettings before the Settings value is shared.
func newNamespaceCodecs() map[string]SettingsNamespaceCodec {
	settingsNamespaces.mu.RLock()
	defer settingsNamespaces.mu.RUnlock()
	out := make(map[string]SettingsNamespaceCodec, len(settingsNamespaces.factories))
	for name, factory := range settingsNamespaces.factories {
		out[name] = factory()
	}
	return out
}
