package daemon

import (
	"encoding/json"
	"testing"
)

func TestLifecycleWireUsesResourceVocabulary(t *testing.T) {
	tests := []struct {
		name       string
		value      any
		want       string
		legacyName string
	}{
		{
			name:       "up request",
			value:      UpRequest{Resources: []string{"redis"}},
			want:       "resources",
			legacyName: "services",
		},
		{
			name:       "status response",
			value:      StatusResponse{Resources: []ServiceStatus{}},
			want:       "resources",
			legacyName: "services",
		},
		{
			name:       "lifecycle response",
			value:      APIResponse{AffectedResources: []string{"redis"}},
			want:       "affected_resources",
			legacyName: "affected_services",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if _, ok := fields[tt.want]; !ok {
				t.Fatalf("%s missing from %s", tt.want, raw)
			}
			if _, ok := fields[tt.legacyName]; ok {
				t.Fatalf("legacy field %s present in %s", tt.legacyName, raw)
			}
		})
	}
}
