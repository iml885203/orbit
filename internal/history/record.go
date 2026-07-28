package history

import "time"

type Source string

const (
	SourceUI     Source = "ui"
	SourceCLI    Source = "cli"
	SourceSystem Source = "system"
)

type Status string

const (
	StatusPending Status = "pending"
	StatusOK      Status = "ok"
	StatusError   Status = "error"
)

type Record struct {
	ID         string    `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	Source     Source    `json:"source"`
	Method     string    `json:"method,omitempty"`
	Path       string    `json:"path,omitempty"`
	Command    string    `json:"command,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	HasCLI     bool      `json:"hasCLI"`
	Status     Status    `json:"status"`
	DurationMs int64     `json:"durationMs,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type Filter struct {
	Source     Source
	OnlyNoCLI  bool
	OnlyErrors bool
	Limit      int
}
