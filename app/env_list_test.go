package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iml885203/orbit/internal/envsync"
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

func TestBuildEnvListJSONDataExposesManagedRepositoryRevision(t *testing.T) {
	data := buildEnvListJSONData(environmentSelection{
		State: environmentSelectionSelected,
		ManagedSource: &envsync.RepositorySource{
			URL:    "https://github.com/example/envs.git",
			Ref:    "v1.2.3",
			Commit: "0123456789abcdef",
		},
		Environments: []environmentChoice{},
	})

	source := data.Environment.ManagedSource
	if source == nil || source.URL != "https://github.com/example/envs.git" ||
		source.Ref != "v1.2.3" || source.Commit != "0123456789abcdef" {
		t.Fatalf("managed source = %+v", source)
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
