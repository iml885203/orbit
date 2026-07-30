package devdb

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/dbstate"
)

// fetchDevDBProjects returns the explicitly configured SQL projects.
func fetchDevDBProjects(c *daemon.Client) (*DevDBProjectsResponse, error) {
	resp, err := c.Get("/api/devdb/projects")
	if err != nil {
		return nil, fmt.Errorf("devdb projects request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, daemon.ReadAPIError(resp)
	}

	var result DevDBProjectsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding devdb projects: %w", err)
	}
	return &result, nil
}

// resolveDBArgFromClient resolves a positional db/project argument to the
// database(s) it names, fetching the project list through the daemon. It lets
// publish/diff accept either a database name or a project name (see
// db_name_resolve.go). It returns the fetched projects too so callers that
// then act per-database (publish fan-out) reuse them instead of re-dialing.
// reset resolves inline (it needs the single-DB variant and nothing else).
func resolveDBArgFromClient(c *daemon.Client, arg string) (resolvedArg, []DevDBProject, error) {
	projects, err := fetchDevDBProjects(c)
	if err != nil {
		return resolvedArg{}, nil, err
	}
	r, err := resolveDBArg(projects.Projects, arg)
	return r, projects.Projects, err
}

// fetchAllPublishTargets is publishTargetsFrom over the project list
// fetched through the daemon — the work list `publish --all` and
// publish paths share. An empty merge is an error: every caller needs
// at least one database to act on.
func fetchAllPublishTargets(c *daemon.Client) ([]publishTargetRef, error) {
	projects, err := fetchDevDBProjects(c)
	if err != nil {
		return nil, err
	}
	targets := publishTargetsFrom(projects.Projects)
	if len(targets) == 0 {
		return nil, fmt.Errorf("no databases found — add .sqlproj paths to sqlserver.projects or check `orbit sqlserver list`")
	}
	return targets, nil
}

// fetchDevDBMeta returns the DB-workflow metadata for the active env,
// including whether the workflow is configured at all (db_configured).
func fetchDevDBMeta(c *daemon.Client) (*DevDBMetaResponse, error) {
	resp, err := c.Get("/api/devdb/meta")
	if err != nil {
		return nil, fmt.Errorf("devdb meta request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, daemon.ReadAPIError(resp)
	}

	var result DevDBMetaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding devdb meta: %w", err)
	}
	return &result, nil
}

// postDBStateEvent best-effort notifies the daemon of a CLI-driven
// DB operation (apply, reset, publish, publish_clean, snapshot).
// Failures are slog-only and never block the caller —
// the operation itself has already happened or failed by the time
// we get here.
func postDBStateEvent(c *daemon.Client, kind, db string, source dbstate.Source, status string, durationMs int64, errorMsg string) {
	fast := daemon.FastClone(c, 200*time.Millisecond)
	body := map[string]any{
		"kind":       kind,
		"db":         db,
		"source":     source,
		"status":     status,
		"durationMs": durationMs,
	}
	if errorMsg != "" {
		body["errorMsg"] = errorMsg
	}
	if _, err := fast.PostJSON("/api/db-state/event", body); err != nil {
		slog.Debug("failed to post db-state event", "error", err)
	}
}
