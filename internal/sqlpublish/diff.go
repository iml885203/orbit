package sqlpublish

// Schema diff (drift) between a SQL project and a running database.
// Builds the project's dacpac on the host, then asks sqlpackage what a
// publish WOULD change — read-only, the database is never touched.
//
// Two shapes share one build:
//   - Diff:       /Action:DeployReport → structured XML → DiffResult
//                 (counts + per-object list + data-loss alerts).
//   - DiffScript: /Action:Script → the exact T-SQL a publish would run.
//
// The report uses the SAME properties a real publish uses, so the diff
// reflects what `orbit sqlserver publish` would actually do. Target-only schema
// objects are dropped: deleting an object from the project is a real schema
// change, protected by the same possible-data-loss gate as publish.

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// DiffOp is one schema change a publish would apply.
type DiffOp struct {
	Action     string `json:"action"`      // "Create" | "Alter" | "Drop"
	ObjectType string `json:"object_type"` // e.g. "SqlTable", "SqlProcedure"
	Name       string `json:"name"`        // e.g. "[dbo].[Customer]"
	DataLoss   bool   `json:"data_loss"`   // this op carries a data-loss alert
}

// DiffAlert is a warning sqlpackage raised about the deployment (most
// commonly possible data loss or data motion).
type DiffAlert struct {
	Kind    string `json:"kind"`    // sqlpackage Alert Name, e.g. "DataIssue"
	Message string `json:"message"` // human-facing text
}

// FileChange is one source-file difference versus the last publish —
// the fast approximation of "what changed" when the engine hasn't run.
// SSDT convention keeps one object per file, so the file name usually
// names the changed object; the exact T-SQL operations still need the
// engine.
type FileChange struct {
	Action string `json:"action"` // "Added" | "Modified" | "Deleted"
	Path   string `json:"path"`   // source-root-relative, e.g. "PaymentDB/dbo/Tables/X.sql"
}

// DiffResult is the structured outcome of a schema diff, shared by the
// CLI, the daemon endpoint, and the dashboard.
type DiffResult struct {
	DB       string      `json:"db"`
	InSync   bool        `json:"in_sync"` // no operations at all
	Created  int         `json:"created"`
	Altered  int         `json:"altered"`
	Dropped  int         `json:"dropped"`
	DataLoss bool        `json:"data_loss"` // any alert implies possible data loss
	Ops      []DiffOp    `json:"ops"`
	Alerts   []DiffAlert `json:"alerts"`
	// Quick marks a result produced without running the engine (see
	// publish_state.go): either "unchanged since the last publish"
	// (InSync) or a file-level change list (FileChanges).
	Quick bool `json:"quick,omitempty"`
	// FileChanges names the source files that moved since the last
	// publish, when that's all the fast path can prove. Ops stays empty;
	// an analyzed diff turns these into exact operations.
	FileChanges []FileChange `json:"file_changes,omitempty"`
	// Cached marks engine operations replayed from the diff-result cache
	// — the project and database state is identical to the last engine
	// run, so the ops are exact, just not recomputed.
	Cached bool `json:"cached,omitempty"`
}

// deploymentReport mirrors the sqlpackage DeployReport XML schema
// (http://schemas.microsoft.com/sqlserver/dac/DeployReport/2012/02).
type deploymentReport struct {
	Alerts     []reportAlert     `xml:"Alerts>Alert"`
	Operations []reportOperation `xml:"Operations>Operation"`
}

type reportAlert struct {
	Name   string        `xml:"Name,attr"`
	Issues []reportIssue `xml:"Issue"`
}

type reportOperation struct {
	Name  string       `xml:"Name,attr"` // Create | Alter | Drop
	Items []reportItem `xml:"Item"`
}

type reportItem struct {
	Value  string        `xml:"Value,attr"`
	Type   string        `xml:"Type,attr"`
	Issues []reportIssue `xml:"Issue"`
}

type reportIssue struct {
	ID    string `xml:"Id,attr"`
	Value string `xml:"Value,attr"`
}

// Diff builds the project and returns a structured schema diff against
// the target database. Read-only.
//
// Unless opts.Analyze, recorded state answers first when it can (see
// publish_state.go): "unchanged since last publish" or a replayed
// engine result or a file-level change list — all milliseconds instead
// of seconds. Any doubt falls through to the engine, whose result is
// then cached for identical later states.
func Diff(ctx context.Context, opts Opts, out io.Writer) (DiffResult, ErrorCode, error) {
	if !opts.Analyze {
		if res, ok := FastDiff(ctx, opts, out); ok {
			switch {
			case res.Cached:
				fmt.Fprintf(out, "[diff] state unchanged since last engine diff — replaying its result\n")
			case len(res.FileChanges) > 0:
				fmt.Fprintf(out, "[diff] %d source file(s) changed since last publish — run with analyze for database impact\n", len(res.FileChanges))
			default:
				fmt.Fprintf(out, "[diff] unchanged since last publish\n")
			}
			return res, CodeNone, nil
		}
	}
	dacpac, fingerprint, code, err := buildDacpac(ctx, opts, out)
	if err != nil {
		return DiffResult{}, code, err
	}
	markers, markerErr := dbMarkers(ctx, opts)
	xmlPath := filepath.Join(opts.OutDir, opts.DB+".deployreport.xml")
	if code, err := runReportAction(ctx, opts, dacpac, "DeployReport", xmlPath, out); err != nil {
		return DiffResult{}, code, err
	}
	data, err := os.ReadFile(xmlPath)
	if err != nil {
		return DiffResult{}, CodePublishFailed, fmt.Errorf("reading deploy report: %w", err)
	}
	result, code, err := parseDeployReport(opts.DB, data)
	if err == nil && markerErr == nil {
		// Cache the engine's answer for identical later states. Best-effort.
		if cacheErr := recordDiffCache(ctx, opts, fingerprint, markers, result); cacheErr != nil {
			fmt.Fprintf(out, "[diff] result not cached (next identical diff pays the engine again): %v\n", cacheErr)
		}
	}
	return result, code, err
}

// DiffScript builds the project and returns the T-SQL script a publish
// would run against the target. Read-only.
func DiffScript(ctx context.Context, opts Opts, out io.Writer) (string, ErrorCode, error) {
	dacpac, _, code, err := buildDacpac(ctx, opts, out)
	if err != nil {
		return "", code, err
	}
	sqlPath := filepath.Join(opts.OutDir, opts.DB+".deployscript.sql")
	if code, err := runReportAction(ctx, opts, dacpac, "Script", sqlPath, out); err != nil {
		return "", code, err
	}
	data, err := os.ReadFile(sqlPath)
	if err != nil {
		return "", CodePublishFailed, fmt.Errorf("reading deploy script: %w", err)
	}
	return string(data), CodeNone, nil
}

// runReportAction runs one read-only sqlpackage action, retrying once
// with composite objects when the target lacks a referenced project's
// shared objects (SQL72033 — e.g. a GRANT to a role the referenced
// project defines but a publish hasn't materialized yet). Publish does
// the same reactive retry, so a diff succeeds wherever a publish would
// self-heal — and its report then includes those composite creations.
func runReportAction(ctx context.Context, opts Opts, dacpac, action, outputPath string, out io.Writer) (ErrorCode, error) {
	code, err := runSqlpackageAction(ctx, opts, dacpac, action, outputPath, out)
	if err != nil && code == CodeReferenceUnresolved && !opts.IncludeComposite {
		fmt.Fprintf(out, "[diff] unresolved references — retrying with composite objects\n")
		opts.IncludeComposite = true
		return runSqlpackageAction(ctx, opts, dacpac, action, outputPath, out)
	}
	return code, err
}

// runSqlpackageAction runs a read-only sqlpackage action (DeployReport /
// Script) that writes to outputPath. It mirrors publishDacpac's arg shape
// and output capture but never mutates the target. IncludeComposite is
// honoured so an empty-server diff resolves referenced objects the same
// way a first publish would; Force is irrelevant to a report and omitted.
func runSqlpackageAction(ctx context.Context, opts Opts, dacpac, action, outputPath string, out io.Writer) (ErrorCode, error) {
	sqlpackage, err := SqlpackagePath()
	if err != nil {
		return CodeToolchainMissing, err
	}
	var capture bytes.Buffer
	tee := io.MultiWriter(out, &capture)

	args := sqlpackageArgs(opts, action, dacpac)
	args = append(args, "/OutputPath:"+outputPath)
	cmd := exec.CommandContext(ctx, sqlpackage, args...)
	cmd.Stdout = tee
	cmd.Stderr = tee
	if err := cmd.Run(); err != nil {
		return classifyPublish(capture.String()), fmt.Errorf("sqlpackage %s: %w", action, err)
	}
	return CodeNone, nil
}

// parseDeployReport turns the DeployReport XML into a DiffResult. Items
// whose Issue ids match a DataIssue alert are flagged data_loss.
func parseDeployReport(db string, data []byte) (DiffResult, ErrorCode, error) {
	var rep deploymentReport
	if err := xml.Unmarshal(data, &rep); err != nil {
		return DiffResult{}, CodePublishFailed, fmt.Errorf("parsing deploy report: %w", err)
	}

	res := DiffResult{DB: db}

	// Issue ids that belong to a data-loss ("DataIssue") alert; also
	// surface every alert's text.
	dataLossIssueIDs := map[string]bool{}
	for _, a := range rep.Alerts {
		if a.Name == "DataIssue" {
			res.DataLoss = true
		}
		for _, iss := range a.Issues {
			if a.Name == "DataIssue" && iss.ID != "" {
				dataLossIssueIDs[iss.ID] = true
			}
			msg := iss.Value
			if msg == "" {
				msg = a.Name
			}
			res.Alerts = append(res.Alerts, DiffAlert{Kind: a.Name, Message: msg})
		}
	}

	for _, op := range rep.Operations {
		for _, item := range op.Items {
			lossy := false
			for _, iss := range item.Issues {
				if dataLossIssueIDs[iss.ID] {
					lossy = true
				}
			}
			res.Ops = append(res.Ops, DiffOp{
				Action:     op.Name,
				ObjectType: item.Type,
				Name:       item.Value,
				DataLoss:   lossy,
			})
			switch op.Name {
			case "Create":
				res.Created++
			case "Alter":
				res.Altered++
			case "Drop":
				res.Dropped++
			}
		}
	}

	res.InSync = len(res.Ops) == 0
	// Stable display order: Drop (most destructive) → Alter → Create, then
	// by name.
	sort.SliceStable(res.Ops, func(i, j int) bool {
		ri, rj := ActionRank(res.Ops[i].Action), ActionRank(res.Ops[j].Action)
		if ri != rj {
			return ri < rj
		}
		return res.Ops[i].Name < res.Ops[j].Name
	})
	return res, CodeNone, nil
}

// ActionRank orders changes most-destructive-first. One table for both
// vocabularies — engine operations (Drop/Alter/Create) and file changes
// (Deleted/Modified/Added) — so the ordering can never drift between
// the two displays.
func ActionRank(action string) int {
	switch action {
	case "Drop", "Deleted":
		return 0
	case "Alter", "Modified":
		return 1
	case "Create", "Added":
		return 2
	default:
		return 3
	}
}

// Summary renders a one-line human summary of the diff.
func (r DiffResult) Summary() string {
	if r.InSync {
		if r.Quick {
			return "in sync — unchanged since last publish"
		}
		return "in sync — no schema changes"
	}
	if len(r.FileChanges) > 0 {
		counts := map[string]int{}
		for _, c := range r.FileChanges {
			counts[c.Action]++
		}
		var parts []string
		for _, action := range []string{"Deleted", "Modified", "Added"} {
			if counts[action] > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", counts[action], strings.ToLower(action)))
			}
		}
		return fmt.Sprintf("%d source file(s) changed since last publish (%s)",
			len(r.FileChanges), strings.Join(parts, ", "))
	}
	var parts []string
	if r.Created > 0 {
		parts = append(parts, fmt.Sprintf("%d to create", r.Created))
	}
	if r.Altered > 0 {
		parts = append(parts, fmt.Sprintf("%d to alter", r.Altered))
	}
	if r.Dropped > 0 {
		parts = append(parts, fmt.Sprintf("%d to drop", r.Dropped))
	}
	s := strings.Join(parts, ", ")
	if r.DataLoss {
		s += " — possible data loss"
	}
	return s
}
