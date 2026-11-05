package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/tracing"
	"github.com/spf13/cobra"
)

var (
	logRecordLimit    int
	logRecordSeverity string
	logRecordFilter   tracing.LogFilter
)

const logRecordColServiceMax = 20

func traceLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "List the OpenTelemetry log records services exported to Orbit",
		Long: `List the structured log records services export over OTLP, newest first
in --json and oldest first on the terminal.

Unlike 'orbit logs <resource>', which shows a resource's stdout, these records
keep their severity, trace and span ids, and attributes (including exception.*).
A service ships them only when its OpenTelemetry logging exporter is set up.

--search matches the body and every attribute value. --since takes a record's
seq (or latestSeq) and returns only newer records, oldest-first up to --limit,
so passing the highest seq back reads forward without gaps.`,
		Args: cobra.NoArgs,
		RunE: runTraceLogs,
	}
	cmd.Flags().StringVar(&logRecordFilter.Service, "service", "", "only records from this service")
	cmd.Flags().StringVar(&logRecordSeverity, "severity", "", "minimum level: trace, debug, info, warn, error or fatal")
	cmd.Flags().StringVar(&logRecordFilter.TraceID, "trace", "", "only records stamped with this trace id")
	cmd.Flags().StringVar(&logRecordFilter.Search, "search", "", "case-insensitive text in the body or an attribute value")
	cmd.Flags().Int64Var(&logRecordFilter.Since, "since", 0, "only records after this seq")
	cmd.Flags().IntVar(&logRecordLimit, "limit", 100, "max number of records to show (0 = all)")
	return cmd
}

func runTraceLogs(cmd *cobra.Command, _ []string) error {
	filter := logRecordFilter
	filter.HasSince = cmd.Flags().Changed("since")
	if logRecordLimit < 0 {
		return cli.NewInvalidArgumentError(fmt.Sprintf("--limit must be zero or more, got %d", logRecordLimit))
	}
	if filter.Since < 0 {
		return cli.NewInvalidArgumentError(fmt.Sprintf("--since must be zero or more, got %d", filter.Since))
	}
	if logRecordSeverity != "" {
		n, err := tracing.ParseSeverity(logRecordSeverity)
		if err != nil {
			return cli.NewInvalidArgumentError("--severity: " + err.Error())
		}
		filter.MinSeverity = n
	}

	client, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return err
	}
	page, err := listLogRecords(client, logRecordLimit, filter)
	if err != nil {
		return fmt.Errorf("log records failed: %w", err)
	}
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), page, nil)
	}
	if len(page.Logs) == 0 && filter == (tracing.LogFilter{}) {
		st, _ := tracingStatus(client)
		printNoLogRecords(os.Stdout, st)
		return nil
	}
	printLogRecordPage(os.Stdout, page, filter)
	return nil
}

// listLogRecords queries GET /api/otel/logs. MinSeverity travels as its OTLP
// number, which the daemon's ParseSeverity accepts like a level name.
func listLogRecords(c *daemon.Client, limit int, f tracing.LogFilter) (*tracing.LogPage, error) {
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	for key, value := range map[string]string{"service": f.Service, "trace": f.TraceID, "search": f.Search} {
		if value != "" {
			q.Set(key, value)
		}
	}
	if f.MinSeverity > 0 {
		q.Set("severity", strconv.Itoa(int(f.MinSeverity)))
	}
	if f.HasSince {
		q.Set("since", strconv.FormatInt(f.Since, 10))
	}
	resp, err := c.Get("/api/otel/logs?" + q.Encode())
	if err != nil {
		return nil, fmt.Errorf("log records request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, daemon.ReadAPIError(resp)
	}
	// A daemon that predates log records answers the unknown path with the
	// dashboard page rather than a JSON error.
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		return nil, errors.New("the running daemon does not keep OTLP log records; restart it with 'orbit down && orbit up'")
	}
	var page tracing.LogPage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("decoding log records: %w", err)
	}
	return &page, nil
}

// printLogRecordPage renders a page oldest first, like a log tail, and a
// footer naming the --since cursor that continues from it.
func printLogRecordPage(w io.Writer, page *tracing.LogPage, f tracing.LogFilter) {
	printLogRecords(w, page.Logs, "")
	if len(page.Logs) > 0 {
		fmt.Fprintln(w)
	}
	footer := fmt.Sprintf("%d record(s).", len(page.Logs))
	if page.CursorReset {
		footer = fmt.Sprintf("--since %d was not issued by this daemon (it restarted, or the clock moved back); showing from the oldest retained record. ",
			f.Since) + footer
	}
	switch {
	case f.HasSince && page.HasMore:
		footer += fmt.Sprintf(" More follow: --since %d.", page.Logs[0].Seq)
	case page.HasMore:
		footer += fmt.Sprintf(" Older matches withheld by --limit. Newer records later: --since %d.", page.LatestSeq)
	default:
		footer += fmt.Sprintf(" Newer records later: --since %d.", page.LatestSeq)
	}
	fmt.Fprintln(w, cli.Faint.Sprint(footer))
}

// printLogRecords prints newest-first records oldest first: one
// `time level service body` line with the trace id, then any exception on
// indented lines. indent prefixes every line.
func printLogRecords(w io.Writer, newestFirst []tracing.LogRecord, indent string) {
	width := 0
	for i := range newestFirst {
		width = max(width, min(len([]rune(newestFirst[i].Service)), logRecordColServiceMax))
	}
	for i := len(newestFirst) - 1; i >= 0; i-- {
		r := &newestFirst[i]
		line := fmt.Sprintf("%s%s %s %s %s", indent, logRecordClock(r), logRecordLevel(r),
			padRight(truncate(r.Service, logRecordColServiceMax), width),
			strings.ReplaceAll(r.Body, "\n", "\n"+indent+"    "))
		if r.TraceID != "" {
			line += " " + cli.Faint.Sprint("trace="+r.TraceID)
		}
		fmt.Fprintln(w, line)
		for _, ex := range exceptionLines(r.Attributes) {
			fmt.Fprintln(w, indent+"    "+ex)
		}
	}
}

func logRecordClock(r *tracing.LogRecord) string {
	ts := r.TimeUnixNano
	if ts == 0 {
		ts = r.ObservedTimeUnixNano
	}
	return time.Unix(0, ts).Local().Format("15:04:05.000")
}

func logRecordLevel(r *tracing.LogRecord) string {
	level := tracing.SeverityName(r.SeverityNumber)
	if level == "" {
		level = strings.ToUpper(r.SeverityText)
	}
	if level == "" {
		level = "-"
	}
	cell := padRight(level, 5)
	switch {
	case r.SeverityNumber >= tracing.SeverityError:
		return cli.Red.Sprint(cell)
	case r.SeverityNumber >= tracing.SeverityWarn:
		return cli.Yellow.Sprint(cell)
	default:
		return cell
	}
}

// exceptionLines renders the OTel exception.* attributes. A stack trace that
// already opens with "type: message" (as .NET's does) is printed alone.
func exceptionLines(attrs map[string]string) []string {
	head := attrs["exception.type"]
	if msg := attrs["exception.message"]; msg != "" {
		head = strings.TrimPrefix(head+": "+msg, ": ")
	}
	stack := strings.TrimRight(attrs["exception.stacktrace"], "\n")
	var lines []string
	if head != "" && !strings.HasPrefix(stack, head) {
		lines = append(lines, head)
	}
	if stack != "" {
		lines = append(lines, strings.Split(stack, "\n")...)
	}
	return lines
}

// printNoLogRecords explains an empty store in terms of the receiver's state,
// the same three cases printNoTraces separates.
func printNoLogRecords(w io.Writer, st *tracing.TracingStatus) {
	switch {
	case st != nil && !st.Configured:
		fmt.Fprintln(w, "No log records — tracing is turned off for this env.")
	case st != nil && !st.ReceiverHealthy:
		fmt.Fprintf(w, "No log records — the OTLP receiver did not start (wanted port %d). See 'orbit tracing status'.\n", st.OTLPPort)
	default:
		fmt.Fprintln(w, "No log records received yet. A service sends them only when its OpenTelemetry logging exporter is set up; its stdout stays in 'orbit logs <resource>'.")
	}
}
