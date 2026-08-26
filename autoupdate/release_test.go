package autoupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewerReleaseRequiresComparableNewerVersion(t *testing.T) {
	for _, test := range []struct {
		candidate string
		current   string
		want      bool
	}{
		{"v0.17.0", "v0.16.0 (build)", true},
		{"v0.16.0", "v0.16.0", false},
		{"v0.15.9", "v0.16.0", false},
		{"main-dirty", "v0.16.0", false},
	} {
		if got := newerRelease(test.candidate, test.current); got != test.want {
			t.Errorf("newerRelease(%q, %q) = %v, want %v", test.candidate, test.current, got, test.want)
		}
	}
}

func TestCheckDueHonorsPolicyAndInterval(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	launch := filepath.Join(t.TempDir(), "orbit")
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	checker := Checker{Now: func() time.Time { return now }}
	due, err := checker.CheckDue(launch)
	if err != nil || !due {
		t.Fatalf("initial due = %v, err = %v", due, err)
	}
	_, err = Update(launch, func(state *State) error {
		next := now.Add(time.Hour)
		state.NextCheckAt = &next
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	due, err = checker.CheckDue(launch)
	if err != nil || due {
		t.Fatalf("cached due = %v, err = %v", due, err)
	}
}

func TestBackgroundCheckClaimIsGloballyDeduplicated(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	launch := filepath.Join(t.TempDir(), "orbit")
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	checker := Checker{Now: func() time.Time { return now }, Channel: Channel{ReleaseAPIURL: "https://example.invalid/latest"}}
	first, err := checker.ClaimBackgroundCheck(launch)
	if err != nil || !first {
		t.Fatalf("first claim=%v err=%v", first, err)
	}
	second, err := checker.ClaimBackgroundCheck(launch)
	if err != nil || second {
		t.Fatalf("second claim=%v err=%v", second, err)
	}
}

func TestManagedCheckNeverDownloadsArtifacts(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = fmt.Fprint(w, `{"tag_name":"v0.17.0","assets":[]}`)
	}))
	defer server.Close()
	launch := filepath.Join(t.TempDir(), "Cellar", "orbit", "0.16.0", "bin", "orbit")
	checker := Checker{Client: server.Client(), Channel: Channel{ReleaseAPIURL: server.URL}}
	state, err := checker.CheckAndStage(context.Background(), launch, "v0.16.0")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || state.Owner != OwnerHomebrew || state.Phase != "available" || state.StagedBinary != "" {
		t.Fatalf("unexpected managed state: requests=%d state=%+v", requests, state)
	}
}

func TestChecksumForRejectsMissingAsset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checksums.txt")
	sum := sha256.Sum256([]byte("other"))
	if err := os.WriteFile(path, []byte(hex.EncodeToString(sum[:])+"  other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	asset, err := platformAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	if _, err := checksumFor(path, asset); err == nil {
		t.Fatal("missing checksum accepted")
	}
}

func TestCheckAndStageVerifiesArtifactAndReportedVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture is a POSIX executable script")
	}
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	assetName, err := platformAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	binary := []byte("#!/bin/sh\necho 'v0.17.0'\n")
	sum := sha256.Sum256(binary)
	checksums := []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), assetName))
	checksumsSum := sha256.Sum256(checksums)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release":
			_, _ = fmt.Fprintf(w, `{"tag_name":"v0.17.0","assets":[{"name":%q,"browser_download_url":%q},{"name":"checksums.txt","browser_download_url":%q}]}`,
				assetName, server.URL+"/binary", server.URL+"/checksums")
		case "/binary":
			_, _ = w.Write(binary)
		case "/checksums":
			_, _ = w.Write(checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	launch := filepath.Join(t.TempDir(), "orbit")
	state, err := (Checker{Client: server.Client(), Channel: Channel{ReleaseAPIURL: server.URL + "/release"},
		verifyRelease: func(context.Context, githubRelease) (*VerificationRecord, error) {
			return &VerificationRecord{PolicyVersion: "github-release-v1", Repository: "test/repo", Tag: "v0.17.0",
				TargetCommit: strings.Repeat("a", 40), AssetName: assetName,
				AssetSHA256: hex.EncodeToString(sum[:]), ChecksumsSHA256: hex.EncodeToString(checksumsSum[:]),
				VerifiedAt: time.Now().UTC()}, nil
		}}).
		CheckAndStage(context.Background(), launch, "v0.16.0")
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != "ready" || state.TargetVersion != "v0.17.0" {
		t.Fatalf("unexpected state: %+v", state)
	}
	if _, err := os.Stat(state.StagedBinary); err != nil {
		t.Fatalf("staged binary unavailable: %v", err)
	}
}

func TestCheckAndStageRejectsEvidenceBeforeArtifactDownload(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	assetName, err := platformAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	artifactRequests := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release":
			_, _ = fmt.Fprintf(w, `{"tag_name":"v0.17.0","assets":[{"name":%q,"browser_download_url":%q},{"name":"checksums.txt","browser_download_url":%q}]}`,
				assetName, server.URL+"/binary", server.URL+"/checksums")
		default:
			artifactRequests++
			http.Error(w, "must not download", http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	launch := filepath.Join(t.TempDir(), "orbit")
	_, err = (Checker{Client: server.Client(), Channel: Channel{ReleaseAPIURL: server.URL + "/release"},
		verifyRelease: func(context.Context, githubRelease) (*VerificationRecord, error) {
			return nil, fmt.Errorf("untrusted release evidence")
		}}).CheckAndStage(context.Background(), launch, "v0.16.0")
	if err == nil {
		t.Fatal("untrusted release evidence was accepted")
	}
	if artifactRequests != 0 {
		t.Fatalf("artifact requests=%d", artifactRequests)
	}
	state, loadErr := Load(launch)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if state.StagedBinary != "" || state.StagedEvidence != nil {
		t.Fatalf("untrusted update persisted: %+v", state)
	}
}

func TestForgedBinaryAndChecksumsCannotOverrideAttestedDigests(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture is a POSIX executable script")
	}
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	assetName, err := platformAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	forged := []byte("#!/bin/sh\necho 'v0.17.0'\n")
	forgedSum := sha256.Sum256(forged)
	checksums := []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(forgedSum[:]), assetName))
	checksumsSum := sha256.Sum256(checksums)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/binary" {
			_, _ = w.Write(forged)
			return
		}
		_, _ = w.Write(checksums)
	}))
	defer server.Close()
	evidence := &VerificationRecord{
		AssetName: assetName, AssetSHA256: strings.Repeat("a", 64),
		ChecksumsSHA256: hex.EncodeToString(checksumsSum[:]),
	}
	if _, err := (Checker{Client: server.Client()}).downloadAndVerify(context.Background(),
		"install", "v0.17.0", assetName, server.URL+"/binary", server.URL+"/checksums", evidence); err == nil {
		t.Fatal("forged matching binary and checksums were accepted")
	}
}

func TestAttestationDoesNotReplaceChecksumOrCandidateVersionChecks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture is a POSIX executable script")
	}
	assetName, err := platformAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	for _, test := range []struct {
		name      string
		binary    []byte
		checksums func(string) []byte
	}{
		{name: "checksum entry", binary: []byte("#!/bin/sh\necho 'v0.17.0'\n"), checksums: func(string) []byte {
			return []byte(strings.Repeat("0", 64) + "  " + assetName + "\n")
		}},
		{name: "candidate version", binary: []byte("#!/bin/sh\necho 'v9.9.9'\n"), checksums: func(digest string) []byte {
			return []byte(digest + "  " + assetName + "\n")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
			binarySum := sha256.Sum256(test.binary)
			checksums := test.checksums(hex.EncodeToString(binarySum[:]))
			checksumsSum := sha256.Sum256(checksums)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/binary" {
					_, _ = w.Write(test.binary)
					return
				}
				_, _ = w.Write(checksums)
			}))
			defer server.Close()
			evidence := &VerificationRecord{AssetName: assetName,
				AssetSHA256: hex.EncodeToString(binarySum[:]), ChecksumsSHA256: hex.EncodeToString(checksumsSum[:])}
			if _, err := (Checker{Client: server.Client()}).downloadAndVerify(context.Background(),
				"install", "v0.17.0", assetName, server.URL+"/binary", server.URL+"/checksums", evidence); err == nil {
				t.Fatal("candidate skipped an independent integrity check")
			}
		})
	}
}

func TestCustomDirectChannelWithoutTrustPolicyCannotStage(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	assetName, err := platformAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	var server *httptest.Server
	artifactRequests := 0
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/release" {
			artifactRequests++
			http.Error(w, "unexpected artifact request", http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprintf(w, `{"tag_name":"v0.17.0","immutable":true,"assets":[{"name":%q,"browser_download_url":%q},{"name":"checksums.txt","browser_download_url":%q}]}`,
			assetName, server.URL+"/binary", server.URL+"/checksums")
	}))
	defer server.Close()
	_, err = (Checker{Client: server.Client(), Channel: Channel{ReleaseAPIURL: server.URL + "/release"}}).
		CheckAndStage(context.Background(), filepath.Join(t.TempDir(), "orbit"), "v0.16.0")
	if err == nil {
		t.Fatal("custom direct channel without trust policy staged an update")
	}
	if artifactRequests != 0 {
		t.Fatalf("artifact requests=%d", artifactRequests)
	}
}

func TestCheckFailurePreservesVerifiedStagedUpdate(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_HOME", t.TempDir())
	launch := filepath.Join(t.TempDir(), "orbit")
	evidence := &VerificationRecord{
		PolicyVersion: githubReleasePolicyVersion, Repository: "iml885203/orbit", Tag: "v0.17.0",
		TargetCommit: strings.Repeat("a", 40), AssetName: "orbit-linux-amd64",
		AssetSHA256: strings.Repeat("b", 64), ChecksumsSHA256: strings.Repeat("c", 64),
		VerifiedAt: time.Now().UTC(),
	}
	_, err := Update(launch, func(state *State) error {
		state.Phase = "ready"
		state.TargetVersion = "v0.17.0"
		state.StagedBinary = filepath.Join(t.TempDir(), "orbit")
		state.StagedEvidence = evidence
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := (Checker{}).recordCheckFailure(launch, fmt.Errorf("offline"))
	if err == nil || state.Phase != "ready" || state.StagedBinary == "" || state.StagedEvidence == nil || *state.StagedEvidence != *evidence {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestRemoveSupersededStagesKeepsOnlyActiveCandidate(t *testing.T) {
	root := t.TempDir()
	oldStage := filepath.Join(root, "0.16.0")
	newStage := filepath.Join(root, "0.17.0")
	for _, dir := range []string{oldStage, newStage} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	removeSupersededStages(root, newStage)
	if _, err := os.Stat(oldStage); !os.IsNotExist(err) {
		t.Fatalf("superseded stage still exists: %v", err)
	}
	if _, err := os.Stat(newStage); err != nil {
		t.Fatalf("active stage removed: %v", err)
	}
}
