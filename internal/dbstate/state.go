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

// BuildEvent extends Event with the project that produced the image
// baseline this db mirrors. Build is the only kind that doesn't 1:1
// correspond to a db name on the CLI surface.
//
// Fields are duplicated rather than embedded because tygo emits embedded
// fields as a nested object (BuildEvent { Event: Event }), whereas Go's
// encoding/json flattens them — the wire shape then disagrees with the
// generated TS type. Flattening on the Go side keeps both ends consistent.
type BuildEvent struct {
	At         time.Time `json:"at"`
	Source     Source    `json:"source"`
	DurationMs int64     `json:"durationMs,omitempty"`
	Project    string    `json:"project"`
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

	LastReset *Event      `json:"lastReset,omitempty"`
	LastBuild *BuildEvent `json:"lastBuild,omitempty"`

	// ResetPending: build's image step succeeded but the post-build
	// reset for this db failed. Cleared on next successful Reset.
	ResetPending bool   `json:"resetPending,omitempty"`
	ResetError   string `json:"resetError,omitempty"`
}

// DerivedState collapses the event log into a display state and reason:
// reset_pending (a post-build reset failed — needs attention) beats
// modified (a local apply sits on top of the baseline) beats baseline.
// Single owner of this precedence for Go consumers. (The UI dropped its
// dbBadge mirror when apply/reset retired; the fields survive for
// version-skew history only.)
func (d DBState) DerivedState() (state, reason string) {
	switch {
	case d.ResetPending:
		return "reset_pending", d.ResetError
	case d.LastApply != nil:
		return "modified", ""
	default:
		return "baseline", ""
	}
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
