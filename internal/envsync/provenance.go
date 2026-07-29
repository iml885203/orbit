package envsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/iml885203/orbit/atomicio"
)

const provenanceFilename = ".orbit-source.json"

// RepositorySource identifies the immutable repository state that produced
// the managed environment files. URL is credential-free so the metadata is
// safe to surface in CLI and daemon JSON.
type RepositorySource struct {
	URL    string `json:"url"`
	Ref    string `json:"ref,omitempty"`
	Commit string `json:"commit"`
}

func ReadRepositorySource(envsDir string) (RepositorySource, error) {
	data, err := os.ReadFile(filepath.Join(envsDir, provenanceFilename))
	if errors.Is(err, os.ErrNotExist) {
		return RepositorySource{}, nil
	}
	if err != nil {
		return RepositorySource{}, fmt.Errorf("read environment source: %w", err)
	}
	var source RepositorySource
	if err := json.Unmarshal(data, &source); err != nil {
		return RepositorySource{}, fmt.Errorf("parse environment source: %w", err)
	}
	return source, nil
}

func RemoveRepositorySource(envsDir string) error {
	err := os.Remove(filepath.Join(envsDir, provenanceFilename))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove environment source: %w", err)
	}
	return nil
}

func writeRepositorySource(envsDir string, source RepositorySource) error {
	data, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		return fmt.Errorf("encode environment source: %w", err)
	}
	data = append(data, '\n')
	if err := atomicio.WriteFile(filepath.Join(envsDir, provenanceFilename), data, 0644); err != nil {
		return fmt.Errorf("write environment source: %w", err)
	}
	return nil
}
