package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iml885203/orbit/config"
)

// Every shipped environment must load and validate against the current
// configuration schema.
func TestShippedEnvsLoad(t *testing.T) {
	envsDir := "../../envs"
	entries, err := os.ReadDir(envsDir)
	if err != nil {
		t.Fatalf("reading %s: %v", envsDir, err)
	}
	var checked int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		checked++
		path := filepath.Join(envsDir, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			if _, err := config.Load(path); err != nil {
				t.Errorf("Load(%s) failed: %v", path, err)
			}
		})
	}
	if checked == 0 {
		t.Fatal("no env yamls found — wrong envsDir?")
	}
}
