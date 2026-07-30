package devdb

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/iml885203/orbit/internal/sqlpublish"
)

func captureDiffSummary(t *testing.T, result sqlpublish.DiffResult) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = write
	defer func() { os.Stdout = original }()

	printDiffSummary(result)
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	_ = read.Close()
	return string(out)
}

func TestPrintDiffSummary_ChangedFilesOffersImpactAnalysis(t *testing.T) {
	out := captureDiffSummary(t, sqlpublish.DiffResult{
		DB: "AppDB",
		FileChanges: []sqlpublish.FileChange{{
			Action: "Modified",
			Path:   "AppDB/dbo/Stored Procedures/GetUser.sql",
		}},
	})
	if !strings.Contains(out, "Analyze database impact: orbit sqlserver diff AppDB --analyze") {
		t.Fatalf("missing impact-analysis next action:\n%s", out)
	}
	for _, implementationTerm := range []string{"--deep", "engine", "fingerprint"} {
		if strings.Contains(out, implementationTerm) {
			t.Errorf("output exposes implementation term %q:\n%s", implementationTerm, out)
		}
	}
}

func TestPrintDiffSummary_QuickSyncNeedsNoImplementationExplanation(t *testing.T) {
	out := captureDiffSummary(t, sqlpublish.DiffResult{DB: "AppDB", InSync: true, Quick: true})
	if !strings.Contains(out, "no changes since the last publish") {
		t.Fatalf("unexpected in-sync output:\n%s", out)
	}
	if strings.Contains(out, "--deep") || strings.Contains(out, "engine") {
		t.Fatalf("in-sync output exposes implementation details:\n%s", out)
	}
}

func TestDBDiffFlags_OnlyAnalyzeIsExposed(t *testing.T) {
	cmd := dbDiffCmd()
	if analyze := cmd.Flags().Lookup("analyze"); analyze == nil || analyze.Hidden {
		t.Fatal("--analyze must be the public database-impact option")
	}
	if deep := cmd.Flags().Lookup("deep"); deep != nil {
		t.Fatal("--deep must not remain as a second name for the same concept")
	}
}
