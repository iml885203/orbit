package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkflowChecksResolveLocalCallsWithoutGitMetadata(t *testing.T) {
	root := t.TempDir()
	workflows := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(workflows, 0o755); err != nil {
		t.Fatal(err)
	}
	child := "name: child\non:\n  workflow_call:\n    inputs:\n      message:\n        type: string\n        required: true\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
	if err := os.WriteFile(filepath.Join(workflows, "child.yml"), []byte(child), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]string{"valid": "message", "unknown local input": "wrong"} {
		t.Run(name, func(t *testing.T) {
			parent := "name: parent\non: push\njobs:\n  child:\n    uses: ./.github/workflows/child.yml\n    with:\n      " + input + ": hello\n"
			if err := os.WriteFile(filepath.Join(workflows, "parent.yml"), []byte(parent), 0o600); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			err := checkWorkflows(root, &output)
			if (err == nil) != (name == "valid") {
				t.Fatalf("unexpected workflow result: %v\n%s", err, output.String())
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Fatalf("verification fixture acquired Git metadata: %v", err)
	}
}
