package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildEnvListJSONDataExposesUnavailableSelectionAndChoices(t *testing.T) {
	data := buildEnvListJSONData(environmentSelection{
		State:        environmentSelectionUnavailable,
		SelectedName: "original",
		SelectedPath: "/tmp/original.yaml",
		Environments: []environmentChoice{{
			Name: "renamed",
			Path: "/tmp/renamed.yaml",
		}},
	})

	if data.Operation != "env_list" || data.Environment.State != environmentSelectionUnavailable {
		t.Fatalf("data = %+v", data)
	}
	if data.Environment.SelectedName != "original" || len(data.Environment.Environments) != 1 {
		t.Fatalf("data = %+v", data)
	}
}

func TestBuildEnvListJSONDataUsesEmptyArray(t *testing.T) {
	raw, err := json.Marshal(buildEnvListJSONData(environmentSelection{
		State:        environmentSelectionNone,
		Environments: []environmentChoice{},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"environments":null`) {
		t.Fatalf("environments marshaled as null: %s", raw)
	}
}
