package releasepolicy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var pinnedAction = regexp.MustCompile(`^[^./][^@]*@[0-9a-f]{40}$`)

func TestReleaseTrustClosurePinsExternalActions(t *testing.T) {
	for _, name := range []string{
		"release.yml",
		"platform-smoke.yml",
		"sqlserver-smoke.yml",
		"sync-homebrew.yml",
		"sync-scoop.yml",
	} {
		path := filepath.Join(repositoryRoot(t), ".github", "workflows", name)
		var document yaml.Node
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal(contents, &document); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		walkScalars(&document, func(key, value *yaml.Node) {
			if key.Value != "uses" || strings.HasPrefix(value.Value, "./") {
				return
			}
			if !pinnedAction.MatchString(value.Value) {
				t.Errorf("%s:%d external action is not commit-pinned: %s", name, value.Line, value.Value)
			}
			if !strings.Contains(value.LineComment, "v") {
				t.Errorf("%s:%d action pin has no upstream version comment", name, value.Line)
			}
		})
	}
}

func TestPackageSyncVerifiesBeforeMintingWriteToken(t *testing.T) {
	for _, name := range []string{"sync-homebrew.yml", "sync-scoop.yml"} {
		contents, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := validatePackageSyncSecurity(contents); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestPackageSyncSecurityPolicyRejectsFailOpenMutations(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "sync-homebrew.yml"))
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]string{
		"verification continues on error": strings.Replace(string(contents),
			"uses: ./.github/actions/verify-orbit-release",
			"uses: ./.github/actions/verify-orbit-release\n        continue-on-error: true", 1),
		"conditional verification": strings.Replace(string(contents),
			"uses: ./.github/actions/verify-orbit-release",
			"uses: ./.github/actions/verify-orbit-release\n        if: ${{ always() }}", 1),
		"token mint always runs": strings.Replace(string(contents),
			"uses: actions/create-github-app-token@",
			"if: ${{ always() }}\n        uses: actions/create-github-app-token@", 1),
		"dispatch always runs": strings.Replace(string(contents),
			"      - uses: ./.github/actions/sync-package-repository",
			"      - if: ${{ always() }}\n        uses: ./.github/actions/sync-package-repository", 1),
	}
	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			if err := validatePackageSyncSecurity([]byte(mutation)); err == nil {
				t.Fatal("fail-open workflow mutation passed release security policy")
			}
		})
	}
}

func validatePackageSyncSecurity(contents []byte) error {
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return fmt.Errorf("parse workflow: %w", err)
	}
	workflow := document.Content[0]
	permissions, ok := mappingValue(workflow, "permissions")
	if !ok || scalarValue(permissions, "contents") != "read" || len(permissions.Content) != 2 {
		return fmt.Errorf("ordinary token must have only contents: read")
	}
	jobs, ok := mappingValue(workflow, "jobs")
	if !ok {
		return fmt.Errorf("workflow has no jobs")
	}
	update, ok := mappingValue(jobs, "update")
	if !ok {
		return fmt.Errorf("workflow has no update job")
	}
	steps, ok := mappingValue(update, "steps")
	if !ok || steps.Kind != yaml.SequenceNode {
		return fmt.Errorf("update job has no steps")
	}
	verifyIndex := stepIndex(steps, "uses", "./.github/actions/verify-orbit-release")
	tokenIndex := stepIndex(steps, "uses", "actions/create-github-app-token@")
	dispatchIndex := stepIndex(steps, "uses", "./.github/actions/sync-package-repository")
	if verifyIndex < 0 || tokenIndex < 0 || dispatchIndex < 0 || !(verifyIndex < tokenIndex && tokenIndex < dispatchIndex) {
		return fmt.Errorf("steps must verify release before token mint and dispatch")
	}
	verify := steps.Content[verifyIndex]
	if _, ok := mappingValue(verify, "continue-on-error"); ok {
		return fmt.Errorf("release verification must not continue on error")
	}
	if _, ok := mappingValue(verify, "if"); ok {
		return fmt.Errorf("release verification must not be conditional")
	}
	for _, index := range []int{tokenIndex, dispatchIndex} {
		if condition, ok := mappingValue(steps.Content[index], "if"); ok && strings.Contains(condition.Value, "always()") {
			return fmt.Errorf("token mint and dispatch must not run with always()")
		}
	}
	return nil
}

func TestReleasePublishesAndVerifiesBeforePackageSync(t *testing.T) {
	workflow := readWorkflow(t, "release.yml")
	jobs := mapping(t, workflow, "jobs")
	release := mapping(t, jobs, "release")
	steps := sequence(t, release, "steps")
	publishIndex := stepIndex(steps, "name", "Publish GitHub release")
	verifyIndex := stepIndex(steps, "name", "Verify immutable published release")
	if publishIndex < 0 || verifyIndex <= publishIndex {
		t.Fatal("release must verify the immutable published release after publication")
	}
	for _, name := range []string{"update-homebrew", "update-scoop"} {
		job := mapping(t, jobs, name)
		if scalar(t, job, "needs") != "release" {
			t.Fatalf("%s must depend on the verified release job", name)
		}
	}
}

func TestReleaseAssetManifestIsExact(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), ".github", "actions", "verify-orbit-release", "assets.txt")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "checksums.txt\norbit-darwin-amd64\norbit-darwin-arm64\norbit-linux-amd64\norbit-linux-arm64\norbit-windows-amd64.exe\norbit-windows-arm64.exe\norbit.spdx.json\n"
	if string(contents) != want {
		t.Fatalf("release asset manifest changed without updating its contract:\n%s", contents)
	}
}

func TestDependabotMaintainsPinnedActions(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), ".github", "dependabot.yml")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := yaml.Unmarshal(contents, &config); err != nil {
		t.Fatal(err)
	}
	updates, ok := config["updates"].([]any)
	if !ok {
		t.Fatal("dependabot has no updates")
	}
	for _, item := range updates {
		update := item.(map[string]any)
		if update["package-ecosystem"] == "github-actions" && update["directory"] == "/" {
			return
		}
	}
	t.Fatal("dependabot does not maintain GitHub Actions from repository root")
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func readWorkflow(t *testing.T, name string) *yaml.Node {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return document.Content[0]
}

func mapping(t *testing.T, node *yaml.Node, key string) *yaml.Node {
	t.Helper()
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	t.Fatalf("line %d has no %s mapping", node.Line, key)
	return nil
}

func mappingValue(node *yaml.Node, key string) (*yaml.Node, bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, false
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1], true
		}
	}
	return nil, false
}

func scalarValue(node *yaml.Node, key string) string {
	value, _ := mappingValue(node, key)
	if value == nil {
		return ""
	}
	return value.Value
}

func sequence(t *testing.T, node *yaml.Node, key string) *yaml.Node {
	t.Helper()
	value := mapping(t, node, key)
	if value.Kind != yaml.SequenceNode {
		t.Fatalf("%s is not a sequence", key)
	}
	return value
}

func scalar(t *testing.T, node *yaml.Node, key string) string {
	t.Helper()
	return mapping(t, node, key).Value
}

func stepIndex(steps *yaml.Node, key, value string) int {
	for index, step := range steps.Content {
		for child := 0; child+1 < len(step.Content); child += 2 {
			if step.Content[child].Value == key &&
				(step.Content[child+1].Value == value || strings.HasPrefix(step.Content[child+1].Value, value)) {
				return index
			}
		}
	}
	return -1
}

func walkScalars(node *yaml.Node, visit func(key, value *yaml.Node)) {
	if node.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(node.Content); index += 2 {
			key, value := node.Content[index], node.Content[index+1]
			if value.Kind == yaml.ScalarNode {
				visit(key, value)
			}
			walkScalars(value, visit)
		}
		return
	}
	for _, child := range node.Content {
		walkScalars(child, visit)
	}
}
