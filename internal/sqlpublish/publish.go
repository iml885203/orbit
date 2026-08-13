// Package sqlpublish builds a SQL project on the host and publishes
// the dacpac straight to a SQL Server port — no docker cp, no
// container-side sqlpackage (on Apple Silicon that also means native
// arm64 instead of amd64 emulation). It is the generic publish path:
// any .sqlproj works, nothing here knows about any team's image build
// flow.
//
// Streaming output goes to the io.Writer passed by the caller — the
// package never touches stdout/stderr directly. The CLI passes
// os.Stdout; the daemon passes its dbops manager which broadcasts each
// line as SSE frames.
package sqlpublish

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrorCode is the stable, wire-facing classification of a failed
// publish. The dashboard maps codes to actionable hints; free-form
// error text is for humans reading logs.
type ErrorCode string

const (
	CodeNone                   ErrorCode = ""
	CodeToolchainMissing       ErrorCode = "toolchain_missing"
	CodeSQLProjectNotFound     ErrorCode = "sql_project_not_found"
	CodeDacpacArtifactMissing  ErrorCode = "dacpac_artifact_missing"
	CodeBuildFailed            ErrorCode = "build_failed"
	CodePublishBlockedDataLoss ErrorCode = "publish_blocked_data_loss"
	CodeSQLServerUnavailable   ErrorCode = "sql_server_unavailable"
	CodeDatabaseBusy           ErrorCode = "database_busy"
	CodePublishFailed          ErrorCode = "publish_failed"
	CodeCleanStateMissing      ErrorCode = "reset_clean_state_missing"
	CodeResetRestoreFailed     ErrorCode = "reset_restore_failed"
	CodeResetPrepareFailed     ErrorCode = "reset_prepare_failed"
	CodeResetPartial           ErrorCode = "reset_partial"
	// CodeReferenceUnresolved: publish couldn't resolve objects a
	// referenced dacpac defines (SQL72033) — the shared roles/schemas
	// aren't on the server yet. Publish self-heals by retrying with
	// composite objects; this code is the retry trigger, rarely surfaced.
	CodeReferenceUnresolved ErrorCode = "reference_unresolved"
)

// Opts captures everything Publish needs. SQLProj and OutDir are
// absolute paths, pre-resolved by the caller.
type Opts struct {
	DB      string
	SQLProj string // absolute path to .sqlproj
	OutDir  string // tempdir receiving dotnet build artifacts
	Host    string // SQL Server host as seen from this machine (e.g. "localhost")
	Port    int    // published container port
	// TargetID distinguishes env/image targets that reuse the same host port.
	// Publish-state and diff caches never cross this boundary.
	TargetID string
	User     string // usually "sa"
	Password string
	Force    bool // /p:BlockOnPossibleDataLoss=false
	// IncludeComposite deploys objects from referenced dacpacs too
	// (/p:IncludeCompositeObjects=true). Bootstrap needs it: on an
	// empty server the shared objects (roles, schemas) a project's
	// references define don't exist yet; a converge publish against a
	// prepared server deliberately leaves them alone.
	IncludeComposite bool
	// Analyze makes Diff compute exact object operations and data-loss
	// warnings instead of returning the source-file approximation.
	Analyze bool
	// DacpacDir is an invocation-scoped root containing one build-output
	// directory per SQL project. Empty keeps the source-build path.
	DacpacDir string
}

// Result describes one publish attempt.
type Result struct {
	OK         bool
	DurationMs int64
	Err        error     // nil iff OK
	Code       ErrorCode // stable classification when !OK
	// Created is true when the publish brought a database into
	// existence (it did not exist beforehand). A freshly created DB is
	// clean — schema plus reference data, no test data — so the caller
	// can declare it as the baseline reset reverts to.
	Created bool
}

// Publish runs host `dotnet build` then host `sqlpackage
// /Action:Publish` against Host:Port. Each step streams its native
// stdout+stderr to out as it is produced. The publish is idempotent —
// a second run against an unchanged project converges to a no-op.
func Publish(ctx context.Context, opts Opts, out io.Writer) Result {
	start := time.Now()
	if code, err := validatePublishInput(opts); err != nil {
		return failed(start, err, code)
	}

	// Auto-heal the empty-server case up front: a target that doesn't
	// exist yet needs its referenced shared objects (roles, schemas)
	// created too, which only composite deployment does. Detecting it
	// here keeps the steady-state publish (DB present) on the default
	// path — no composite, no reshaping objects another DB owns.
	created := false
	exists, err := DatabaseExists(ctx, opts)
	if err != nil {
		return failed(start, fmt.Errorf("probe database existence: %w", err), CodeSQLServerUnavailable)
	}
	if !exists {
		opts.IncludeComposite = true
		created = true
	}

	dacpac, fingerprint, code, err := buildDacpac(ctx, opts, out)
	if err != nil {
		return failed(start, err, code)
	}
	code, err = publishWithCompositeRetry(ctx, opts, dacpac, out)
	if err != nil {
		return failed(start, err, code)
	}
	// Remember what was just published so the next diff can short-circuit.
	recordPublishStateWhenAvailable(ctx, opts, fingerprint, out)
	return Result{OK: true, DurationMs: time.Since(start).Milliseconds(), Created: created}
}

func validatePublishInput(opts Opts) (ErrorCode, error) {
	if opts.DacpacDir != "" {
		if err := ValidateDacpacArtifacts(opts.DacpacDir, opts.SQLProj); err != nil {
			return CodeDacpacArtifactMissing, err
		}
		return CodeNone, nil
	}
	if _, err := os.Stat(opts.SQLProj); err != nil {
		return CodeSQLProjectNotFound, fmt.Errorf("sqlproj not found at %s", opts.SQLProj)
	}
	return CodeNone, nil
}

func recordPublishStateWhenAvailable(ctx context.Context, opts Opts, fingerprint string, out io.Writer) {
	if fingerprint != "" {
		recordPublishStateBestEffort(ctx, opts, fingerprint, out)
	}
}

// buildDacpac verifies the toolchain and builds the project — zero
// side effects beyond OutDir, which is what lets PublishClean run it
// BEFORE the destructive revert.
//
// A dacpac cache short-circuits the dotnet build when the project's source
// is unchanged since a prior build: the stored dacpac is copied into OutDir
// and returned. The cache is best-effort — any error falls through to a
// full build (see dacpac_cache.go).
func buildDacpac(ctx context.Context, opts Opts, out io.Writer) (string, string, ErrorCode, error) {
	if _, err := SqlpackagePath(); err != nil {
		return "", "", CodeToolchainMissing, err
	}
	if opts.DacpacDir != "" {
		dacpac, err := restoreSuppliedDacpacs(opts, out)
		if err != nil {
			return "", "", CodeDacpacArtifactMissing, err
		}
		return dacpac, "", CodeNone, nil
	}
	if _, err := DotnetVersion(ctx); err != nil {
		return "", "", CodeToolchainMissing, err
	}
	if _, err := os.Stat(opts.SQLProj); err != nil {
		return "", "", CodeSQLProjectNotFound, fmt.Errorf("sqlproj not found at %s", opts.SQLProj)
	}

	dacpac := builtDacpacPath(opts)
	fingerprint, fpErr := projectFingerprint(opts.SQLProj, opts.DB)
	if fpErr == nil {
		restored, err := restoreCachedDacpac(opts, dacpac, fingerprint)
		if err != nil {
			return "", "", CodeBuildFailed, err
		}
		if restored {
			fmt.Fprintf(out, "[build] reused cached dacpac (project unchanged)\n")
			return dacpac, fingerprint, CodeNone, nil
		}
	}

	if err := buildFreshDacpac(ctx, opts, dacpac, fingerprint, fpErr, out); err != nil {
		return "", "", CodeBuildFailed, err
	}
	return dacpac, fingerprint, CodeNone, nil
}

// ValidateDacpacArtifacts checks the invocation-specific artifact layout
// without copying files or touching SQL Server.
func ValidateDacpacArtifacts(root, sqlProj string) error {
	rootInfo, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("prebuilt dacpac root not found at %s", root)
		}
		return fmt.Errorf("checking prebuilt dacpac root %s: %w", root, err)
	}
	if !rootInfo.IsDir() {
		return fmt.Errorf("prebuilt dacpac root is not a directory: %s", root)
	}
	projectName := strings.TrimSuffix(filepath.Base(sqlProj), filepath.Ext(sqlProj))
	rootEntries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("reading prebuilt dacpac root %s: %w", root, err)
	}
	if exactEntry(rootEntries, projectName) == nil {
		if actual := equalFoldEntry(rootEntries, projectName); actual != nil {
			return fmt.Errorf("prebuilt dacpac directory for project %s has incorrect casing %q in %s", projectName, actual.Name(), root)
		}
		return fmt.Errorf("prebuilt dacpac directory for project %s not found at %s", projectName, filepath.Join(root, projectName))
	}
	projectDir := filepath.Join(root, projectName)
	projectInfo, err := os.Stat(projectDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("prebuilt dacpac directory for project %s not found at %s", projectName, projectDir)
		}
		return fmt.Errorf("checking prebuilt dacpac directory for project %s at %s: %w", projectName, projectDir, err)
	}
	if !projectInfo.IsDir() {
		return fmt.Errorf("prebuilt dacpac path for project %s is not a directory: %s", projectName, projectDir)
	}
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return fmt.Errorf("reading prebuilt dacpacs for project %s: %w", projectName, err)
	}
	leafName := projectName + ".dacpac"
	leafEntry := exactEntry(entries, leafName)
	if leafEntry == nil {
		if actual := equalFoldEntry(entries, leafName); actual != nil {
			return fmt.Errorf("prebuilt dacpac for project %s has incorrect casing %q in %s", projectName, actual.Name(), projectDir)
		}
		var found []string
		for _, entry := range entries {
			if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".dacpac") {
				found = append(found, entry.Name())
			}
		}
		if len(found) == 0 {
			return fmt.Errorf("prebuilt dacpac directory for project %s contains no dacpac files: %s", projectName, projectDir)
		}
		return fmt.Errorf("prebuilt dacpac for project %s not found at %s; directory holds %s", projectName, filepath.Join(projectDir, leafName), strings.Join(found, ", "))
	}
	leaf := filepath.Join(projectDir, leafName)
	info, err := os.Stat(leaf)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("checking prebuilt dacpac for project %s at %s: %w", projectName, leaf, err)
		}
		return fmt.Errorf("prebuilt dacpac for project %s not found at %s", projectName, leaf)
	}
	if info.IsDir() {
		return fmt.Errorf("prebuilt dacpac for project %s is a directory at %s", projectName, leaf)
	}
	return nil
}

func exactEntry(entries []os.DirEntry, name string) os.DirEntry {
	for _, entry := range entries {
		if entry.Name() == name {
			return entry
		}
	}
	return nil
}

func equalFoldEntry(entries []os.DirEntry, name string) os.DirEntry {
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), name) {
			return entry
		}
	}
	return nil
}

func restoreSuppliedDacpacs(opts Opts, out io.Writer) (string, error) {
	if err := ValidateDacpacArtifacts(opts.DacpacDir, opts.SQLProj); err != nil {
		return "", err
	}
	projectName := strings.TrimSuffix(filepath.Base(opts.SQLProj), filepath.Ext(opts.SQLProj))
	projectDir := filepath.Join(opts.DacpacDir, projectName)
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return "", fmt.Errorf("reading prebuilt dacpacs for project %s: %w", projectName, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".dacpac") {
			continue
		}
		src := filepath.Join(projectDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return "", fmt.Errorf("reading prebuilt dacpac metadata for %s: %w", src, err)
		}
		if err := copyFileAtomic(src, filepath.Join(opts.OutDir, entry.Name())); err != nil {
			return "", fmt.Errorf("copying prebuilt dacpac %s: %w", src, err)
		}
		fmt.Fprintf(out, "[artifact] %s (%d bytes, modified %s)\n", entry.Name(), info.Size(), info.ModTime().Format(time.RFC3339))
	}
	return builtDacpacPath(opts), nil
}

// Referenced projects emit dacpacs beside the leaf, so a cache hit restores
// the complete build output before accepting the leaf artifact.
func restoreCachedDacpac(opts Opts, dacpac, fingerprint string) (bool, error) {
	cacheDir, err := cachedBuildDir(fingerprint)
	if err != nil {
		return false, nil
	}
	n, err := restoreDacpacs(cacheDir, opts.OutDir)
	if err != nil || n == 0 {
		return false, nil
	}
	if _, err := os.Stat(dacpac); err != nil {
		return false, nil
	}
	current, err := projectFingerprint(opts.SQLProj, opts.DB)
	if err != nil || current != fingerprint {
		return false, fmt.Errorf("SQL project changed while restoring its build cache; retry")
	}
	return true, nil
}

// builtDacpacPath mirrors Microsoft.Build.Sql 2.x, which names the artifact
// from the .sqlproj filename and ignores both <Name> (microsoft/DacFx#491)
// and <AssemblyName>. A project that redirects its output anyway — via
// <SqlTargetName>, or the third-party MSBuild.Sdk.SqlProj SDK, which does
// honour <AssemblyName> — hits the "not produced at" error below rather
// than publishing the wrong artifact.
func builtDacpacPath(opts Opts) string {
	base := filepath.Base(opts.SQLProj)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	return filepath.Join(opts.OutDir, name+".dacpac")
}

// dacpacNotProduced reports a build that finished without the artifact orbit
// publishes. Listing the directory's contents turns "file missing" into
// something the user can act on, but it stops short of claiming a rename: a
// project with <ProjectReference>s emits its dependencies' dacpacs here too,
// so what is present may be a sibling rather than a redirected output.
func dacpacNotProduced(want, outDir string) error {
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return fmt.Errorf("dacpac not produced at %s", want)
	}
	var found []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".dacpac") {
			found = append(found, e.Name())
		}
	}
	if len(found) == 0 {
		return fmt.Errorf("dacpac not produced at %s", want)
	}
	return fmt.Errorf(
		"dacpac not produced at %s; the build directory holds %s. Orbit publishes the dacpac named after the .sqlproj, so check the build output for errors, or for a <SqlTargetName> override that renamed it",
		want, strings.Join(found, ", "),
	)
}

func buildFreshDacpac(ctx context.Context, opts Opts, dacpac, fingerprint string, fingerprintErr error, out io.Writer) error {
	build := exec.CommandContext(ctx, "dotnet", "build", opts.SQLProj, "-o", opts.OutDir)
	build.Stdout = out
	build.Stderr = out
	if err := build.Run(); err != nil {
		return fmt.Errorf("dotnet build: %w", err)
	}
	if _, err := os.Stat(dacpac); err != nil {
		return dacpacNotProduced(dacpac, opts.OutDir)
	}
	// A build can overlap an editor save or pull. Publishing that artifact, or
	// storing it under the pre-build key, would make the cache claim a source
	// state the dacpac may not represent.
	if fingerprintErr == nil {
		current, err := projectFingerprint(opts.SQLProj, opts.DB)
		if err != nil || current != fingerprint {
			return fmt.Errorf("SQL project changed during build; retry")
		}
		if cacheDir, err := cachedBuildDir(fingerprint); err == nil {
			// Silent by design, unlike the publish-state record which says so
			// when it fails: a store failure costs the next build a cache miss
			// and nothing else, and that build retries the store. Nothing else
			// comes to depend on the entry, so there is no delayed failure to
			// warn about.
			_ = storeDacpacs(opts.OutDir, cacheDir)
		}
	}
	return nil
}

// publishWithCompositeRetry pushes a built dacpac, retrying once with
// composite objects when the target lacks a referenced dacpac's shared
// objects (SQL72033) — e.g. a shared project's role was never deployed,
// or a baseline revert just removed it. Publish and PublishClean share
// it so every schema-advancing path self-heals the same way; a
// steady-state publish against a prepared server never retries.
func publishWithCompositeRetry(ctx context.Context, opts Opts, dacpac string, out io.Writer) (ErrorCode, error) {
	code, err := publishDacpac(ctx, opts, dacpac, out)
	if err != nil && code == CodeReferenceUnresolved && !opts.IncludeComposite {
		fmt.Fprintf(out, "[publish] unresolved references — retrying with composite objects\n")
		opts.IncludeComposite = true
		return publishDacpac(ctx, opts, dacpac, out)
	}
	return code, err
}

// publishDacpac pushes a built dacpac to the target. Output is captured
// alongside streaming: error classification reads the tool output
// because sqlpackage's exit codes don't distinguish causes.
func publishDacpac(ctx context.Context, opts Opts, dacpac string, out io.Writer) (ErrorCode, error) {
	sqlpackage, err := SqlpackagePath()
	if err != nil {
		return CodeToolchainMissing, err
	}
	var capture bytes.Buffer
	tee := io.MultiWriter(out, &capture)

	args := sqlpackageArgs(opts, "Publish", dacpac)
	if opts.Force {
		args = append(args, "/p:BlockOnPossibleDataLoss=false")
	}
	pub := exec.CommandContext(ctx, sqlpackage, args...)
	pub.Stdout = tee
	pub.Stderr = tee
	if err := pub.Run(); err != nil {
		return classifyPublish(capture.String()), fmt.Errorf("sqlpackage publish: %w", err)
	}
	return CodeNone, nil
}

func sqlpackageArgs(opts Opts, action, dacpac string) []string {
	args := []string{
		"/Action:" + action,
		"/SourceFile:" + dacpac,
		fmt.Sprintf("/TargetServerName:%s,%d", opts.Host, opts.Port),
		"/TargetDatabaseName:" + opts.DB,
		"/TargetUser:" + opts.User,
		"/TargetPassword:" + opts.Password,
		"/TargetTrustServerCertificate:True",
		"/p:DropObjectsNotInSource=true",
	}
	if opts.IncludeComposite {
		args = append(args, "/p:IncludeCompositeObjects=true")
	}
	return args
}

// classifyPublish maps sqlpackage output to a stable error code.
//
// This is EXTERNAL-TOOL OUTPUT parsing, not Go error-chain
// classification: the string-matching prohibition (error-handling.md,
// CODE_CONVENTIONS §9) exists because %w chains make errors.Is
// possible — sqlpackage offers no such channel (exit 1 for every
// failure; localized text is the only signal). Matches are
// deliberately loose — misclassification degrades to the generic
// code, never to a wrong specific one the UI would act on.
func classifyPublish(output string) ErrorCode {
	lower := strings.ToLower(output)
	switch {
	case strings.Contains(lower, "blockonpossibledataloss") || strings.Contains(lower, "possible data loss") ||
		(strings.Contains(lower, "sql72015") && strings.Contains(lower, "sql72045")):
		return CodePublishBlockedDataLoss
	case strings.Contains(lower, "could not connect") || strings.Contains(lower, "network-related") ||
		strings.Contains(lower, "login failed") || strings.Contains(lower, "connection was refused"):
		return CodeSQLServerUnavailable
	case strings.Contains(lower, "deadlock") || strings.Contains(lower, "lock request time out") ||
		strings.Contains(lower, "database is in use"):
		return CodeDatabaseBusy
	case strings.Contains(lower, "sql72033") || strings.Contains(lower, "unresolved reference to object"):
		return CodeReferenceUnresolved
	default:
		return CodePublishFailed
	}
}

func failed(start time.Time, err error, code ErrorCode) Result {
	return Result{OK: false, DurationMs: time.Since(start).Milliseconds(), Err: err, Code: code}
}
