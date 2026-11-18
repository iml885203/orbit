package bigquery

import (
	"bytes"
	"encoding/json"
	"fmt"
	bq "google.golang.org/api/bigquery/v2"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const maxInputBytes = 8 << 20

type tableInput struct {
	ID      string
	Schema  *bq.TableSchema
	Fixture []byte
}
type inputs struct {
	Tables []tableInput
	Hash   string
}

func readInputs(root string, p Profile) (inputs, error) {
	var result inputs
	ids := make([]string, 0, len(p.Tables))
	for id := range p.Tables {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var fingerprint bytes.Buffer
	for _, id := range ids {
		spec := p.Tables[id]
		raw, err := readWorkspaceFile(root, spec.Schema)
		if err != nil {
			return result, err
		}
		var schema bq.TableSchema
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&schema); err != nil {
			return result, failure("schema_invalid", err)
		}
		if decoder.Decode(new(any)) != io.EOF || len(schema.Fields) == 0 {
			return result, failure("schema_invalid", nil)
		}
		if err := validateFields(schema.Fields); err != nil {
			return result, err
		}
		fixture, err := readWorkspaceFile(root, spec.Fixture)
		if err != nil {
			return result, err
		}
		lines := bytes.Split(bytes.TrimSpace(fixture), []byte("\n"))
		for _, line := range lines {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var row map[string]any
			if err := json.Unmarshal(line, &row); err != nil || row == nil {
				return result, failure("fixture_invalid", err)
			}
		}
		fmt.Fprintf(&fingerprint, "%s\x00%s\x00%s\n", id, digest(raw), digest(fixture))
		result.Tables = append(result.Tables, tableInput{ID: id, Schema: &schema, Fixture: fixture})
	}
	result.Hash = digest(fingerprint.Bytes())
	return result, nil
}

func readWorkspaceFile(root, path string) ([]byte, error) {
	if filepath.IsAbs(path) || path == "" {
		return nil, failure("input_path_invalid", nil)
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, failure("workspace_unavailable", err)
	}
	full, err := filepath.EvalSymlinks(filepath.Join(root, path))
	if err != nil {
		return nil, failure("input_unavailable", err)
	}
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, failure("input_path_outside_workspace", err)
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		return nil, failure("input_not_regular", err)
	}
	file, err := os.Open(full)
	if err != nil {
		return nil, failure("input_unavailable", err)
	}
	defer func() { _ = file.Close() }()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, failure("input_not_regular", err)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxInputBytes+1))
	if err != nil {
		return nil, failure("input_unavailable", err)
	}
	if len(data) > maxInputBytes {
		return nil, failure("input_too_large", nil)
	}
	return data, nil
}

func validateFields(fields []*bq.TableFieldSchema) error {
	names := map[string]bool{}
	for _, f := range fields {
		if f == nil || len(f.Name) > 300 || !datasetID.MatchString(f.Name) || names[strings.ToLower(f.Name)] {
			return failure("schema_field_invalid", nil)
		}
		names[strings.ToLower(f.Name)] = true
		switch f.Type {
		case "STRING", "INTEGER", "FLOAT", "BOOLEAN", "TIMESTAMP", "DATE", "DATETIME", "TIME", "NUMERIC", "BIGNUMERIC", "BYTES", "RECORD", "JSON", "GEOGRAPHY":
		default:
			return failure("schema_type_unsupported", nil)
		}
		if f.Mode != "" && f.Mode != "NULLABLE" && f.Mode != "REQUIRED" && f.Mode != "REPEATED" {
			return failure("schema_mode_invalid", nil)
		}
		if (f.Type == "RECORD") != (len(f.Fields) > 0) {
			return failure("schema_nested_fields_invalid", nil)
		}
		if err := validateFields(f.Fields); err != nil {
			return err
		}
		rest := *f
		rest.Name = ""
		rest.Type = ""
		rest.Mode = ""
		rest.Description = ""
		rest.Fields = nil
		if !reflect.DeepEqual(rest, bq.TableFieldSchema{}) {
			return failure("schema_options_unsupported", nil)
		}
	}
	return nil
}
