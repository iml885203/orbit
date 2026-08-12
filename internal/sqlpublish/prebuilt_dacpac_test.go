package sqlpublish

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDacpac_RestoresPrebuiltArtifactSetWithoutSourceOrSDK(t *testing.T) {
	toolsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(toolsDir, "sqlpackage"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", toolsDir)
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	projectDir := filepath.Join(root, "PlatformDB")
	if err := os.Mkdir(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"PlatformDB.dacpac", "CommonFiles.dacpac"} {
		if err := os.WriteFile(filepath.Join(projectDir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	outDir := t.TempDir()
	var log bytes.Buffer

	dacpac, fingerprint, code, err := buildDacpac(context.Background(), Opts{
		SQLProj:   filepath.Join(t.TempDir(), "missing", "PlatformDB.sqlproj"),
		OutDir:    outDir,
		DacpacDir: root,
	}, &log)
	if err != nil || code != CodeNone {
		t.Fatalf("buildDacpac: code=%s err=%v", code, err)
	}
	if fingerprint != "" {
		t.Fatalf("prebuilt artifact returned source fingerprint %q", fingerprint)
	}
	if dacpac != filepath.Join(outDir, "PlatformDB.dacpac") {
		t.Fatalf("dacpac = %q", dacpac)
	}
	for _, name := range []string{"PlatformDB.dacpac", "CommonFiles.dacpac"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Errorf("%s was not restored: %v", name, err)
		}
		if !strings.Contains(log.String(), "[artifact] "+name) {
			t.Errorf("artifact log omitted %s: %s", name, log.String())
		}
	}
}

func TestValidateDacpacArtifacts_DistinguishesLayoutFailures(t *testing.T) {
	missingRoot := filepath.Join(t.TempDir(), "missing")
	project := filepath.Join(t.TempDir(), "PlatformDB.sqlproj")
	if err := ValidateDacpacArtifacts(missingRoot, project); err == nil || !strings.Contains(err.Error(), "root not found") {
		t.Fatalf("missing root error = %v", err)
	}

	root := t.TempDir()
	if err := ValidateDacpacArtifacts(root, project); err == nil || !strings.Contains(err.Error(), "directory for project PlatformDB not found") {
		t.Fatalf("missing project error = %v", err)
	}

	projectDir := filepath.Join(root, "PlatformDB")
	if err := os.Mkdir(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDacpacArtifacts(root, project); err == nil || !strings.Contains(err.Error(), "contains no dacpac files") {
		t.Fatalf("missing leaf error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "Other.dacpac"), []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDacpacArtifacts(root, project); err == nil || !strings.Contains(err.Error(), "directory holds Other.dacpac") {
		t.Fatalf("misnamed leaf error = %v", err)
	}
}

func TestValidateDacpacArtifacts_RequiresExactProjectAndLeafCase(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(t.TempDir(), "PlatformDB.sqlproj")
	wrongProjectDir := filepath.Join(root, "platformdb")
	if err := os.Mkdir(wrongProjectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wrongProjectDir, "PlatformDB.dacpac"), []byte("leaf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDacpacArtifacts(root, project); err == nil || !strings.Contains(err.Error(), "incorrect casing") {
		t.Fatalf("project casing error = %v", err)
	}
	if err := os.Rename(wrongProjectDir, filepath.Join(root, "PlatformDB")); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(root, "PlatformDB")
	if err := os.Rename(filepath.Join(projectDir, "PlatformDB.dacpac"), filepath.Join(projectDir, "platformdb.dacpac")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDacpacArtifacts(root, project); err == nil || !strings.Contains(err.Error(), "incorrect casing") {
		t.Fatalf("leaf casing error = %v", err)
	}
}

func TestRecordPublishStateWhenAvailable_SkipsPrebuiltArtifact(t *testing.T) {
	var log bytes.Buffer
	recordPublishStateWhenAvailable(context.Background(), Opts{
		SQLProj: filepath.Join(t.TempDir(), "missing.sqlproj"),
	}, "", &log)
	if log.Len() != 0 {
		t.Fatalf("artifact publish attempted source-state recording: %s", log.String())
	}
}
