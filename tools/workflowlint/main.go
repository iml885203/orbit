package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rhysd/actionlint"
)

func checkWorkflows(root string, output io.Writer) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve workflow root: %w", err)
	}
	project, err := actionlint.NewProject(root)
	if err != nil {
		return fmt.Errorf("load workflow project: %w", err)
	}
	linter, err := actionlint.NewLinter(output, &actionlint.LinterOptions{Oneline: true, WorkingDir: root})
	if err != nil {
		return fmt.Errorf("create workflow linter: %w", err)
	}
	findings, err := linter.LintDir(project.WorkflowsDir(), project)
	if err != nil {
		return fmt.Errorf("check workflows: %w", err)
	}
	if len(findings) != 0 {
		return fmt.Errorf("workflow validation found %d errors", len(findings))
	}
	return nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: workflowlint SOURCE_ROOT")
		os.Exit(2)
	}
	if err := checkWorkflows(os.Args[1], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
