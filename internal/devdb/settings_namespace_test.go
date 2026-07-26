package devdb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/iml885203/orbit/daemon"
)

func TestSettings_DevDBNamespaceRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := daemon.LoadSettings(path)
	if err := s.Set("db_root", "/dev/db"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("sql_server_image", "example/sql-server:latest"); err != nil {
		t.Fatal(err)
	}

	s2 := daemon.LoadSettings(path)
	if s2.Get("db_root") != "/dev/db" || s2.Get("sql_server_image") != "example/sql-server:latest" {
		t.Fatalf("devdb settings did not round-trip: %+v", s2)
	}
}

func TestSettings_PreservesForeignExtensionNamespaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	seed := `{
  "extensions": {
    "devdb": {"sql_server_image": "example/sql-server:latest"},
    "teamx": {"custom_key": "custom_value"}
  }
}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	s := daemon.LoadSettings(path)
	if err := s.Set("workspace_root", "/dev/ws"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk struct {
		Extensions map[string]json.RawMessage `json:"extensions"`
	}
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	var teamx map[string]string
	if err := json.Unmarshal(disk.Extensions["teamx"], &teamx); err != nil || teamx["custom_key"] != "custom_value" {
		t.Fatalf("foreign namespace dropped or mangled by save: %s", data)
	}
	var state settingsState
	if err := json.Unmarshal(disk.Extensions["devdb"], &state); err != nil || state.SQLServerImage != "example/sql-server:latest" {
		t.Fatalf("devdb namespace lost by save: %s", data)
	}
}

func TestSettings_WireShapeStaysFlat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := daemon.LoadSettings(path)
	if err := s.Set("sql_server_image", "example/sql-server:latest"); err != nil {
		t.Fatal(err)
	}

	wire, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var flat map[string]json.RawMessage
	if err := json.Unmarshal(wire, &flat); err != nil {
		t.Fatal(err)
	}
	if _, ok := flat["sql_server_image"]; !ok {
		t.Fatalf("wire shape lost flat sql_server_image: %s", wire)
	}
	if _, ok := flat["extensions"]; ok {
		t.Fatalf("disk namespace leaked onto the wire: %s", wire)
	}
}

func TestSettings_CorruptDevDBBlobPreservedAcrossSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	seed := `{"extensions": {"devdb": "not-an-object"}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	s := daemon.LoadSettings(path)
	if err := s.Set("workspace_root", "/dev/ws"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk struct {
		Extensions map[string]json.RawMessage `json:"extensions"`
	}
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	if string(disk.Extensions["devdb"]) != `"not-an-object"` {
		t.Fatalf("corrupt blob not preserved: %s", data)
	}
}

func TestSettings_ShowHistoryFalseRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := daemon.LoadSettings(path)
	off := false
	if err := s.SetShowHistory(&off); err != nil {
		t.Fatal(err)
	}

	s2 := daemon.LoadSettings(path)
	if s2.ShowHistory == nil || *s2.ShowHistory {
		t.Fatalf("ShowHistory=&false did not round-trip: %+v", s2.ShowHistory)
	}
}
