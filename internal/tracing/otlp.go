package tracing

import (
	"compress/gzip"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	collectorlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectorpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// OTLPHandler returns the HTTP handler for the OTLP/HTTP receiver. It accepts
// traces on /v1/traces and log records on /v1/logs; metrics (which a
// service's OTLP exporter may also push when its endpoint env points here)
// are accepted and dropped so the exporter doesn't log delivery errors.
func (s *Store) OTLPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", s.handleOTLPTraces)
	mux.HandleFunc("/v1/logs", s.handleOTLPLogs)
	mux.HandleFunc("/v1/metrics", drainOK)
	return mux
}

func drainOK(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	w.WriteHeader(http.StatusOK)
}

// maxOTLPBodyBytes caps a single OTLP/HTTP export body (after gzip
// decompression). always_on sampling can produce large batches; this bounds the
// per-request memory the decoder allocates so one client can't push an
// arbitrarily large payload into the in-process store. Oversized requests are
// rejected with 413 and their contents never reach the store.
const maxOTLPBodyBytes = 8 * 1024 * 1024

func (s *Store) handleOTLPTraces(w http.ResponseWriter, r *http.Request) {
	req := &collectorpb.ExportTraceServiceRequest{}
	isJSON, ok := decodeOTLPExport(w, r, req, "trace")
	if !ok {
		return
	}
	s.Ingest(spansFromExport(req))
	writeOTLPResponse(w, isJSON, &collectorpb.ExportTraceServiceResponse{})
}

func (s *Store) handleOTLPLogs(w http.ResponseWriter, r *http.Request) {
	req := &collectorlogspb.ExportLogsServiceRequest{}
	isJSON, ok := decodeOTLPExport(w, r, req, "log")
	if !ok {
		return
	}
	s.logs.Ingest(logRecordsFromExport(req, time.Now()))
	writeOTLPResponse(w, isJSON, &collectorlogspb.ExportLogsServiceResponse{})
}

// decodeOTLPExport reads an export request body (gzip-aware, size-capped) into
// req, in protobuf or JSON per the Content-Type. On failure it has already
// written the error response and returns ok=false.
func decodeOTLPExport(w http.ResponseWriter, r *http.Request, req proto.Message, signal string) (isJSON, ok bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxOTLPBodyBytes)
	body, err := readBody(r)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return false, false
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false, false
	}

	isJSON = strings.Contains(r.Header.Get("Content-Type"), "json")
	if isJSON {
		err = protojson.Unmarshal(body, req)
	} else {
		err = proto.Unmarshal(body, req)
	}
	if err != nil {
		slog.Warn("otlp "+signal+" decode failed", "component", "tracing", "json", isJSON, "err", err)
		http.Error(w, "decode failed", http.StatusBadRequest)
		return isJSON, false
	}
	return isJSON, true
}

// writeOTLPResponse writes the empty success response in the same encoding
// the client used.
func writeOTLPResponse(w http.ResponseWriter, isJSON bool, resp proto.Message) {
	var out []byte
	if isJSON {
		out, _ = protojson.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
	} else {
		out, _ = proto.Marshal(resp)
		w.Header().Set("Content-Type", "application/x-protobuf")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

func readBody(r *http.Request) ([]byte, error) {
	var reader io.Reader = r.Body
	if strings.Contains(r.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, err
		}
		defer func() { _ = gz.Close() }()
		reader = gz
	}
	return io.ReadAll(reader)
}

// spansFromExport flattens an OTLP export request into Orbit spans, carrying
// the resource's service.name down onto each span.
func spansFromExport(req *collectorpb.ExportTraceServiceRequest) []Span {
	var spans []Span
	for _, rs := range req.GetResourceSpans() {
		service := serviceName(rs.GetResource().GetAttributes())
		for _, ss := range rs.GetScopeSpans() {
			for _, sp := range ss.GetSpans() {
				spans = append(spans, convertSpan(sp, service))
			}
		}
	}
	return spans
}

func convertSpan(sp *tracepb.Span, service string) Span {
	start := int64(sp.GetStartTimeUnixNano())
	end := int64(sp.GetEndTimeUnixNano())
	out := Span{
		TraceID:       hex.EncodeToString(sp.GetTraceId()),
		SpanID:        hex.EncodeToString(sp.GetSpanId()),
		ParentID:      hexIDOrEmpty(sp.GetParentSpanId()),
		Service:       service,
		Name:          sp.GetName(),
		Kind:          spanKind(sp.GetKind()),
		StartUnixNano: start,
		EndUnixNano:   end,
		DurationMs:    float64(end-start) / 1e6,
		Status:        spanStatus(sp.GetStatus()),
		StatusMsg:     sp.GetStatus().GetMessage(),
		Attributes:    flattenAttrs(sp.GetAttributes()),
	}
	return out
}

// hexIDOrEmpty hex-encodes a trace or span id, returning "" for the all-zero
// (absent) id: a parentless span is a root, and a log record emitted outside
// any span has no trace.
func hexIDOrEmpty(b []byte) string {
	for _, c := range b {
		if c != 0 {
			return hex.EncodeToString(b)
		}
	}
	return ""
}

// logRecordsFromExport flattens an OTLP logs export into Orbit log records,
// carrying the resource's service.name and the scope name down onto each.
// receivedAt stands in for an exporter that left the observed time unset.
func logRecordsFromExport(req *collectorlogspb.ExportLogsServiceRequest, receivedAt time.Time) []LogRecord {
	var records []LogRecord
	for _, rl := range req.GetResourceLogs() {
		service := serviceName(rl.GetResource().GetAttributes())
		for _, sl := range rl.GetScopeLogs() {
			scope := sl.GetScope().GetName()
			for _, lr := range sl.GetLogRecords() {
				records = append(records, convertLogRecord(lr, service, scope, receivedAt))
			}
		}
	}
	return records
}

func convertLogRecord(lr *logspb.LogRecord, service, scope string, receivedAt time.Time) LogRecord {
	observed := int64(lr.GetObservedTimeUnixNano())
	if observed == 0 {
		observed = receivedAt.UnixNano()
	}
	severity := int32(lr.GetSeverityNumber())
	if severity == 0 {
		severity, _ = ParseSeverity(lr.GetSeverityText())
	}
	var body string
	if lr.GetBody() != nil {
		body = stringifyValue(lr.GetBody())
	}
	attrs := lr.GetAttributes()
	if len(attrs) > maxLogAttributes {
		attrs = attrs[:maxLogAttributes]
	}
	return LogRecord{
		TimeUnixNano:         int64(lr.GetTimeUnixNano()),
		ObservedTimeUnixNano: observed,
		SeverityNumber:       severity,
		SeverityText:         lr.GetSeverityText(),
		Body:                 body,
		TraceID:              hexIDOrEmpty(lr.GetTraceId()),
		SpanID:               hexIDOrEmpty(lr.GetSpanId()),
		Service:              service,
		Scope:                scope,
		Attributes:           flattenAttrs(attrs),
	}
}

func serviceName(attrs []*commonpb.KeyValue) string {
	for _, kv := range attrs {
		if kv.GetKey() == "service.name" {
			if v := kv.GetValue().GetStringValue(); v != "" {
				return v
			}
		}
	}
	return "unknown"
}

func flattenAttrs(attrs []*commonpb.KeyValue) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]string, len(attrs))
	for _, kv := range attrs {
		out[kv.GetKey()] = stringifyValue(kv.GetValue())
	}
	return out
}

func stringifyValue(v *commonpb.AnyValue) string {
	switch v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return v.GetStringValue()
	case *commonpb.AnyValue_BoolValue:
		return strconv.FormatBool(v.GetBoolValue())
	case *commonpb.AnyValue_IntValue:
		return strconv.FormatInt(v.GetIntValue(), 10)
	case *commonpb.AnyValue_DoubleValue:
		return strconv.FormatFloat(v.GetDoubleValue(), 'g', -1, 64)
	default:
		// Arrays / kvlists / bytes — fall back to protojson text.
		b, err := protojson.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func spanKind(k tracepb.Span_SpanKind) string {
	switch k {
	case tracepb.Span_SPAN_KIND_SERVER:
		return "server"
	case tracepb.Span_SPAN_KIND_CLIENT:
		return "client"
	case tracepb.Span_SPAN_KIND_PRODUCER:
		return "producer"
	case tracepb.Span_SPAN_KIND_CONSUMER:
		return "consumer"
	case tracepb.Span_SPAN_KIND_INTERNAL:
		return "internal"
	default:
		return ""
	}
}

func spanStatus(st *tracepb.Status) string {
	switch st.GetCode() {
	case tracepb.Status_STATUS_CODE_ERROR:
		return "error"
	case tracepb.Status_STATUS_CODE_OK:
		return "ok"
	default:
		return "unset"
	}
}
