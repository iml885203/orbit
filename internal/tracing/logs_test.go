package tracing

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	collectorlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

func resourceLogs(service, scope string, records ...*logspb.LogRecord) *logspb.ResourceLogs {
	return &logspb.ResourceLogs{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{strAttr("service.name", service)}},
		ScopeLogs: []*logspb.ScopeLogs{{
			Scope:      &commonpb.InstrumentationScope{Name: scope},
			LogRecords: records,
		}},
	}
}

func stringBody(s string) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
}

var logTraceID = []byte{0x0a, 0x1b, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

func logsExport() *collectorlogspb.ExportLogsServiceRequest {
	return &collectorlogspb.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{
		resourceLogs("api", "Payments.Controller",
			&logspb.LogRecord{
				TimeUnixNano: 1_000_000_000, ObservedTimeUnixNano: 1_000_000_500,
				SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_ERROR, SeverityText: "Error",
				Body:    stringBody("payment failed"),
				TraceId: logTraceID, SpanId: []byte{1, 1, 1, 1, 1, 1, 1, 1},
				Attributes: []*commonpb.KeyValue{
					strAttr("exception.type", "System.InvalidOperationException"),
					strAttr("exception.message", "card declined"),
					strAttr("exception.stacktrace", "at Payments.Charge()\n at Payments.Post()"),
					{Key: "Attempt", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 3}}},
				},
			},
		),
		resourceLogs("billing", "Billing.Worker",
			&logspb.LogRecord{
				TimeUnixNano: 2_000_000_000,
				SeverityText: "Warning",
				Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_KvlistValue{KvlistValue: &commonpb.KeyValueList{
					Values: []*commonpb.KeyValue{strAttr("Reason", "retry")},
				}}},
				TraceId: make([]byte, 16), SpanId: make([]byte, 8),
			},
		),
	}}
}

func postLogs(t *testing.T, url, contentType string, body []byte, gzipped bool) *http.Response {
	t.Helper()
	if gzipped {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = gz.Write(body)
		_ = gz.Close()
		body = buf.Bytes()
	}
	req, err := http.NewRequest(http.MethodPost, url+"/v1/logs", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	if gzipped {
		req.Header.Set("Content-Encoding", "gzip")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()
	return resp
}

// TestOTLPLogReceiver posts the same export as protobuf and as gzipped JSON
// and asserts both land as attributed, queryable records.
func TestOTLPLogReceiver(t *testing.T) {
	export := logsExport()
	pb, err := proto.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	js, err := protojson.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, contentType string
		body              []byte
		gzipped           bool
		wantResponseType  string
	}{
		{"protobuf", "application/x-protobuf", pb, false, "application/x-protobuf"},
		{"gzipped json", "application/json", js, true, "application/json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewStore(10)
			srv := httptest.NewServer(store.OTLPHandler())
			defer srv.Close()
			base := store.Logs().Query(0, LogFilter{}).LatestSeq

			resp := postLogs(t, srv.URL, tc.contentType, tc.body, tc.gzipped)
			if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != tc.wantResponseType {
				t.Fatalf("status %d content-type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
			}

			page := store.Logs().Query(0, LogFilter{})
			if len(page.Logs) != 2 || page.LatestSeq != base+2 || page.HasMore {
				t.Fatalf("page = %+v, want 2 records, latestSeq %d", page, base+2)
			}
			warn, failure := page.Logs[0], page.Logs[1]

			if failure.Seq != base+1 || failure.Service != "api" || failure.Scope != "Payments.Controller" ||
				failure.Body != "payment failed" || failure.SeverityNumber != 17 || failure.SeverityText != "Error" ||
				failure.TimeUnixNano != 1_000_000_000 || failure.ObservedTimeUnixNano != 1_000_000_500 ||
				failure.TraceID != "0a1b02030405060708090a0b0c0d0e0f" || failure.SpanID != "0101010101010101" {
				t.Errorf("error record = %+v", failure)
			}
			if failure.Attributes["exception.type"] != "System.InvalidOperationException" ||
				failure.Attributes["exception.stacktrace"] != "at Payments.Charge()\n at Payments.Post()" ||
				failure.Attributes["Attempt"] != "3" {
				t.Errorf("error attributes = %v", failure.Attributes)
			}

			if warn.Service != "billing" || warn.SeverityNumber != 13 || warn.TraceID != "" || warn.SpanID != "" {
				t.Errorf("warning record = %+v (severity from text, all-zero ids dropped)", warn)
			}
			if !strings.Contains(warn.Body, `"Reason"`) || !strings.Contains(warn.Body, `"retry"`) {
				t.Errorf("non-string body = %q, want it rendered as JSON", warn.Body)
			}
			if warn.ObservedTimeUnixNano == 0 {
				t.Error("observed time should fall back to the receive time")
			}
		})
	}
}

func TestOTLPLogReceiverRejectsBadRequests(t *testing.T) {
	store := NewStore(10)
	srv := httptest.NewServer(store.OTLPHandler())
	defer srv.Close()

	if resp := postLogs(t, srv.URL, "application/x-protobuf", []byte{0xff, 0xff}, false); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("undecodable body: status %d, want 400", resp.StatusCode)
	}
	huge := bytes.Repeat([]byte{'x'}, maxOTLPBodyBytes+1)
	if resp := postLogs(t, srv.URL, "application/x-protobuf", huge, false); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: status %d, want 413", resp.StatusCode)
	}
	resp, err := http.Get(srv.URL + "/v1/logs")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET: status %d, want 405", resp.StatusCode)
	}
	if page := store.Logs().Query(0, LogFilter{}); len(page.Logs) != 0 {
		t.Errorf("rejected requests stored %d records", len(page.Logs))
	}
}

// seqs lists a page's Seqs relative to base, the store's LatestSeq before the
// test ingested anything.
func seqs(page LogPage, base int64) []int64 {
	out := make([]int64, len(page.Logs))
	for i, r := range page.Logs {
		out[i] = r.Seq - base
	}
	return out
}

func storeWithRecords(n int) (*LogStore, int64) {
	store := NewLogStore(100, 1<<20)
	base := store.Query(0, LogFilter{}).LatestSeq
	for i := 0; i < n; i++ {
		store.Ingest([]LogRecord{{Service: "api", Body: fmt.Sprintf("line %d", i+1)}})
	}
	return store, base
}

func equalSeqs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLogStoreEvictsOldestPastEitherCeiling(t *testing.T) {
	byCount := NewLogStore(3, 1<<20)
	countBase := byCount.Query(0, LogFilter{}).LatestSeq
	for i := 0; i < 5; i++ {
		byCount.Ingest([]LogRecord{{Service: "api", Body: "line"}})
	}
	page := byCount.Query(0, LogFilter{})
	if got := seqs(page, countBase); !equalSeqs(got, []int64{5, 4, 3}) || page.LatestSeq != countBase+5 {
		t.Errorf("count ceiling kept %v (latest %d), want [5 4 3]", got, page.LatestSeq-countBase)
	}

	big := strings.Repeat("x", 1000)
	byBytes := NewLogStore(100, 2*(logRecordOverhead+len("api")+len(big)))
	bytesBase := byBytes.Query(0, LogFilter{}).LatestSeq
	byBytes.Ingest([]LogRecord{{Service: "api", Body: big}, {Service: "api", Body: big}, {Service: "api", Body: big}})
	if got := seqs(byBytes.Query(0, LogFilter{}), bytesBase); !equalSeqs(got, []int64{3, 2}) {
		t.Errorf("byte ceiling kept %v, want [3 2]", got)
	}

	tinyAttrs := func() map[string]string {
		attrs := make(map[string]string, 50)
		for i := 0; i < 50; i++ {
			attrs[fmt.Sprintf("k%02d", i)] = "v"
		}
		return attrs
	}
	attrRecordBytes := logRecordOverhead + len("api") + 50*(logAttributeOverhead+len("k00")+len("v"))
	byAttrs := NewLogStore(100, 2*attrRecordBytes)
	attrsBase := byAttrs.Query(0, LogFilter{}).LatestSeq
	byAttrs.Ingest([]LogRecord{{Service: "api", Attributes: tinyAttrs()}, {Service: "api", Attributes: tinyAttrs()},
		{Service: "api", Attributes: tinyAttrs()}})
	if got := seqs(byAttrs.Query(0, LogFilter{}), attrsBase); !equalSeqs(got, []int64{3, 2}) {
		t.Errorf("many tiny attributes kept %v, want [3 2]: each attribute should cost its overhead", got)
	}

	huge := NewLogStore(10, 1<<20)
	huge.Ingest([]LogRecord{{Body: strings.Repeat("é", maxLogValueBytes), Attributes: map[string]string{"exception.stacktrace": strings.Repeat("y", maxLogValueBytes+1)}}})
	rec := huge.Query(0, LogFilter{}).Logs[0]
	if !strings.HasSuffix(rec.Body, truncatedMark) || len(rec.Body) > maxLogValueBytes+len(truncatedMark) {
		t.Errorf("body not truncated to the value ceiling: %d bytes", len(rec.Body))
	}
	if !strings.HasSuffix(rec.Attributes["exception.stacktrace"], truncatedMark) {
		t.Error("oversized attribute value should be truncated, not dropped")
	}
}

func TestOTLPLogReceiverKeepsTheFirstAttributesUpToTheCap(t *testing.T) {
	attrs := make([]*commonpb.KeyValue, maxLogAttributes+72)
	for i := range attrs {
		attrs[i] = strAttr(fmt.Sprintf("a%03d", i), "v")
	}
	export := &collectorlogspb.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{
		resourceLogs("api", "Chatty", &logspb.LogRecord{Body: stringBody("many fields"), Attributes: attrs}),
	}}
	body, err := proto.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(10)
	srv := httptest.NewServer(store.OTLPHandler())
	defer srv.Close()

	postLogs(t, srv.URL, "application/x-protobuf", body, false)

	got := store.Logs().Query(0, LogFilter{}).Logs[0].Attributes
	last, first := fmt.Sprintf("a%03d", maxLogAttributes-1), fmt.Sprintf("a%03d", maxLogAttributes)
	if len(got) != maxLogAttributes || got["a000"] == "" || got[last] == "" || got[first] != "" {
		t.Errorf("kept %d attributes (has %s=%q, %s=%q), want the first %d in export order",
			len(got), last, got[last], first, got[first], maxLogAttributes)
	}
}

// TestLogStoreSinceZeroIsACursor is the fresh-daemon case: a cursor of the
// store's LatestSeq or 0 must read forward from the oldest record after it,
// not return the newest page and skip what came between.
func TestLogStoreSinceZeroIsACursor(t *testing.T) {
	store := NewLogStore(100, 1<<20)
	cursor := store.Query(0, LogFilter{}).LatestSeq
	for i := 0; i < 25; i++ {
		store.Ingest([]LogRecord{{Service: "api", Body: "line"}})
	}

	page := store.Query(10, LogFilter{HasSince: true, Since: cursor})
	if got := seqs(page, cursor); !equalSeqs(got, []int64{10, 9, 8, 7, 6, 5, 4, 3, 2, 1}) || !page.HasMore {
		t.Fatalf("since latestSeq = %v hasMore=%v, want the oldest 10 and more", got, page.HasMore)
	}
	read := 0
	for since := cursor; ; {
		page := store.Query(10, LogFilter{HasSince: true, Since: since})
		if page.CursorReset {
			t.Fatalf("since %d flagged as a reset cursor", since-cursor)
		}
		read += len(page.Logs)
		if !page.HasMore {
			break
		}
		since = page.Logs[0].Seq
	}
	if read != 25 {
		t.Errorf("paging from the cursor read %d records, want all 25", read)
	}

	fromZero := store.Query(10, LogFilter{HasSince: true, Since: 0})
	if got := seqs(fromZero, cursor); !equalSeqs(got, []int64{10, 9, 8, 7, 6, 5, 4, 3, 2, 1}) || fromZero.CursorReset {
		t.Errorf("since 0 = %v reset=%v, want the oldest 10", got, fromZero.CursorReset)
	}
	if got := seqs(store.Query(10, LogFilter{}), cursor); got[0] != 25 {
		t.Errorf("without since = %v, want the newest page", got)
	}
}

// TestLogStoreSeqSurvivesARestart models `orbit down && orbit up`: a cursor
// from the old store still reads every record of the new one.
func TestLogStoreSeqSurvivesARestart(t *testing.T) {
	before, base := storeWithRecords(5)
	cursor := before.Query(0, LogFilter{}).LatestSeq
	if cursor != base+5 || base < time.Now().Add(-time.Hour).UnixMicro() {
		t.Fatalf("seq should start from the wall clock in microseconds: base %d, latest %d", base, cursor)
	}
	for time.Now().UnixMicro() <= cursor {
		time.Sleep(time.Microsecond)
	}

	after, _ := storeWithRecords(3)
	page := after.Query(0, LogFilter{HasSince: true, Since: cursor})
	if len(page.Logs) != 3 || page.CursorReset {
		t.Errorf("old cursor on the restarted store returned %d records (reset=%v), want all 3", len(page.Logs), page.CursorReset)
	}
}

// TestLogStoreFlagsACursorItDidNotIssue covers the clock stepping back across
// a restart: the cursor is past LatestSeq, so the page says so and starts at
// the oldest retained record instead of waiting for Seq to catch up.
func TestLogStoreFlagsACursorItDidNotIssue(t *testing.T) {
	store, base := storeWithRecords(3)
	latest := store.Query(0, LogFilter{}).LatestSeq

	page := store.Query(2, LogFilter{HasSince: true, Since: latest + 1000})
	if got := seqs(page, base); !page.CursorReset || !equalSeqs(got, []int64{2, 1}) || !page.HasMore {
		t.Errorf("stale cursor = %v reset=%v hasMore=%v, want reset and the oldest 2", got, page.CursorReset, page.HasMore)
	}
	for _, f := range []LogFilter{{HasSince: true, Since: latest}, {Since: latest + 1000}} {
		if page := store.Query(0, f); page.CursorReset {
			t.Errorf("%+v: cursorReset set for a cursor this store issued or no cursor at all", f)
		}
	}
}

func TestParseSeverity(t *testing.T) {
	for in, want := range map[string]int32{
		"warn": SeverityWarn, "Warning": SeverityWarn, "ERROR": SeverityError, "information": SeverityInfo,
		"critical": SeverityFatal, "trace": SeverityTrace, "Verbose": SeverityTrace, "debug": SeverityDebug, "7": 7,
	} {
		if got, err := ParseSeverity(in); err != nil || got != want {
			t.Errorf("ParseSeverity(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "loud", "0", "25"} {
		if _, err := ParseSeverity(in); !errors.Is(err, ErrUnknownSeverity) {
			t.Errorf("ParseSeverity(%q) err = %v, want ErrUnknownSeverity", in, err)
		}
	}
	if SeverityName(14) != "WARN" || SeverityName(SeverityError) != "ERROR" || SeverityName(SeverityError-1) != "WARN" ||
		SeverityName(0) != "" {
		t.Errorf("SeverityName: 14=%q 17=%q 16=%q 0=%q", SeverityName(14), SeverityName(SeverityError),
			SeverityName(SeverityError-1), SeverityName(0))
	}
}
