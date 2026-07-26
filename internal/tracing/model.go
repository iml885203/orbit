// Package tracing implements Orbit's built-in local trace store: an
// in-memory ring buffer of OTLP spans, an OTLP/HTTP receiver, and query +
// subscription APIs for the CLI and dashboard.
//
// model.go holds only the wire types (exported to TypeScript via tygo). The
// store and OTLP ingest live in tracing.go and otlp.go and are excluded from
// tygo because they carry locks and protobuf types TS must never see.
package tracing

// Span is one span within a trace. Times are unix-nanoseconds (numbers in TS)
// so the UI can lay out the waterfall without parsing timestamps.
type Span struct {
	TraceID       string            `json:"traceId"`
	SpanID        string            `json:"spanId"`
	ParentID      string            `json:"parentId,omitempty"`
	Service       string            `json:"service"`
	Name          string            `json:"name"`
	Kind          string            `json:"kind,omitempty"` // server | client | producer | consumer | internal
	StartUnixNano int64             `json:"startUnixNano"`
	EndUnixNano   int64             `json:"endUnixNano"`
	DurationMs    float64           `json:"durationMs"`
	Status        string            `json:"status"` // ok | error | unset
	StatusMsg     string            `json:"statusMsg,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

// TraceSummary is a row in the trace list — everything needed to render the
// list without shipping the spans.
type TraceSummary struct {
	TraceID       string   `json:"traceId"`
	RootService   string   `json:"rootService"`
	RootName      string   `json:"rootName"`
	StartUnixNano int64    `json:"startUnixNano"`
	DurationMs    float64  `json:"durationMs"`
	SpanCount     int      `json:"spanCount"`
	Services      []string `json:"services"` // distinct service names, first-seen order
	Status        string   `json:"status"`   // ok | error
}

// Trace is a full trace: the summary fields plus every span, sorted by start
// time. Fields mirror TraceSummary (rather than embedding it) so the JSON and
// the tygo-generated TS stay flat — embedding would nest under a "TraceSummary"
// key in TS while Go promotes the fields inline.
type Trace struct {
	TraceID       string   `json:"traceId"`
	RootService   string   `json:"rootService"`
	RootName      string   `json:"rootName"`
	StartUnixNano int64    `json:"startUnixNano"`
	DurationMs    float64  `json:"durationMs"`
	SpanCount     int      `json:"spanCount"`
	Services      []string `json:"services"`
	Status        string   `json:"status"`
	Spans         []Span   `json:"spans"`
}

// TracingStatus is the collector health snapshot for the live indicator.
// Domain-prefixed (not just "Status") because tygo strips the package
// qualifier when exporting to the shared types.gen.ts namespace.
//
// Configured (the env opted in) and ReceiverHealthy (the OTLP listener
// actually bound and is serving) are deliberately separate: the receiver's
// bind is non-fatal, so an env can be Configured yet have no live receiver
// (e.g. port already taken and no fallback succeeded). A UI that conflates the
// two shows "healthy" while nothing is being collected — the exact trap this
// split closes. ActualPort is the port the receiver actually bound (may differ
// from the configured port after fallback); it is 0 when the receiver is not
// healthy.
type TracingStatus struct {
	Configured         bool   `json:"configured"`              // env opted into tracing
	ReceiverHealthy    bool   `json:"receiverHealthy"`         // OTLP listener bound and serving
	OTLPPort           int    `json:"otlpPort"`                // configured/desired port
	ActualPort         int    `json:"actualPort"`              // port actually bound; 0 when unhealthy
	ReceiverError      string `json:"receiverError,omitempty"` // bind failure reason, when any
	TraceCount         int    `json:"traceCount"`
	TotalSpans         int64  `json:"totalSpans"`
	SpansPerMin        int    `json:"spansPerMin"`
	LastReceivedUnixMs int64  `json:"lastReceivedUnixMs"` // 0 = never
	// SpansDropped counts spans rejected by the ingest ceilings (oversized
	// body, per-trace span cap, or attribute-bytes cap). Non-zero means the
	// store is shedding load, not that nothing is arriving.
	SpansDropped int64 `json:"spansDropped"`
}
