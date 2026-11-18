package bigquery

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/gofrs/flock"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/instance"
	bq "google.golang.org/api/bigquery/v2"
	"google.golang.org/api/option"
	"os"
	"path/filepath"
)

// Canonical records live outside disposable instance homes. A marker inside
// the home separates incarnations; recreating a name cannot inherit authority.
func storeDirectory(identity string) string {
	key := digest([]byte(instance.CurrentName() + "\x00" + identity))
	return filepath.Join(instance.BaseHome(), "bigquery", key)
}

func runtimeGeneration(create bool) (string, error) {
	dir := daemon.OrbitDir()
	path := filepath.Join(dir, "bq-generation.json")
	if create {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", err
		}
	}
	var lock *flock.Flock
	if create {
		lock = flock.New(filepath.Join(dir, "bq-generation.lock"))
		if ok, err := lock.TryLock(); err != nil || !ok {
			return "", failure("operation_busy", err)
		}
		defer func() { _ = lock.Unlock() }()
	}
	var marker struct {
		Generation string `json:"generation"`
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && create {
		marker.Generation = nonce()
		err = saveJSON(path, marker)
	} else if err == nil {
		err = json.Unmarshal(data, &marker)
	}
	if err != nil {
		return "", failure("runtime_generation_unavailable", err)
	}
	if decoded, decodeErr := hex.DecodeString(marker.Generation); decodeErr != nil || len(decoded) != 16 {
		return "", failure("runtime_generation_invalid", nil)
	}
	return marker.Generation, nil
}

func newRunner(ctx context.Context, identity string, create bool) (*sandboxRunner, error) {
	generation, err := runtimeGeneration(create)
	if err != nil {
		return nil, err
	}
	store, err := openStore(storeDirectory(identity))
	if err != nil {
		return nil, err
	}
	// ADC resolves in this short-lived CLI process, never in the daemon.
	api, err := bq.NewService(ctx, option.WithScopes(bq.BigqueryScope))
	if err != nil {
		return nil, failure("credentials_unavailable", err)
	}
	return &sandboxRunner{api: api, store: store, generation: generation, instance: instance.CurrentName()}, nil
}
