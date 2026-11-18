package bigquery

import (
	"context"
	"encoding/json"
	"errors"
	bq "google.golang.org/api/bigquery/v2"
	"google.golang.org/api/option"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func inspectionClient(ctx context.Context) (*bq.Service, error) {
	api, err := bq.NewService(ctx, option.WithScopes(bq.BigqueryScope))
	if err != nil {
		return nil, failure("credentials_unavailable", err)
	}
	return api, nil
}

func readRecords(dir string) ([]*Receipt, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []*Receipt{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []*Receipt{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || entry.Name() == "owner.json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var rec Receipt
		if err := json.Unmarshal(raw, &rec); err != nil {
			return nil, failure("receipt_invalid", err)
		}
		if err := validateReceipt(&rec, rec.Scope); err != nil {
			return nil, err
		}
		if entry.Name() != rec.Scope+".json" {
			return nil, failure("receipt_invalid", nil)
		}
		result = append(result, &rec)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Scope < result[j].Scope })
	return result, nil
}
