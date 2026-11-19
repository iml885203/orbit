package releasepolicy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCandidatePromotionTrustGraph(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCandidatePromotion(contents); err != nil {
		t.Fatal(err)
	}
	mutations := map[string]string{
		"native gate removed":         strings.Replace(string(contents), "[approval, candidate, platform-smoke, sqlserver-smoke]", "[approval, candidate, sqlserver-smoke]", 1),
		"producer not approved":       strings.Replace(string(contents), "candidate:\n    needs: approval", "candidate:\n    needs: []", 1),
		"foreign candidate":           strings.Replace(string(contents), "artifact-id: ${{ needs.candidate.outputs.artifact-id }}", "artifact-id: '123'", 1),
		"new binary at promotion":     strings.Replace(string(contents), "python3 scripts/candidate.py promote", "go build -o dist/orbit-linux-amd64 ./cmd/orbit\n          python3 scripts/candidate.py promote", 1),
		"promotion fail-open":         strings.Replace(string(contents), "name: Promote the verified inventory without rebuilding", "name: Promote the verified inventory without rebuilding\n        continue-on-error: true", 1),
		"blind duplicate publication": strings.Replace(string(contents), "if: steps.publication-state.outputs.accepted-existing != 'true'", "if: always()", 1),
	}
	for name, contents := range mutations {
		t.Run(name, func(t *testing.T) {
			var document yaml.Node
			if err := yaml.Unmarshal([]byte(contents), &document); err != nil {
				t.Fatalf("mutation must remain valid YAML: %v", err)
			}
			if err := validateCandidatePromotion([]byte(contents)); err == nil {
				t.Fatal("unsafe candidate workflow passed policy")
			}
		})
	}
}

func validateCandidatePromotion(contents []byte) error {
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return fmt.Errorf("parse workflow: %w", err)
	}
	jobs, _ := mappingValue(document.Content[0], "jobs")
	release, _ := mappingValue(jobs, "release")
	needs, _ := mappingValue(release, "needs")
	if needs == nil || needs.Kind != yaml.SequenceNode {
		return fmt.Errorf("promotion requires explicit gate dependencies")
	}
	for _, name := range []string{"approval", "candidate", "platform-smoke", "sqlserver-smoke"} {
		found := false
		for _, dependency := range needs.Content {
			found = found || dependency.Value == name
		}
		if !found {
			return fmt.Errorf("promotion does not require %s", name)
		}
	}
	producer, _ := mappingValue(jobs, "candidate")
	if scalarValue(producer, "needs") != "approval" {
		return fmt.Errorf("candidate producer must require approval")
	}
	for _, name := range []string{"platform-smoke", "sqlserver-smoke"} {
		consumer, _ := mappingValue(jobs, name)
		inputs, _ := mappingValue(consumer, "with")
		if scalarValue(consumer, "needs") != "candidate" || scalarValue(inputs, "artifact-id") != "${{ needs.candidate.outputs.artifact-id }}" {
			return fmt.Errorf("%s must test the producer's artifact", name)
		}
	}
	steps, _ := mappingValue(release, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return fmt.Errorf("promotion steps missing")
	}
	promote := stepIndex(steps, "run", "python3 scripts/candidate.py promote")
	probe := stepIndex(steps, "id", "publication-state")
	publish := stepIndex(steps, "name", "Publish GitHub release")
	if promote < 0 || probe <= promote || publish <= probe {
		return fmt.Errorf("promotion and live publication reconciliation must precede publication")
	}
	for _, index := range []int{promote, probe, publish} {
		if _, exists := mappingValue(steps.Content[index], "continue-on-error"); exists {
			return fmt.Errorf("promotion and publication verification must fail closed")
		}
	}
	if scalarValue(steps.Content[publish], "if") != "steps.publication-state.outputs.accepted-existing != 'true'" {
		return fmt.Errorf("publication must reconcile an accepted mutation before retrying")
	}
	for _, step := range steps.Content {
		run := scalarValue(step, "run")
		if strings.Contains(run, "go build") || strings.Contains(run, "pnpm ") {
			return fmt.Errorf("promotion cannot rebuild product assets")
		}
	}
	return nil
}

func TestCandidateProducerAndPRChecksHaveNoWriteAuthority(t *testing.T) {
	for _, name := range []string{"candidate.yml", "ci.yml", "platform-smoke.yml", "sqlserver-smoke.yml"} {
		workflow := readWorkflow(t, name)
		permissions := mapping(t, workflow, "permissions")
		walkScalars(permissions, func(key, value *yaml.Node) {
			if value.Value != "read" && value.Value != "none" {
				t.Errorf("%s grants %s:%s to candidate validation", name, key.Value, value.Value)
			}
		})
		jobs := mapping(t, workflow, "jobs")
		for index := 1; index < len(jobs.Content); index += 2 {
			if permissions, exists := mappingValue(jobs.Content[index], "permissions"); exists {
				walkScalars(permissions, func(key, value *yaml.Node) {
					if value.Value != "read" && value.Value != "none" {
						t.Errorf("%s job grants %s:%s", name, key.Value, value.Value)
					}
				})
			}
		}
	}
}
