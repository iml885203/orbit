package daemonsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/iml885203/orbit/internal/config"
)

func TestPreparingStateHelper(t *testing.T) {
	if os.Getenv("ORBIT_STATE_HELPER") != "1" {
		return
	}
	fmt.Println("PREPARING")
	time.Sleep(10 * time.Second)
}

func TestStatePersistsOriginalPreparationInsteadOfMainCommand(t *testing.T) {
	cfg := &config.Config{Services: map[string]*config.Service{"api": {Name: "api", Type: "node", Command: "different-main ${TOKEN}"}}, Containers: map[string]*config.Container{}}
	srv := newTestServer(t, cfg)
	dir := t.TempDir()
	command := strconv.Quote(os.Args[0]) + " -test.run=^TestPreparingStateHelper$ -test.timeout=15s -- ${TOKEN}"
	manager := srv.app.ProcessMgr
	ready := make(chan struct{})
	var once sync.Once
	manager.OnOutput = func(_ string, line string) {
		if line == "PREPARING" {
			once.Do(func() { close(ready) })
		}
	}
	manager.OnOwnershipChanged = func(string) { srv.PersistState() }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- manager.Start(ctx, "api", dir, "different-main ${TOKEN}", map[string]string{"ORBIT_STATE_HELPER": "1", "TOKEN": "secret-must-not-be-persisted"}, []string{command}, 1)
	}()
	t.Cleanup(func() { cancel(); _ = manager.Stop("api", 100*time.Millisecond) })
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("preparation not ready")
	}
	persisted, err := srv.stateFile.Read()
	if err != nil {
		t.Fatal(err)
	}
	record := persisted.Processes["api"]
	if record.Stage != "pre_start" || record.Command != command || record.Dir != dir || record.PID <= 0 || record.PGID <= 0 {
		t.Fatalf("wrong stage identity: %+v", record)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var restored ProcessRecord
	if err := json.Unmarshal(encoded, &restored); err != nil || restored != record {
		t.Fatalf("record roundtrip=%+v %v", restored, err)
	}
	var legacy ProcessRecord
	if err := json.Unmarshal([]byte(`{"pid":1,"pgid":1,"command":"old-main","dir":"."}`), &legacy); err != nil || legacy.Command != "old-main" {
		t.Fatalf("legacy=%+v %v", legacy, err)
	}
	// The persisted preparation PID must take the existing recovery retirement
	// path, not be adopted as a healthy main service.
	if err := srv.app.ReconcilePersistedProcesses(map[string]struct{ PID, PGID int }{"recovered": {record.PID, record.PGID}}); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("preparation did not retire after recovery")
	}
	retired, err := srv.stateFile.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(retired.Processes) != 0 {
		t.Error("retired preparation still persisted")
	}
}
