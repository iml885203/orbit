package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveDotnetAssemblyPath_UsesProjectTargetFramework(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "MetisAPI.csproj")
	if err := os.WriteFile(project, []byte(`<Project Sdk="Microsoft.NET.Sdk.Web">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
</Project>`), 0644); err != nil {
		t.Fatal(err)
	}

	net6 := filepath.Join(dir, "bin", "Debug", "net6.0")
	net8 := filepath.Join(dir, "bin", "Debug", "net8.0")
	if err := os.MkdirAll(net6, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(net8, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(net6, "MetisAPI.dll"), []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(net8, "MetisAPI.dll"), []byte("current"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveDotnetAssemblyPath(dir, "MetisAPI.csproj")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(net8, "MetisAPI.dll")
	if got != want {
		t.Fatalf("assembly path = %q, want %q", got, want)
	}
}

func TestResolveDotnetAssemblyPath_FallsBackToNewestBuildOutput(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "Api.csproj")
	if err := os.WriteFile(project, []byte(`<Project></Project>`), 0644); err != nil {
		t.Fatal(err)
	}

	oldDir := filepath.Join(dir, "bin", "Debug", "net6.0")
	newDir := filepath.Join(dir, "bin", "Debug", "net8.0")
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(oldDir, "Api.dll")
	newPath := filepath.Join(newDir, "Api.dll")
	if err := os.WriteFile(oldPath, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-time.Hour)
	newTime := time.Now()
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	got, err := resolveDotnetAssemblyPath(dir, "Api.csproj")
	if err != nil {
		t.Fatal(err)
	}
	if got != newPath {
		t.Fatalf("assembly path = %q, want %q", got, newPath)
	}
}
