package tracing

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// LogStore is the in-memory ring of OTLP log records shared by every service,
// kept in Seq order. It evicts the oldest records once either ceiling is
// exceeded: the record count or the approximate retained bytes. The byte
// ceiling is what bounds memory; the count keeps queries cheap when records
// are small.
//
// All mutable state is guarded by mu.
type LogStore struct {
	mu         sync.RWMutex
	maxRecords int
	maxBytes   int
	records    []LogRecord // ascending Seq, oldest first
	bytes      int
	lastSeq    int64
}

// Log ingest ceilings. Body and attribute values are truncated rather than
// dropped: an exception's message and stack trace are the reason to keep a
// record at all. The overheads approximate the heap a record and each of its
// attributes cost beyond their string bytes (struct, string headers, map
// slots, allocator rounding), so records with many tiny attributes still
// count against maxLogBytes at roughly their real size. maxLogAttributes is
// the OTel SDK's default attribute count limit; attributes past it are dropped
// in export order.
const (
	maxLogRecords        = 10000
	maxLogBytes          = 32 << 20
	maxLogValueBytes     = 16 << 10
	maxLogAttributes     = 128
	logRecordOverhead    = 256
	logAttributeOverhead = 96
)

const truncatedMark = "…[truncated]"

// NewLogStore returns a ring holding at most maxRecords records and roughly
// maxBytes of record data (each min 1).
//
// Seq starts from the wall clock in microseconds rather than 0 so a cursor
// taken before a daemon restart stays below every Seq the new daemon assigns.
// A clock stepped backwards can still break that; Query flags such a cursor
// with CursorReset.
func NewLogStore(maxRecords, maxBytes int) *LogStore {
	return &LogStore{
		maxRecords: max(maxRecords, 1),
		maxBytes:   max(maxBytes, 1),
		lastSeq:    time.Now().UnixMicro(),
	}
}

// Ingest assigns each record the next Seq and appends it, evicting the oldest
// records past either ceiling.
func (l *LogStore) Ingest(records []LogRecord) {
	if len(records) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, rec := range records {
		rec.Body = truncateLogValue(rec.Body)
		for k, v := range rec.Attributes {
			rec.Attributes[k] = truncateLogValue(v)
		}
		l.lastSeq++
		rec.Seq = l.lastSeq
		l.records = append(l.records, rec)
		l.bytes += logRecordBytes(&rec)
	}
	l.evictLocked()
}

// evictLocked drops the oldest records until both ceilings hold. Evicted
// slots are zeroed so their strings are collectable before the backing array
// is reallocated. Caller holds mu.
func (l *LogStore) evictLocked() {
	drop := 0
	for drop < len(l.records) && (len(l.records)-drop > l.maxRecords || l.bytes > l.maxBytes) {
		l.bytes -= logRecordBytes(&l.records[drop])
		l.records[drop] = LogRecord{}
		drop++
	}
	l.records = l.records[drop:]
}

func logRecordBytes(r *LogRecord) int {
	return logRecordOverhead + len(r.Body) + len(r.SeverityText) + len(r.TraceID) +
		len(r.SpanID) + len(r.Service) + len(r.Scope) +
		len(r.Attributes)*logAttributeOverhead + attrBytes(r.Attributes)
}

func truncateLogValue(s string) string {
	if len(s) <= maxLogValueBytes {
		return s
	}
	cut := maxLogValueBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + truncatedMark
}

// Query returns up to limit records that pass f, newest first (limit <= 0
// returns all). Without f.HasSince the page holds the newest matches. With it,
// the page holds the oldest matches after Since, so passing the page's
// highest Seq back as Since reads forward without gaps. A Since beyond the
// latest Seq was not issued by this store; the page then reads from the
// oldest retained record and sets CursorReset.
func (l *LogStore) Query(limit int, f LogFilter) LogPage {
	needle := strings.ToLower(f.Search)
	l.mu.RLock()
	defer l.mu.RUnlock()
	page := LogPage{Logs: []LogRecord{}, LatestSeq: l.lastSeq}
	take := func(r *LogRecord) bool {
		if !f.matches(r, needle) {
			return true
		}
		if limit > 0 && len(page.Logs) == limit {
			page.HasMore = true
			return false
		}
		page.Logs = append(page.Logs, *r)
		return true
	}
	if f.HasSince {
		since := f.Since
		if since > l.lastSeq {
			page.CursorReset = true
			since = 0
		}
		start := sort.Search(len(l.records), func(i int) bool { return l.records[i].Seq > since })
		for i := start; i < len(l.records); i++ {
			if !take(&l.records[i]) {
				break
			}
		}
		slices.Reverse(page.Logs)
		return page
	}
	for i := len(l.records) - 1; i >= 0; i-- {
		if !take(&l.records[i]) {
			break
		}
	}
	return page
}

// HasService reports whether any retained record came from service — how a
// service.name set in code rather than by Orbit's OTEL_SERVICE_NAME is still
// recognised as a filter target.
func (l *LogStore) HasService(service string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for i := range l.records {
		if l.records[i].Service == service {
			return true
		}
	}
	return false
}

// LogFilter narrows a log-record query. The zero value keeps every record.
type LogFilter struct {
	// Service keeps only records whose resource service.name equals it.
	Service string
	// MinSeverity keeps records at or above this OTLP severity number; records
	// with no severity are dropped once it is set. 0 keeps all.
	MinSeverity int32
	// TraceID keeps only records stamped with this trace id.
	TraceID string
	// Search is a case-insensitive substring matched against the body and
	// every attribute value.
	Search string
	// HasSince turns on cursor paging: only records with a Seq greater than
	// Since, oldest matches first. Since 0 is a real cursor, not "unset".
	HasSince bool
	Since    int64
}

func (f LogFilter) matches(r *LogRecord, lowerNeedle string) bool {
	if f.Service != "" && r.Service != f.Service {
		return false
	}
	if f.TraceID != "" && !strings.EqualFold(r.TraceID, f.TraceID) {
		return false
	}
	if f.MinSeverity > 0 && r.SeverityNumber < f.MinSeverity {
		return false
	}
	if lowerNeedle == "" || containsFold(r.Body, lowerNeedle) {
		return true
	}
	for _, v := range r.Attributes {
		if containsFold(v, lowerNeedle) {
			return true
		}
	}
	return false
}

// The lowest OTLP severity number of each level (TRACE 1-4, DEBUG 5-8, ...
// FATAL 21-24).
const (
	SeverityTrace int32 = 1
	SeverityDebug int32 = 5
	SeverityInfo  int32 = 9
	SeverityWarn  int32 = 13
	SeverityError int32 = 17
	SeverityFatal int32 = 21
)

var severityLevels = []struct {
	name   string
	lowest int32
}{
	{"trace", SeverityTrace}, {"debug", SeverityDebug}, {"info", SeverityInfo},
	{"warn", SeverityWarn}, {"error", SeverityError}, {"fatal", SeverityFatal},
}

// severityAliases are the level names common loggers put in SeverityText.
var severityAliases = map[string]string{
	"verbose":     "trace",
	"information": "info",
	"warning":     "warn",
	"critical":    "fatal",
}

// ErrUnknownSeverity is returned by ParseSeverity for a name or number outside
// the OTLP scale.
var ErrUnknownSeverity = errors.New("unknown severity")

// ParseSeverity maps a level name (trace, debug, info, warn, error, fatal and
// the aliases verbose, information, warning, critical; any case) or an OTLP severity
// number 1-24 to the lowest severity number of that level.
func ParseSeverity(s string) (int32, error) {
	name := strings.ToLower(strings.TrimSpace(s))
	if n, err := strconv.Atoi(name); err == nil {
		if n < 1 || n > 24 {
			return 0, fmt.Errorf("%w %q: numbers run 1-24", ErrUnknownSeverity, s)
		}
		return int32(n), nil
	}
	if alias, ok := severityAliases[name]; ok {
		name = alias
	}
	for _, lvl := range severityLevels {
		if lvl.name == name {
			return lvl.lowest, nil
		}
	}
	return 0, fmt.Errorf("%w %q: want trace, debug, info, warn, error, fatal or 1-24", ErrUnknownSeverity, s)
}

// SeverityName returns the upper-case level of an OTLP severity number, or ""
// when it is unspecified or out of range.
func SeverityName(n int32) string {
	if n < 1 || n > 24 {
		return ""
	}
	name := ""
	for _, lvl := range severityLevels {
		if n >= lvl.lowest {
			name = lvl.name
		}
	}
	return strings.ToUpper(name)
}
