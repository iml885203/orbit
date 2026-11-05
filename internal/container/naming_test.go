package container

import "testing"

func TestContainerName(t *testing.T) {
	tests := []struct {
		namespace string
		svc       string
		want      string
	}{
		{"", "redis", "orbit-redis"},
		{"e2e-abc", "redis", "orbit-e2e-abc-redis"},
		{"", "sql-server", "orbit-sql-server"},
		{"dev1", "kafka", "orbit-dev1-kafka"},
	}
	for _, tt := range tests {
		got := ContainerName(tt.namespace, tt.svc)
		if got != tt.want {
			t.Errorf("ContainerName(%q, %q) = %q, want %q", tt.namespace, tt.svc, got, tt.want)
		}
	}
}

func TestManager_NamespaceMatching(t *testing.T) {
	m := &Manager{namespace: "e2e-abc"}
	tests := []struct {
		labels map[string]string
		want   bool
	}{
		{map[string]string{labelNamespace: "e2e-abc"}, true},
		{map[string]string{labelNamespace: "other"}, false},
		{map[string]string{}, false}, // legacy (un-namespaced) doesn't match a namespaced manager
	}
	for _, tt := range tests {
		if got := m.matchesNamespace(tt.labels); got != tt.want {
			t.Errorf("matchesNamespace(%v) = %v, want %v", tt.labels, got, tt.want)
		}
	}
}

func TestManager_DefaultNamespaceMatchesLegacy(t *testing.T) {
	m := &Manager{namespace: ""}
	// Un-namespaced manager owns legacy containers (no label).
	if !m.matchesNamespace(map[string]string{}) {
		t.Error("default manager should match legacy (unlabelled) containers")
	}
	// But must not see e2e containers bleed in.
	if m.matchesNamespace(map[string]string{labelNamespace: "e2e-abc"}) {
		t.Error("default manager should not match namespaced containers")
	}
}
