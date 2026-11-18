package bigquery

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	bq "google.golang.org/api/bigquery/v2"
)

// fakeBigQuery models only the tested protocol scenarios, not GoogleSQL or IAM.
type fakeBigQuery struct {
	mu              sync.Mutex
	server          *httptest.Server
	datasets        map[string]*bq.Dataset
	tables          map[string]*bq.Table
	jobs            map[string]*bq.Job
	rows            map[string][]order
	loads           int
	deletes         int
	requests        int
	pending         bool
	loadError       bool
	loadWarning     bool
	deny            bool
	metadataError   bool
	schemaError     bool
	recreateOnDel   bool
	loseLoadReply   bool
	loseCreateReply bool
}

type order struct {
	Customer string `json:"customer"`
	Amount   int    `json:"amount"`
}

func newFake() *fakeBigQuery {
	f := &fakeBigQuery{
		datasets: map[string]*bq.Dataset{}, tables: map[string]*bq.Table{},
		jobs: map[string]*bq.Job{}, rows: map[string][]order{},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

func reply(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func apiError(w http.ResponseWriter, code int, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": reason, "errors": []map[string]string{{"reason": reason}}}})
}

func (f *fakeBigQuery) serve(w http.ResponseWriter, req *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	if f.deny {
		apiError(w, 403, "accessDenied")
		return
	}
	path := strings.TrimPrefix(strings.TrimPrefix(req.URL.Path, "/upload"), "/bigquery/v2/")
	parts := strings.Split(path, "/")
	if len(parts) < 3 || parts[0] != "projects" {
		apiError(w, 404, "notFound")
		return
	}
	project := parts[1]
	if parts[2] == "jobs" {
		f.job(w, req, project, parts)
		return
	}
	if parts[2] != "datasets" {
		apiError(w, 404, "notFound")
		return
	}
	if len(parts) == 3 && req.Method == "POST" {
		var ds bq.Dataset
		if json.NewDecoder(req.Body).Decode(&ds) != nil || ds.DatasetReference == nil {
			apiError(w, 400, "invalid")
			return
		}
		key := project + "." + ds.DatasetReference.DatasetId
		if f.datasets[key] != nil {
			apiError(w, 409, "duplicate")
			return
		}
		ds.CreationTime = int64(f.requests)
		f.datasets[key] = &ds
		if f.loseCreateReply {
			f.loseCreateReply = false
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		reply(w, ds)
		return
	}
	if len(parts) < 4 {
		apiError(w, 404, "notFound")
		return
	}
	key := project + "." + parts[3]
	ds := f.datasets[key]
	if ds == nil {
		apiError(w, 404, "notFound")
		return
	}
	if len(parts) == 4 && req.Method == "GET" {
		if f.metadataError {
			apiError(w, 503, "backendError")
			return
		}
		reply(w, ds)
		return
	}
	if len(parts) == 4 && req.Method == "DELETE" {
		if f.recreateOnDel {
			f.datasets[key] = &bq.Dataset{Labels: map[string]string{"orbit_generation": "another-owner"}}
		}
		// The real API has no documented generation-match delete parameter.
		delete(f.datasets, key)
		f.deletes++
		reply(w, map[string]any{})
		return
	}
	if len(parts) == 5 && parts[4] == "tables" && req.Method == "POST" {
		if f.schemaError {
			apiError(w, 400, "invalidSchema")
			return
		}
		var table bq.Table
		if json.NewDecoder(req.Body).Decode(&table) != nil || table.TableReference == nil {
			apiError(w, 400, "invalid")
			return
		}
		tableKey := key + "." + table.TableReference.TableId
		if f.tables[tableKey] != nil {
			apiError(w, 409, "duplicate")
			return
		}
		table.CreationTime = int64(f.requests)
		f.tables[tableKey] = &table
		reply(w, table)
		return
	}
	if len(parts) == 6 && parts[4] == "tables" && req.Method == "GET" {
		if table := f.tables[key+"."+parts[5]]; table != nil {
			reply(w, table)
		} else {
			apiError(w, 404, "notFound")
		}
		return
	}
	apiError(w, 404, "notFound")
}

func (f *fakeBigQuery) job(w http.ResponseWriter, req *http.Request, project string, parts []string) {
	if req.Method == "GET" && len(parts) == 4 {
		job := f.jobs[project+"."+parts[3]]
		if job == nil || req.URL.Query().Get("location") != job.JobReference.Location {
			apiError(w, 404, "notFound")
			return
		}
		reply(w, job)
		return
	}
	if req.Method != "POST" || len(parts) != 3 {
		apiError(w, 404, "notFound")
		return
	}
	_, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || params["boundary"] == "" {
		apiError(w, 400, "multipartRequired")
		return
	}
	reader := multipart.NewReader(req.Body, params["boundary"])
	meta, err := reader.NextPart()
	if err != nil {
		apiError(w, 400, "metadataRequired")
		return
	}
	var job bq.Job
	if json.NewDecoder(meta).Decode(&job) != nil || job.JobReference == nil || job.Configuration == nil || job.Configuration.Load == nil {
		apiError(w, 400, "invalid")
		return
	}
	key := project + "." + job.JobReference.JobId
	if f.jobs[key] != nil {
		apiError(w, 409, "duplicate")
		return
	}
	media, err := reader.NextPart()
	if err != nil {
		apiError(w, 400, "fixtureRequired")
		return
	}
	var rows []order
	decoder := json.NewDecoder(media)
	for {
		var row order
		if err := decoder.Decode(&row); err == io.EOF {
			break
		} else if err != nil {
			apiError(w, 400, "invalidFixture")
			return
		}
		rows = append(rows, row)
	}
	job.Status = &bq.JobStatus{State: "DONE"}
	if f.pending {
		job.Status.State = "RUNNING"
	}
	if f.loadError {
		job.Status.ErrorResult = &bq.ErrorProto{Reason: "invalid", Message: "injected invalid row"}
	} else if !f.pending {
		dest := job.Configuration.Load.DestinationTable
		f.rows[dest.ProjectId+"."+dest.DatasetId+"."+dest.TableId] = rows
	}
	if f.loadWarning {
		job.Status.Errors = []*bq.ErrorProto{{Reason: "diagnosticOnly", Message: "injected nonfatal diagnostic"}}
	}
	job.Statistics = &bq.JobStatistics{Load: &bq.JobStatistics3{OutputRows: int64(len(rows))}}
	f.jobs[key] = &job
	f.loads++
	if f.loseLoadReply {
		f.loseLoadReply = false
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
		return
	}
	reply(w, job)
}

func (f *fakeBigQuery) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loads, f.deletes
}

func (f *fakeBigQuery) String() string {
	loads, deletes := f.counts()
	return fmt.Sprintf("load submissions=%d dataset deletes=%d", loads, deletes)
}
