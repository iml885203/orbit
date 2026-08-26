package autoupdate

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	intoto "github.com/in-toto/attestation/go/v1"
	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestCapturedGitHubReleaseBundleCryptographicallyVerifies(t *testing.T) {
	bundleJSON, err := os.ReadFile("testdata/github_release_bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	var protobuf protobundle.Bundle
	if err := protojson.Unmarshal(bundleJSON, &protobuf); err != nil {
		t.Fatal(err)
	}
	signed, err := bundle.NewBundle(&protobuf)
	if err != nil {
		t.Fatal(err)
	}
	rootJSON, err := os.ReadFile("testdata/github_trusted_root.json")
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := root.NewTrustedRootFromJSON(rootJSON)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := verify.NewVerifier(trusted, verify.WithSignedTimestamps(1))
	if err != nil {
		t.Fatal(err)
	}
	san, _ := verify.NewSANMatcher("", `^https://dotcom\.releases\.github\.com$`)
	issuer, _ := verify.NewIssuerMatcher("", ".*")
	identity, _ := verify.NewCertificateIdentity(san, issuer, certificate.Extensions{})
	digest, _ := hex.DecodeString("c5e17a62e06a1d201570249c61fae531e9244e1b")
	result, err := verifier.Verify(signed, verify.NewPolicy(
		verify.WithArtifactDigest("sha1", digest), verify.WithCertificateIdentity(identity)))
	if err != nil {
		t.Fatal(err)
	}
	if result.Statement.PredicateType != releasePredicateV01 {
		t.Fatalf("predicate=%q", result.Statement.PredicateType)
	}
	wrongDigest := append([]byte(nil), digest...)
	wrongDigest[0] ^= 0xff
	if _, err := verifier.Verify(signed, verify.NewPolicy(verify.WithArtifactDigest("sha1", wrongDigest), verify.WithCertificateIdentity(identity))); err == nil {
		t.Fatal("wrong release digest accepted")
	}
	tamperedProto := proto.Clone(&protobuf).(*protobundle.Bundle)
	tamperedProto.Content.(*protobundle.Bundle_DsseEnvelope).DsseEnvelope.Payload[0] ^= 1
	tampered, err := bundle.NewBundle(tamperedProto)
	if err == nil {
		_, err = verifier.Verify(tampered, verify.NewPolicy(verify.WithArtifactDigest("sha1", digest), verify.WithCertificateIdentity(identity)))
	}
	if err == nil {
		t.Fatal("tampered statement accepted")
	}
	wrongSAN, _ := verify.NewSANMatcher("", `^https://example\.invalid$`)
	wrongIdentity, _ := verify.NewCertificateIdentity(wrongSAN, issuer, certificate.Extensions{})
	if _, err := verifier.Verify(signed, verify.NewPolicy(verify.WithArtifactDigest("sha1", digest), verify.WithCertificateIdentity(wrongIdentity))); err == nil {
		t.Fatal("wrong certificate SAN accepted")
	}
	strictTimestamps, err := verify.NewVerifier(trusted, verify.WithSignedTimestamps(2))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strictTimestamps.Verify(signed, verify.NewPolicy(verify.WithArtifactDigest("sha1", digest), verify.WithCertificateIdentity(identity))); err == nil {
		t.Fatal("insufficient signed timestamps accepted")
	}
	result.Statement.PredicateType = "https://example.invalid/predicate"
	if _, err := verificationRecord(result, "bdehamer/delme", "v6", strings.Repeat("a", 40), strings.Repeat("a", 40), "artifact.zip"); err == nil {
		t.Fatal("wrong predicate accepted")
	}
}

func TestCapturedBundleVerifiesAnnotatedTagThroughProductionReleaseVerification(t *testing.T) {
	bundleJSON, err := os.ReadFile("testdata/github_release_bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	rootJSON, err := os.ReadFile("testdata/github_trusted_root.json")
	if err != nil {
		t.Fatal(err)
	}
	const digest = "c5e17a62e06a1d201570249c61fae531e9244e1b"
	const peeledCommit = "0123456789abcdef0123456789abcdef01234567"
	initiator, copies := "github", 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/bdehamer/delme/git/ref/tags/v6":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + digest + `","type":"tag"}}`))
		case r.URL.Path == "/repos/bdehamer/delme/git/tags/"+digest:
			_, _ = w.Write([]byte(`{"object":{"sha":"` + peeledCommit + `","type":"commit"}}`))
		case strings.HasPrefix(r.URL.Path, "/repos/bdehamer/delme/attestations/"):
			_, _ = w.Write([]byte(`{"attestations":[`))
			for i := 0; i < copies; i++ {
				if i > 0 {
					_, _ = w.Write([]byte(","))
				}
				_, _ = w.Write([]byte(`{"initiator":"` + initiator + `","bundle":`))
				_, _ = w.Write(bundleJSON)
				_, _ = w.Write([]byte(`}`))
			}
			_, _ = w.Write([]byte(`]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	checker := Checker{Client: server.Client(), Channel: Channel{
		ReleaseAPIURL:     server.URL + "/repos/bdehamer/delme/releases/latest",
		ReleaseRepository: "bdehamer/delme",
	}, trustedRootJSON: func(context.Context) ([]byte, error) { return rootJSON, nil }}
	_, err = checker.verifyGitHubRelease(context.Background(), githubRelease{TagName: "v6", Immutable: true})
	if err == nil || !strings.Contains(err.Error(), "required assets") {
		t.Fatalf("captured attestation did not reach subject policy: %v", err)
	}
	initiator = "user"
	if _, err := checker.verifyGitHubRelease(context.Background(), githubRelease{TagName: "v6", Immutable: true}); err == nil || !strings.Contains(err.Error(), "GitHub-initiated") {
		t.Fatalf("wrong initiator accepted: %v", err)
	}
	initiator, copies = "github", 2
	if _, err := checker.verifyGitHubRelease(context.Background(), githubRelease{TagName: "v6", Immutable: true}); err == nil || !strings.Contains(err.Error(), "2 matching") {
		t.Fatalf("duplicate matching bundles accepted: %v", err)
	}
}

func TestRealAnnotatedReleaseBundleProducesVerifiedRecord(t *testing.T) {
	bundleJSON, err := os.ReadFile("testdata/github_annotated_release_bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	rootJSON, err := os.ReadFile("testdata/github_trusted_root.json")
	if err != nil {
		t.Fatal(err)
	}
	const tagObject = "3fc1d21a1a701a587a3837374cb94e9d8aede5e3"
	const peeledCommit = "a38ac3174c591f95049234d25bb326104d3ca820"
	const asset = "goreleaser_Linux_x86_64.tar.gz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/goreleaser/goreleaser/git/ref/tags/v2.18.0":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + tagObject + `","type":"tag"}}`))
		case "/repos/goreleaser/goreleaser/git/tags/" + tagObject:
			_, _ = w.Write([]byte(`{"object":{"sha":"` + peeledCommit + `","type":"commit"}}`))
		case "/repos/goreleaser/goreleaser/attestations/sha1:" + tagObject:
			_, _ = w.Write([]byte(`{"attestations":[{"initiator":"github","bundle":`))
			_, _ = w.Write(bundleJSON)
			_, _ = w.Write([]byte(`}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	checker := Checker{
		Client:           server.Client(),
		Channel:          Channel{ReleaseAPIURL: server.URL + "/repos/goreleaser/goreleaser/releases/latest", ReleaseRepository: "goreleaser/goreleaser"},
		trustedRootJSON:  func(context.Context) ([]byte, error) { return rootJSON, nil },
		releaseAssetName: func() (string, error) { return asset, nil },
	}
	record, err := checker.verifyGitHubRelease(context.Background(), githubRelease{TagName: "v2.18.0", Immutable: true})
	if err != nil {
		t.Fatal(err)
	}
	if record.TargetCommit != peeledCommit || record.AssetName != asset || record.Tag != "v2.18.0" {
		t.Fatalf("record=%+v", record)
	}
}

func TestTrustCacheLockSerializesCheckers(t *testing.T) {
	dir := t.TempDir()
	var active, maximum atomic.Int32
	load := func() ([]byte, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
		}
		time.Sleep(50 * time.Millisecond)
		return []byte("root"), nil
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := withTrustCacheLock(context.Background(), dir, load); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if maximum.Load() != 1 {
		t.Fatalf("maximum concurrent trust refreshes=%d", maximum.Load())
	}
}

func TestTrustedRootCreatesMissingUpdateHomeBeforeLocking(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing", "update-home")
	t.Setenv("ORBIT_UPDATE_HOME", dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = (Checker{}).githubTrustedRootJSON(ctx)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("update home mode=%#o", info.Mode().Perm())
	}
}

func TestResolveReleaseTagPeelsAnnotatedTagAndRejectsNonCommit(t *testing.T) {
	commit := strings.Repeat("a", 40)
	tagObject := strings.Repeat("b", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/iml885203/orbit/git/ref/tags/v1.2.3":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + tagObject + `","type":"tag"}}`))
		case "/repos/iml885203/orbit/git/tags/" + tagObject:
			_, _ = w.Write([]byte(`{"object":{"sha":"` + commit + `","type":"commit"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	digest, algorithm, resolved, err := (Checker{Client: server.Client()}).resolveReleaseTag(
		context.Background(), base, "iml885203/orbit", "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if digest != tagObject || algorithm != "sha1" || resolved != commit {
		t.Fatalf("digest=%q algorithm=%q commit=%q", digest, algorithm, resolved)
	}

	nonCommit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":{"sha":"` + commit + `","type":"tree"}}`))
	}))
	defer nonCommit.Close()
	base, _ = url.Parse(nonCommit.URL)
	if _, _, _, err := (Checker{Client: nonCommit.Client()}).resolveReleaseTag(
		context.Background(), base, "iml885203/orbit", "v1.2.3"); err == nil {
		t.Fatal("non-commit tag target accepted")
	}
}

func TestReleaseAPIOverrideCannotChangeRepositoryIdentity(t *testing.T) {
	if _, err := releaseAPIBase("https://fixture.example/repos/attacker/orbit/releases/latest", "iml885203/orbit"); err == nil {
		t.Fatal("endpoint override changed repository identity")
	}
	base, err := releaseAPIBase("https://fixture.example/repos/iml885203/orbit/releases/latest", "iml885203/orbit")
	if err != nil || base.String() != "https://fixture.example" {
		t.Fatalf("base=%v err=%v", base, err)
	}
}

func TestVerificationRecordAllowsExtraSubjectsAndRejectsDuplicateRequiredSubject(t *testing.T) {
	asset, err := platformAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	commit := strings.Repeat("a", 40)
	statementJSON := `{
		"_type":"https://in-toto.io/Statement/v1",
		"predicateType":"https://in-toto.io/attestation/release/v0.1",
		"subject":[
			{"uri":"pkg:github/iml885203/orbit@v1.2.3","digest":{"sha1":"` + commit + `"}},
			{"name":"` + asset + `","digest":{"sha256":"` + strings.Repeat("b", 64) + `"}},
			{"name":"checksums.txt","digest":{"sha256":"` + strings.Repeat("c", 64) + `"}},
			{"name":"future-installer.pkg","digest":{"sha256":"` + strings.Repeat("d", 64) + `"}}
		],
		"predicate":{"repository":"iml885203/orbit","tag":"v1.2.3"}}
	`
	var statement intoto.Statement
	if err := protojson.Unmarshal([]byte(statementJSON), &statement); err != nil {
		t.Fatal(err)
	}
	record, err := verificationRecord(&verify.VerificationResult{Statement: &statement}, "iml885203/orbit", "v1.2.3", commit, commit, asset)
	if err != nil {
		t.Fatal(err)
	}
	if record.AssetName != asset || record.VerifiedAt.IsZero() {
		t.Fatalf("record=%+v", record)
	}
	statement.Subject = append(statement.Subject, statement.Subject[1])
	if _, err := verificationRecord(&verify.VerificationResult{Statement: &statement}, "iml885203/orbit", "v1.2.3", commit, commit, asset); err == nil {
		t.Fatal("duplicate required subject accepted")
	}
}

func TestVerificationRecordValidateForApplyRejectsIncompleteAndFutureEvidence(t *testing.T) {
	record := VerificationRecord{
		PolicyVersion: "github-release-v1", Repository: "iml885203/orbit", Tag: "v1.2.3",
		TargetCommit: strings.Repeat("a", 40), AssetName: "orbit-linux-amd64",
		AssetSHA256: strings.Repeat("b", 64), ChecksumsSHA256: strings.Repeat("c", 64),
		VerifiedAt: time.Now().UTC().Add(10 * time.Minute),
	}
	if err := record.ValidateForApply("v1.2.3", "iml885203/orbit", "orbit-linux-amd64"); err == nil {
		t.Fatal("future verification evidence accepted")
	}
	record.VerifiedAt = time.Now().UTC()
	record.Repository = ""
	if err := record.ValidateForApply("v1.2.3", "iml885203/orbit", "orbit-linux-amd64"); err == nil {
		t.Fatal("evidence without repository identity accepted")
	}
}
