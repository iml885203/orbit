package bigquery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	bq "google.golang.org/api/bigquery/v2"
	"google.golang.org/api/googleapi"
	"os"
	"reflect"
	"slices"
	"time"
)

type sandboxRunner struct {
	api        *bq.Service
	store      *receiptStore
	generation string
	instance   string
}

func apiStatus(err error, status int) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == status
}
func scopeFor(p Profile, runKey string) string { return digest([]byte(p.ID + "\x00" + runKey)) }

func (r *sandboxRunner) prepare(ctx context.Context, p Profile, runKey string, input inputs) (rec *Receipt, err error) {
	if p.Mode != "sandbox" || runKey == "" || len(runKey) > 256 {
		return nil, failure("sandbox_profile_and_run_required", nil)
	}
	scope := scopeFor(p, runKey)
	unlock, err := r.store.acquire(scope)
	if err != nil {
		return nil, err
	}
	defer unlock()
	config, _ := json.Marshal(p)
	rec, err = r.store.read(scope)
	switch {
	case errors.Is(err, os.ErrNotExist):
		generation := nonce()
		jobs := map[string]string{}
		states := map[string]string{}
		for _, table := range input.Tables {
			jobs[table.ID] = "orbit_load_" + generation + "_" + digest([]byte(table.ID))[:16]
			states[table.ID] = "planned"
		}
		rec = &Receipt{ProfileName: p.ID, Version: "orbit.bq.receipt.v1", StoreID: r.store.id, Scope: scope, RunKey: runKey, Generation: generation, RuntimeGeneration: r.generation, Instance: r.instance, ConfigHash: digest(config), FixtureHash: input.Hash, Target: p, DatasetID: "orbit_test_" + generation, Jobs: jobs, JobStates: states, TableGenerations: map[string]int64{}, Phase: "intent", AppRuntime: "unverified"}
	case err != nil:
		return nil, err
	case rec.StoreID != r.store.id || rec.RuntimeGeneration != r.generation:
		return rec, failure("foreign_generation_mutation_blocked", nil)
	case rec.ConfigHash != digest(config) || rec.FixtureHash != input.Hash:
		return rec, failure("sandbox_config_conflict", nil)
	case rec.Phase == "cleaned" || rec.Phase == "cleanup_intent":
		return rec, failure("new_run_key_required", nil)
	}
	rec.DataReady = false
	rec.LastError = ""
	if err = r.store.save(rec); err != nil {
		return rec, failure("receipt_write_failed", err)
	}
	defer func() {
		if err != nil {
			rec.LastError = errorCode(err)
			if saveErr := r.store.save(rec); saveErr != nil {
				err = errors.Join(err, failure("receipt_write_failed", saveErr))
			}
		}
	}()
	if rec.Phase == "intent" {
		_, err = r.api.Datasets.Insert(p.DataProject, &bq.Dataset{DatasetReference: &bq.DatasetReference{ProjectId: p.DataProject, DatasetId: rec.DatasetID}, Location: p.Location, DefaultTableExpirationMs: 86400000, Labels: map[string]string{"orbit_generation": rec.Generation, "orbit_scope": rec.Scope[:32]}}).Context(ctx).Do()
		if err != nil && !apiStatus(err, 409) {
			return rec, failure("dataset_create_unknown", err)
		}
		if err := r.verifyDataset(ctx, rec); err != nil {
			return rec, err
		}
		rec.Phase = "loading"
		if err = r.store.save(rec); err != nil {
			return rec, failure("receipt_write_failed", err)
		}
	}
	if err := r.verifyDataset(ctx, rec); err != nil {
		return rec, err
	}
	rec.Warnings = nil
	for _, table := range input.Tables {
		if err := r.prepareTable(ctx, rec, table); err != nil {
			return rec, err
		}
	}

	rec.Phase = "data_ready"
	rec.DataReady = true
	rec.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	rec.AppConfig = appConfig(p, rec.DatasetID)
	if err = r.store.save(rec); err != nil {
		return rec, failure("receipt_write_failed", err)
	}
	return rec, nil
}

func sameSchema(a, b *bq.TableSchema) bool {
	if a == nil || b == nil {
		return false
	}
	// BigQuery normalizes an omitted mode to NULLABLE in metadata responses.
	var normalize func([]*bq.TableFieldSchema) []map[string]any
	normalize = func(fields []*bq.TableFieldSchema) []map[string]any {
		out := make([]map[string]any, 0, len(fields))
		for _, f := range fields {
			if f == nil {
				return nil
			}
			mode := f.Mode
			if mode == "" {
				mode = "NULLABLE"
			}
			out = append(out, map[string]any{"name": f.Name, "type": f.Type, "mode": mode, "fields": normalize(f.Fields), "description": f.Description})
		}
		return out
	}
	return reflect.DeepEqual(normalize(a.Fields), normalize(b.Fields))
}

func (r *sandboxRunner) checkLoad(job *bq.Job, rec *Receipt, table string) error {
	if job == nil || job.Status == nil || job.Configuration == nil || job.Configuration.Load == nil {
		return failure("job_evidence_missing", nil)
	}
	target := job.Configuration.Load.DestinationTable
	if target == nil || target.ProjectId != rec.Target.DataProject || target.DatasetId != rec.DatasetID || target.TableId != table || job.JobReference == nil || job.JobReference.ProjectId != rec.Target.JobProject || job.JobReference.JobId != rec.Jobs[table] || job.JobReference.Location != rec.Target.Location || job.Configuration.Labels["orbit_generation"] != rec.Generation || job.Configuration.Labels["orbit_fixture"] != rec.FixtureHash[:32] {
		return failure("job_target_mismatch", nil)
	}
	if job.Status.ErrorResult != nil {
		return failure("load_failed", fmt.Errorf("load job %s (%s): %s at %s: %s", rec.Jobs[table], rec.Target.Location, job.Status.ErrorResult.Reason, job.Status.ErrorResult.Location, job.Status.ErrorResult.Message))
	}
	for _, diagnostic := range job.Status.Errors {
		if !slices.Contains(rec.Warnings, diagnostic.Reason) {
			rec.Warnings = append(rec.Warnings, diagnostic.Reason)
		}
	}
	if job.Statistics != nil && job.Statistics.Load != nil && job.Statistics.Load.BadRecords > 0 {
		return failure("fixture_rows_rejected", nil)
	}
	return nil
}

func (r *sandboxRunner) verifyDataset(ctx context.Context, rec *Receipt) error {
	ds, err := r.api.Datasets.Get(rec.Target.DataProject, rec.DatasetID).Context(ctx).Do()
	if err != nil {
		return failure("dataset_state_unknown", err)
	}
	if ds.DatasetReference == nil || ds.DatasetReference.ProjectId != rec.Target.DataProject || ds.DatasetReference.DatasetId != rec.DatasetID || ds.Labels["orbit_generation"] != rec.Generation || ds.Labels["orbit_scope"] != rec.Scope[:32] || ds.Location != rec.Target.Location || ds.CreationTime == 0 {
		return failure("ownership_mismatch", nil)
	}
	if rec.CreationTime != 0 && rec.CreationTime != ds.CreationTime {
		return failure("dataset_generation_changed", nil)
	}
	rec.CreationTime = ds.CreationTime
	return nil
}

func (r *sandboxRunner) clean(ctx context.Context, p Profile, runKey string, confirmed bool) (rec *Receipt, err error) {
	if p.Mode != "sandbox" || !confirmed {
		return nil, failure("cleanup_requires_confirmation_and_quiescence", nil)
	}
	scope := scopeFor(p, runKey)
	unlock, err := r.store.acquire(scope)
	if err != nil {
		return nil, err
	}
	defer unlock()
	rec, err = r.store.read(scope)
	if err != nil {
		var fault *operationError
		if errors.As(err, &fault) {
			return nil, err
		}
		return nil, failure("receipt_unavailable", err)
	}
	if rec.StoreID != r.store.id || rec.RuntimeGeneration != r.generation {
		return rec, failure("foreign_generation_mutation_blocked", nil)
	}
	config, _ := json.Marshal(p)
	if rec.ConfigHash != digest(config) {
		return rec, failure("sandbox_config_conflict", nil)
	}
	if rec.Phase == "cleaned" {
		return rec, nil
	}
	defer func() {
		if err != nil {
			rec.LastError = errorCode(err)
			if saveErr := r.store.save(rec); saveErr != nil {
				err = errors.Join(err, failure("receipt_write_failed", saveErr))
			}
		}
	}()
	// Once cleanup begins, prepare cannot resubmit any tracked job.
	if rec.CreationTime == 0 {
		return rec, failure("cleanup_generation_unknown", nil)
	}
	rec.Phase = "cleanup_intent"
	rec.DataReady = false
	if err = r.store.save(rec); err != nil {
		return rec, failure("receipt_write_failed", err)
	}
	for table, jobID := range rec.Jobs {
		if rec.JobStates[table] == "planned" || rec.JobStates[table] == "schema_only" {
			continue
		}
		job, jobErr := r.api.Jobs.Get(p.JobProject, jobID).Location(p.Location).Context(ctx).Do()
		// A missing planned job may still arrive after an ambiguous submission.
		if jobErr != nil {
			return rec, failure("cleanup_job_unknown", jobErr)
		}
		if err = r.checkLoad(job, rec, table); err != nil && errorCode(err) != "load_failed" && errorCode(err) != "fixture_rows_rejected" {
			return rec, err
		}
		if job.Status == nil || job.Status.State != "DONE" {
			return rec, failure("cleanup_job_pending", nil)
		}
	}
	if err := r.verifyDataset(ctx, rec); err != nil {
		if apiStatus(err, 404) {
			rec.Phase = "cleaned"
			rec.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
			return rec, r.store.save(rec)
		}
		return rec, err
	}
	if err = r.api.Datasets.Delete(p.DataProject, rec.DatasetID).DeleteContents(true).Context(ctx).Do(); err != nil && !apiStatus(err, 404) {
		return rec, failure("cleanup_unknown", err)
	}
	rec.Phase = "cleaned"
	rec.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err = r.store.save(rec); err != nil {
		return rec, failure("receipt_write_failed", err)
	}
	return rec, nil
}

func appConfig(p Profile, dataset string) map[string]string {
	out := map[string]string{}
	for field, name := range p.Mapping {
		switch field {
		case "job_project":
			out[name] = p.JobProject
		case "dataset":
			out[name] = p.DataProject + "." + dataset
		case "location":
			out[name] = p.Location
		}
	}
	return out
}

func (r *sandboxRunner) prepareTable(ctx context.Context, rec *Receipt, table tableInput) error {
	p := rec.Target
	var err error
	if rec.TableGenerations[table.ID] == 0 && rec.JobStates[table.ID] == "planned" {
		_, err = r.api.Tables.Insert(p.DataProject, rec.DatasetID, &bq.Table{TableReference: &bq.TableReference{ProjectId: p.DataProject, DatasetId: rec.DatasetID, TableId: table.ID}, Schema: table.Schema}).Context(ctx).Do()
		if err != nil && !apiStatus(err, 409) {
			return failure("schema_create_unknown", err)
		}
	}
	actual, getErr := r.api.Tables.Get(p.DataProject, rec.DatasetID, table.ID).Context(ctx).Do()
	if getErr != nil {
		return failure("schema_state_unknown", getErr)
	}
	if actual.CreationTime == 0 || (rec.TableGenerations[table.ID] != 0 && rec.TableGenerations[table.ID] != actual.CreationTime) {
		return failure("table_generation_changed", nil)
	}
	rec.TableGenerations[table.ID] = actual.CreationTime
	if !sameSchema(actual.Schema, table.Schema) {
		return failure("schema_mismatch", nil)
	}
	if len(bytes.TrimSpace(table.Fixture)) == 0 {
		rec.JobStates[table.ID] = "schema_only"
		if err = r.store.save(rec); err != nil {
			return failure("receipt_write_failed", err)
		}
		return nil
	}
	jobID := rec.Jobs[table.ID]
	job, getErr := r.api.Jobs.Get(p.JobProject, jobID).Location(p.Location).Context(ctx).Do()
	if apiStatus(getErr, 404) {
		if rec.JobStates[table.ID] != "planned" {
			return failure("job_state_unknown", getErr)
		}
		rec.JobStates[table.ID] = "submission_intent"
		if err = r.store.save(rec); err != nil {
			return failure("receipt_write_failed", err)
		}
		job, getErr = r.api.Jobs.Insert(p.JobProject, &bq.Job{JobReference: &bq.JobReference{ProjectId: p.JobProject, JobId: jobID, Location: p.Location}, Configuration: &bq.JobConfiguration{Labels: map[string]string{"orbit_generation": rec.Generation, "orbit_fixture": rec.FixtureHash[:32]}, Load: &bq.JobConfigurationLoad{DestinationTable: &bq.TableReference{ProjectId: p.DataProject, DatasetId: rec.DatasetID, TableId: table.ID}, SourceFormat: "NEWLINE_DELIMITED_JSON", WriteDisposition: "WRITE_EMPTY", MaxBadRecords: 0}}}).Media(bytes.NewReader(table.Fixture), googleapi.ContentType("application/octet-stream")).Context(ctx).Do()
		if apiStatus(getErr, 409) {
			job, getErr = r.api.Jobs.Get(p.JobProject, jobID).Location(p.Location).Context(ctx).Do()
		}
	}
	if getErr != nil {
		return failure("load_submit_unknown", getErr)
	}
	for {
		if err := r.checkLoad(job, rec, table.ID); err != nil {
			return err
		}
		if job.Status.State == "DONE" {
			rec.JobStates[table.ID] = "terminal"
			if err = r.store.save(rec); err != nil {
				return failure("receipt_write_failed", err)
			}
			break
		}
		select {
		case <-ctx.Done():
			return failure("job_pending", ctx.Err())
		case <-time.After(300 * time.Millisecond):
		}
		job, err = r.api.Jobs.Get(p.JobProject, jobID).Location(p.Location).Context(ctx).Do()
		if err != nil {
			return failure("job_state_unknown", err)
		}
	}

	return nil
}
