package app

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/internal/cmdmap/gaps"
	"github.com/iml885203/orbit/internal/history"
	"github.com/spf13/cobra"
)

func historyCmd() *cobra.Command {
	var source string
	var onlyNoCLI bool
	var onlyErrors bool
	var tail int

	cmd := &cobra.Command{
		Use:    "history",
		Short:  "Show recent UI/CLI actions translated to orbit commands",
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			client := daemon.NewClient(daemon.DefaultSocketPath())
			records, err := historyList(client, history.Filter{
				Source:     history.Source(source),
				OnlyNoCLI:  onlyNoCLI,
				OnlyErrors: onlyErrors,
				Limit:      tail,
			})
			if err != nil {
				return err
			}
			if cli.JSONOutput {
				return json.NewEncoder(os.Stdout).Encode(records)
			}
			printHistory(records)
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "filter source: ui or cli")
	cmd.Flags().BoolVar(&onlyNoCLI, "no-cli", false, "show only entries without CLI equivalents")
	cmd.Flags().BoolVar(&onlyErrors, "errors", false, "show only error entries")
	cmd.Flags().IntVar(&tail, "tail", 50, "number of entries to show")

	gapsCmd := &cobra.Command{
		Use:   "gaps",
		Short: "List UI actions with no CLI equivalent",
		RunE: func(_ *cobra.Command, _ []string) error {
			client := daemon.NewClient(daemon.DefaultSocketPath())
			items, err := historyGaps(client)
			if err != nil {
				return err
			}
			if cli.JSONOutput {
				return json.NewEncoder(os.Stdout).Encode(items)
			}
			printGaps(items)
			return nil
		},
	}
	cmd.AddCommand(gapsCmd)
	return cmd
}

// The history client calls live here rather than on the public
// daemon.Client: their signatures name core-internal types
// (history.Filter/Record, gaps.Gap) that an external module couldn't
// even spell, so they build on the client's exported primitives instead
// of widening its API.

func historyList(c *daemon.Client, filter history.Filter) ([]history.Record, error) {
	path := fmt.Sprintf("/api/history/list?limit=%d", 100)
	if filter.Source != "" {
		path += "&source=" + string(filter.Source)
	}
	if filter.OnlyNoCLI {
		path += "&onlyNoCli=true"
	}
	if filter.OnlyErrors {
		path += "&onlyErrors=true"
	}
	if filter.Limit > 0 {
		path = strings.Replace(path, "limit=100", fmt.Sprintf("limit=%d", filter.Limit), 1)
	}
	var out []history.Record
	if err := getJSON(c, path, "history", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func historyGaps(c *daemon.Client) ([]gaps.Gap, error) {
	var out []gaps.Gap
	if err := getJSON(c, "/api/history/gaps", "history gaps", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// postCLIHistoryEvent best-effort notifies the daemon of a CLI
// invocation. Tight timeouts and a swallowed error: recording history
// must never block or fail the command itself.
func postCLIHistoryEvent(rec history.Record) {
	fast := daemon.FastClone(daemon.NewClient(daemon.DefaultSocketPath()), 200*time.Millisecond)
	if _, err := fast.PostJSON("/api/history/cli-event", rec); err != nil {
		slog.Debug("failed to post history event", "error", err)
	}
}

func printHistory(records []history.Record) {
	if len(records) == 0 {
		fmt.Println("(no history)")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "TIME\tSOURCE\tSTATUS\tCOMMAND")
	for i := range records {
		rec := &records[i]
		cmd := rec.Command
		if cmd == "" {
			cmd = rec.Summary
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", rec.Timestamp.Format("2006-01-02 15:04:05"), rec.Source, rec.Status, cmd)
	}
	_ = w.Flush()
}

func printGaps(items []gaps.Gap) {
	if len(items) == 0 {
		fmt.Println("(no gaps recorded)")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "METHOD\tPATTERN\tCOUNT\tSUMMARY")
	for _, g := range items {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", g.Method, g.PathPattern, g.Count, g.Summary)
	}
	_ = w.Flush()
}
