package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/tracing"
	"github.com/spf13/cobra"
)

var (
	traceShowLogs bool
	traceLimit    int
	tracesFollow  bool
)

// Trace table column widths — shared by the header and every row, and by the
// truncate() calls that keep cell content within them.
const (
	traceColRoot     = 34
	traceColServices = 26
)

var traceRowFmt = fmt.Sprintf("%%-9s %%-%ds %%8s  %%-%ds %%s\n", traceColRoot, traceColServices)

// tracingCmd groups tracing meta-operations. Deliberately just `status`: with
// tracing on by default there is no enable/disable action to run — turning it
// off is a one-line `enabled: false` edit in the env YAML, the same mental
// model as every other env setting. A dedicated disable command would imply
// an importance the opt-out does not have.
func tracingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tracing",
		Short: "Inspect local tracing receiver status",
		Long:  "Inspect Orbit's built-in local tracing. Tracing is on by default; opt out per env with `tracing:\n  enabled: false`.",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show whether the trace receiver is healthy and where it is listening",
		Args:  cobra.NoArgs,
		RunE:  runTracingStatus,
	})
	return cmd
}

func runTracingStatus(_ *cobra.Command, _ []string) error {
	client, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return err
	}
	st, err := tracingStatus(client)
	if err != nil {
		return fmt.Errorf("tracing status failed: %w", err)
	}
	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), st, nil)
	}
	printTracingStatus(os.Stdout, st)
	return nil
}

func traceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trace [trace-id]",
		Short: "List traces, or show one trace by ID",
		Long:  "List recent local traces. Pass a trace ID to show its waterfall.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return runTraceShow(cmd, args)
			}
			return runTracesList(cmd, args)
		},
	}
	cmd.Flags().BoolVar(&traceShowLogs, "logs", false, "inline log lines that carry this trace id")
	// 50 ≈ a couple of terminal screenfuls; the API default (100) and the
	// dashboard (200) serve different surfaces.
	cmd.Flags().IntVar(&traceLimit, "limit", 50, "max number of traces to show")
	cmd.Flags().BoolVarP(&tracesFollow, "follow", "f", false, "stream new traces as they arrive")
	return cmd
}

func runTracesList(_ *cobra.Command, _ []string) error {
	client, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return err
	}

	if tracesFollow {
		return runTracesFollow(client)
	}

	traces, err := listTraces(client, traceLimit)
	if err != nil {
		return fmt.Errorf("traces failed: %w", err)
	}

	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), map[string]any{"traces": traces}, nil)
	}

	if len(traces) == 0 {
		st, _ := tracingStatus(client)
		printNoTraces(os.Stdout, st)
		return nil
	}

	printTraceHeader(os.Stdout)
	for i := range traces {
		printTraceRow(os.Stdout, &traces[i])
	}
	cli.Faint.Printf("\n%d trace(s). Inspect one with: orbit trace <id>\n", len(traces))
	return nil
}

// runTracesFollow streams new trace updates live off the SSE 'trace' event.
// A trace accumulates spans over time, so the same id may print more than once
// as it updates — that is the point of a live feed.
func runTracesFollow(client *daemon.Client) error {
	if cli.JSONOutput {
		enc := json.NewEncoder(os.Stdout)
		return streamTraces(client, func(t tracing.TraceSummary) { _ = enc.Encode(t) })
	}
	// Follow blocks silently on an idle stream, so a user who runs it while
	// tracing is off (or the receiver never bound) would just hang. Surface
	// that up front rather than leaving them staring at a dead feed.
	if st, err := tracingStatus(client); err == nil && st != nil && !st.ReceiverHealthy {
		printNoTraces(os.Stderr, st)
		return nil
	}
	fmt.Fprintln(os.Stderr, "Streaming traces (Ctrl+C to stop)…")
	printTraceHeader(os.Stdout)
	return streamTraces(client, func(t tracing.TraceSummary) { printTraceRow(os.Stdout, &t) })
}

// The trace client calls live here rather than on the public
// daemon.Client: their signatures name core-internal tracing types that
// an external module couldn't even spell, so they build on the client's
// exported primitives instead of widening its API.

// getJSON replicates the deleted Client methods' error contract exactly
// (the moved helpers must not change CLI-visible error text): transport
// errors get the caller's "<what> request failed" prefix, non-200s map
// straight through the daemon error contract, and decode errors say
// "decoding <what>".
func getJSON(c *daemon.Client, path, what string, out any) error {
	resp, err := c.Get(path)
	if err != nil {
		return fmt.Errorf("%s request failed: %w", what, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return daemon.ReadAPIError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding %s: %w", what, err)
	}
	return nil
}

// listTraces returns trace summaries newest-first, capped at limit (0 = all).
func listTraces(c *daemon.Client, limit int) ([]tracing.TraceSummary, error) {
	var out []tracing.TraceSummary
	if err := getJSON(c, fmt.Sprintf("/api/traces?limit=%d", limit), "traces", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// getTrace returns one full trace by id. Errors when the trace is
// unknown or has been evicted from the ring buffer.
func getTrace(c *daemon.Client, id string) (*tracing.Trace, error) {
	var out tracing.Trace
	if err := getJSON(c, "/api/traces/"+id, "trace", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// tracingStatus returns the collector health snapshot (configured, receiver
// health, actual port, counters).
func tracingStatus(c *daemon.Client) (*tracing.TracingStatus, error) {
	var out tracing.TracingStatus
	if err := getJSON(c, "/api/tracing/status", "tracing status", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// streamTraces opens the multiplexed /api/events stream and calls fn for
// each trace-summary update (the SSE `trace` event). Blocks until the
// connection drops or the daemon goes away. Used by `orbit trace -f`.
func streamTraces(c *daemon.Client, fn func(tracing.TraceSummary)) error {
	resp, err := c.Get("/api/events")
	if err != nil {
		return fmt.Errorf("events stream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var curEvent string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			curEvent = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			if curEvent != "trace" {
				continue
			}
			var sum tracing.TraceSummary
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &sum); err != nil {
				continue
			}
			fn(sum)
		}
	}
	return scanner.Err()
}

func printTraceHeader(w io.Writer) {
	fmt.Fprintf(w, traceRowFmt, "TIME", "ROOT", "DUR", "SERVICES", "ST")
}

func printTraceRow(w io.Writer, t *tracing.TraceSummary) {
	root := truncate(strings.TrimSpace(t.RootService+" "+t.RootName), traceColRoot)
	svcs := truncate(strings.Join(t.Services, "→"), traceColServices)
	st := cli.Green.Sprint("ok")
	if t.Status == "error" {
		st = cli.Red.Sprint("ERR")
	}
	fmt.Fprintf(w, traceRowFmt,
		tsClock(t.StartUnixNano), root, fmtMs(t.DurationMs), svcs, st)
}

// printNoTraces explains an empty trace list in terms of the receiver's real
// state, so the user can tell "off", "on but the receiver never bound", and
// "on and idle" apart — three situations that otherwise look identical.
func printNoTraces(w io.Writer, st *tracing.TracingStatus) {
	switch {
	case st == nil:
		fmt.Fprintln(w, "No traces captured yet.")
	case !st.Configured:
		fmt.Fprintln(w, "No traces — tracing is turned off for this env.")
		fmt.Fprintln(w, "It is on by default; remove the `tracing:\n  enabled: false` from the env YAML (then 'orbit down && orbit up') to re-enable.")
	case !st.ReceiverHealthy:
		fmt.Fprintf(w, "No traces — the trace receiver did not start (wanted port %d).\n", st.OTLPPort)
		if st.ReceiverError != "" {
			fmt.Fprintf(w, "  reason: %s\n", st.ReceiverError)
		}
		fmt.Fprintln(w, "  Free the port or set a different `tracing.otlp_port`, then 'orbit down && orbit up'.")
	default:
		fmt.Fprintf(w, "No traces captured yet — receiver healthy on port %d. Generate some traffic against a running service.\n", st.ActualPort)
	}
}

// printTracingStatus renders the `orbit tracing status` human view: one
// headline health line, then the receiver location and the running counters.
func printTracingStatus(w io.Writer, st *tracing.TracingStatus) {
	switch {
	case !st.Configured:
		fmt.Fprintln(w, cli.Faint.Sprint("tracing: off for this env (enabled: false)"))
		return
	case st.ReceiverHealthy:
		fmt.Fprintln(w, cli.Green.Sprintf("tracing: healthy — receiver on 127.0.0.1:%d", st.ActualPort))
		if st.ActualPort != st.OTLPPort {
			fmt.Fprintf(w, "  %s\n", cli.Faint.Sprintf("(wanted %d; moved after a port conflict)", st.OTLPPort))
		}
	default:
		fmt.Fprintf(w, "%s\n", cli.Red.Sprintf("tracing: on but the receiver is DOWN (wanted port %d)", st.OTLPPort))
		if st.ReceiverError != "" {
			fmt.Fprintf(w, "  %s\n", cli.Faint.Sprintf("reason: %s", st.ReceiverError))
		}
	}
	fmt.Fprintf(w, "  %s\n", cli.Faint.Sprintf("%d trace(s), %d span(s) total, %d span(s)/min", st.TraceCount, st.TotalSpans, st.SpansPerMin))
	if st.SpansDropped > 0 {
		fmt.Fprintf(w, "  %s\n", cli.Faint.Sprintf("%d span(s) dropped by ingest ceilings", st.SpansDropped))
	}
}

func runTraceShow(_ *cobra.Command, args []string) error {
	client, err := daemon.Dial(daemon.DefaultSocketPath())
	if err != nil {
		return err
	}
	trace, err := getTrace(client, args[0])
	if err != nil {
		return fmt.Errorf("trace failed: %w", err)
	}

	if cli.JSONOutput {
		return cli.WriteJSONSuccess(os.Stdout, commandString(), trace, nil)
	}

	renderWaterfall(os.Stdout, trace)
	if traceShowLogs {
		renderTraceLogs(os.Stdout, client, trace)
	}
	return nil
}

// renderWaterfall prints the trace header and a span timeline. Bars are
// positioned relative to the trace's own start/duration; this is a relative
// scale for spotting bottlenecks, not an absolute axis.
func renderWaterfall(w io.Writer, t *tracing.Trace) {
	header := fmt.Sprintf("trace %s  %s %s  %s  %d spans",
		truncate(t.TraceID, 12), t.RootService, t.RootName, fmtMs(t.DurationMs), t.SpanCount)
	if t.Status == "error" {
		header += "  " + cli.Red.Sprint("ERROR")
	}
	fmt.Fprintln(w, cli.Bold.Sprint(header))

	depth := spanDepths(t.Spans)
	const barWidth = 40
	total := t.DurationMs
	if total <= 0 {
		total = 1
	}
	for i := range t.Spans {
		sp := &t.Spans[i]
		indent := strings.Repeat("  ", depth[sp.SpanID])
		label := truncate(indent+spanLabel(sp), 30)

		startMs := float64(sp.StartUnixNano-t.StartUnixNano) / 1e6
		offset := int(startMs / total * barWidth)
		length := int(sp.DurationMs / total * barWidth)
		offset = clamp(offset, 0, barWidth-1)
		if length < 1 {
			length = 1
		}
		if offset+length > barWidth {
			length = barWidth - offset
		}
		bar := strings.Repeat(" ", offset) + strings.Repeat("█", length)
		bar += strings.Repeat(" ", barWidth-offset-length)

		dur := fmtMs(sp.DurationMs)
		if sp.Status == "error" {
			fmt.Fprintf(w, "%-30s │%s│ %s %s\n", label, cli.Red.Sprint(bar), dur, cli.Red.Sprint("✗"))
		} else {
			fmt.Fprintf(w, "%-30s │%s│ %s\n", label, bar, dur)
		}
	}
}

// renderTraceLogs prints the trace's log lines, joined server-side by the
// daemon (GET /api/traces/{id}/logs) — one implementation of the exact-id
// join, shared with the dashboard. See Server.traceLogs for the join contract
// and its ring-buffer ceiling.
func renderTraceLogs(w io.Writer, client *daemon.Client, t *tracing.Trace) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, cli.Bold.Sprint("logs for this trace:"))
	lines, err := client.TraceLogs(t.TraceID)
	if err != nil {
		fmt.Fprintf(w, "  %s\n", cli.Faint.Sprintf("(trace logs unavailable: %v)", err))
		return
	}
	if len(lines) == 0 {
		fmt.Fprintf(w, "  %s\n", cli.Faint.Sprint("(no log lines carry this trace id — they may have aged out of the log buffer)"))
		return
	}
	for _, l := range lines {
		fmt.Fprintf(w, "  %s %s\n", cli.Faint.Sprint(l.Service), l.Line)
	}
}

func spanLabel(sp *tracing.Span) string {
	name := sp.Name
	if sp.Service != "" {
		return sp.Service + " " + name
	}
	return name
}

// spanDepths computes each span's indentation depth by walking parent links.
// Spans whose parent is absent from the set are roots (depth 0). A visited
// guard bounds pathological cycles.
func spanDepths(spans []tracing.Span) map[string]int {
	byID := make(map[string]tracing.Span, len(spans))
	for _, sp := range spans {
		byID[sp.SpanID] = sp
	}
	depth := make(map[string]int, len(spans))
	for _, sp := range spans {
		d := 0
		cur := sp
		seen := map[string]bool{}
		for cur.ParentID != "" && !seen[cur.SpanID] {
			seen[cur.SpanID] = true
			parent, ok := byID[cur.ParentID]
			if !ok {
				break
			}
			d++
			cur = parent
		}
		depth[sp.SpanID] = d
	}
	return depth
}

func tsClock(unixNano int64) string {
	if unixNano <= 0 {
		return "--:--:--"
	}
	return time.Unix(0, unixNano).Local().Format("15:04:05")
}

func fmtMs(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", int(ms))
	}
	return fmt.Sprintf("%.2fs", ms/1000)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
