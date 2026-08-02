// Package dbstate tracks the most recent apply/reset/build/publish and
// baseline refresh per database.
// It is the canonical "what state is db X in" file. Audit history (the
// full timeline of every command) is owned separately by internal/history.
package dbstate

import "time"

type Source string

const (
	SourceUI  Source = "ui"
	SourceCLI Source = "cli"
)

// Event records one occurrence of a db lifecycle action.
type Event struct {
	At         time.Time `json:"at"`
	Source     Source    `json:"source"`
	DurationMs int64     `json:"durationMs,omitempty"`
}

// DBState is the most-recent snapshot of one database's lifecycle.
// Fields requiring a held Store.mu for read are documented below.
type DBState struct {
	Name string `json:"name"`

	// LastApply is non-nil when the running db carries a local delta on
	// top of the image baseline (i.e. an apply happened more recently
	// than any build or reset). Cleared whenever the baseline moves:
	// successful Reset / Build / PublishClean / SnapshotRefreshed.
	LastApply   *Event `json:"lastApply,omitempty"`
	LastPublish *Event `json:"lastPublish,omitempty"`
	// BaselineAt records when reset's internal clean state was refreshed.
	BaselineAt *Event `json:"baselineAt,omitempty"`

	LastReset *Event `json:"lastReset,omitempty"`
}

// DerivedState collapses the event log into a display state and reason:
// modified (a local apply sits on top of the baseline) beats baseline.
// Single owner of this precedence for Go consumers.
func (d DBState) DerivedState() (state, reason string) {
	if d.LastApply != nil {
		return "modified", ""
	}
	return "baseline", ""
}

// Snapshot is the wire shape returned by GET /api/db-state and pushed
// over the SSE stream. Snapshot rather than delta so a slow subscriber
// converges on next event.
type Snapshot struct {
	Version   int                `json:"version"`
	UpdatedAt time.Time          `json:"updatedAt"`
	DBs       map[string]DBState `json:"dbs"`
}

const fileVersion = 1
