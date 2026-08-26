package autoupdate

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/gofrs/flock"
	intoto "github.com/in-toto/attestation/go/v1"
	"github.com/klauspost/compress/snappy"
	"github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/theupdateframework/go-tuf/v2/metadata/fetcher"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	githubReleasePolicyVersion = "github-release-v1"
	releasePredicateV01        = "https://in-toto.io/attestation/release/v0.1"
	releasePredicateV02        = "https://in-toto.io/attestation/release/v0.2"
	githubTUFMirror            = "https://tuf-repo.github.com"
	maxEvidenceBody            = 4 << 20
	maxBundleBody              = 16 << 20
)

//go:embed trust/github-root.json
var githubTUFRoot []byte

type githubRef struct {
	Object struct {
		SHA  string `json:"sha"`
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"object"`
}

type githubTag struct {
	Object struct {
		SHA  string `json:"sha"`
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"object"`
}

type githubAttestations struct {
	Attestations []struct {
		BundleURL string          `json:"bundle_url"`
		Bundle    json.RawMessage `json:"bundle"`
		Initiator string          `json:"initiator"`
	} `json:"attestations"`
}

func (c Checker) verifyGitHubRelease(ctx context.Context, release githubRelease) (*VerificationRecord, error) {
	repository := strings.TrimSpace(c.Channel.ReleaseRepository)
	if repository == "" {
		return nil, fmt.Errorf("automatic update channel has no release trust policy")
	}
	if !release.Immutable {
		return nil, fmt.Errorf("release %s is not immutable", release.TagName)
	}
	apiBase, err := releaseAPIBase(c.Channel.ReleaseAPIURL, repository)
	if err != nil {
		return nil, err
	}
	tagDigest, algorithm, commit, err := c.resolveReleaseTag(ctx, apiBase, repository, release.TagName)
	if err != nil {
		return nil, err
	}
	bundles, err := c.fetchReleaseBundles(ctx, apiBase, repository, algorithm+":"+tagDigest)
	if err != nil {
		return nil, err
	}
	matching := bundles[:0]
	for _, candidate := range bundles {
		candidateTag, parseErr := releaseBundleTag(candidate)
		if parseErr != nil {
			return nil, parseErr
		}
		if candidateTag == release.TagName {
			matching = append(matching, candidate)
		}
	}
	if len(matching) != 1 {
		return nil, fmt.Errorf("release %s has %d matching immutable-release attestations", release.TagName, len(matching))
	}
	loadRoot := c.trustedRootJSON
	if loadRoot == nil {
		loadRoot = c.githubTrustedRootJSON
	}
	rootJSON, err := loadRoot(ctx)
	if err != nil {
		return nil, err
	}
	trustedRoot, err := root.NewTrustedRootFromJSON(rootJSON)
	if err != nil {
		return nil, fmt.Errorf("parse GitHub release trust root: %w", err)
	}
	verifier, err := verify.NewVerifier(trustedRoot, verify.WithSignedTimestamps(1))
	if err != nil {
		return nil, fmt.Errorf("initialize GitHub release verifier: %w", err)
	}
	digest, err := hex.DecodeString(tagDigest)
	if err != nil {
		return nil, fmt.Errorf("decode release tag digest: %w", err)
	}
	san, err := verify.NewSANMatcher("", `^https://dotcom\.releases\.github\.com$`)
	if err != nil {
		return nil, fmt.Errorf("configure release certificate SAN: %w", err)
	}
	issuer, err := verify.NewIssuerMatcher("", ".*")
	if err != nil {
		return nil, fmt.Errorf("configure release certificate issuer: %w", err)
	}
	identity, err := verify.NewCertificateIdentity(san, issuer, certificate.Extensions{})
	if err != nil {
		return nil, fmt.Errorf("configure release certificate identity: %w", err)
	}
	policy := verify.NewPolicy(verify.WithArtifactDigest(algorithm, digest), verify.WithCertificateIdentity(identity))
	result, err := verifier.Verify(matching[0], policy)
	if err != nil {
		return nil, fmt.Errorf("verify immutable-release attestation: %w", err)
	}
	assetName := c.releaseAssetName
	if assetName == nil {
		assetName = func() (string, error) { return platformAsset(runtime.GOOS, runtime.GOARCH) }
	}
	expectedAsset, err := assetName()
	if err != nil {
		return nil, err
	}
	record, err := verificationRecord(result, repository, release.TagName, tagDigest, commit, expectedAsset)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func releaseBundleTag(candidate *bundle.Bundle) (string, error) {
	envelope := candidate.GetDsseEnvelope()
	if envelope == nil {
		return "", fmt.Errorf("release attestation has no DSSE statement")
	}
	var statement intoto.Statement
	if err := protojson.Unmarshal(envelope.Payload, &statement); err != nil {
		return "", fmt.Errorf("parse release attestation statement: %w", err)
	}
	return statement.Predicate.GetFields()["tag"].GetStringValue(), nil
}

func releaseAPIBase(endpoint, repository string) (*url.URL, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid release API URL")
	}
	wantSuffix := "/repos/" + repository + "/releases/latest"
	if !strings.HasSuffix(strings.TrimSuffix(parsed.Path, "/"), wantSuffix) {
		return nil, fmt.Errorf("release API URL does not match configured repository %s", repository)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, wantSuffix)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}

func (c Checker) resolveReleaseTag(ctx context.Context, apiBase *url.URL, repository, tag string) (string, string, string, error) {
	var ref githubRef
	if err := c.getJSON(ctx, apiURL(apiBase, "repos", repository, "git", "ref", "tags/"+tag), maxEvidenceBody, &ref); err != nil {
		return "", "", "", fmt.Errorf("resolve release tag: %w", err)
	}
	algorithm := ""
	switch len(ref.Object.SHA) {
	case 40:
		algorithm = "sha1"
	case 64:
		algorithm = "sha256"
	default:
		return "", "", "", fmt.Errorf("release tag has an unsupported object digest")
	}
	object := ref.Object
	for depth := 0; object.Type == "tag" && depth < 8; depth++ {
		var nested githubTag
		if err := c.getJSON(ctx, apiURL(apiBase, "repos", repository, "git", "tags", object.SHA), maxEvidenceBody, &nested); err != nil {
			return "", "", "", fmt.Errorf("peel release tag: %w", err)
		}
		object = nested.Object
	}
	if object.Type != "commit" || !regexp.MustCompile(`^[0-9a-fA-F]{40}([0-9a-fA-F]{24})?$`).MatchString(object.SHA) {
		return "", "", "", fmt.Errorf("release tag does not resolve to a commit")
	}
	return ref.Object.SHA, algorithm, strings.ToLower(object.SHA), nil
}

func (c Checker) fetchReleaseBundles(ctx context.Context, apiBase *url.URL, repository, digest string) ([]*bundle.Bundle, error) {
	u := apiURL(apiBase, "repos", repository, "attestations", digest)
	query := u.Query()
	query.Set("predicate_type", "release")
	query.Set("per_page", "100")
	u.RawQuery = query.Encode()
	var response githubAttestations
	if err := c.getJSON(ctx, u, maxEvidenceBody, &response); err != nil {
		return nil, fmt.Errorf("fetch release attestations: %w", err)
	}
	if len(response.Attestations) == 0 || len(response.Attestations) > 100 {
		return nil, fmt.Errorf("release attestation inventory is invalid")
	}
	out := make([]*bundle.Bundle, 0, len(response.Attestations))
	for _, attestation := range response.Attestations {
		if attestation.Initiator != "github" {
			continue
		}
		parsed, err := c.fetchBundle(ctx, attestation.BundleURL, attestation.Bundle)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("release has no GitHub-initiated attestation")
	}
	return out, nil
}

func (c Checker) fetchBundle(ctx context.Context, bundleURL string, inline json.RawMessage) (*bundle.Bundle, error) {
	var encoded []byte
	if bundleURL != "" {
		parsed, err := url.Parse(bundleURL)
		if err != nil || parsed.Scheme != "https" || !strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".blob.core.windows.net") {
			return nil, fmt.Errorf("release attestation bundle URL is not trusted")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("build release attestation request: %w", err)
		}
		client := *c.httpClient()
		client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
			if req.URL.Scheme != "https" || !strings.HasSuffix(strings.ToLower(req.URL.Hostname()), ".blob.core.windows.net") {
				return fmt.Errorf("release attestation redirect is not trusted")
			}
			return nil
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("download release attestation: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("download release attestation: server returned %s", resp.Status)
		}
		encoded, err = io.ReadAll(io.LimitReader(resp.Body, maxEvidenceBody+1))
		if err != nil {
			return nil, fmt.Errorf("read release attestation bundle: %w", err)
		}
		if len(encoded) > maxEvidenceBody {
			return nil, fmt.Errorf("release attestation bundle exceeds the size limit")
		}
		decodedLen, err := snappy.DecodedLen(encoded)
		if err != nil || decodedLen > maxBundleBody {
			return nil, fmt.Errorf("release attestation bundle has invalid compressed content")
		}
		encoded, err = snappy.Decode(nil, encoded)
		if err != nil {
			return nil, fmt.Errorf("decompress release attestation bundle: %w", err)
		}
	} else {
		encoded = inline
	}
	var protobuf v1.Bundle
	if err := protojson.Unmarshal(encoded, &protobuf); err != nil {
		return nil, fmt.Errorf("parse release attestation bundle: %w", err)
	}
	parsed, err := bundle.NewBundle(&protobuf)
	if err != nil {
		return nil, fmt.Errorf("validate release attestation bundle: %w", err)
	}
	return parsed, nil
}

func (c Checker) githubTrustedRootJSON(ctx context.Context) ([]byte, error) {
	dir, err := GlobalDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create update trust directory: %w", err)
	}
	return withTrustCacheLock(ctx, dir, func() ([]byte, error) {
		opts := tuf.DefaultOptions()
		opts.Root = githubTUFRoot
		opts.RepositoryBaseURL = githubTUFMirror
		opts.CachePath = filepath.Join(dir, "trust")
		opts.CacheValidity = 1
		f := fetcher.NewDefaultFetcher()
		f.SetHTTPClient(c.httpClient())
		opts.WithFetcher(f)
		client, err := tuf.New(opts)
		if err != nil {
			return nil, fmt.Errorf("refresh GitHub release trust root: %w", err)
		}
		contents, err := client.GetTarget("trusted_root.json")
		if err != nil {
			return nil, fmt.Errorf("load GitHub release trust root: %w", err)
		}
		return contents, nil
	})
}

func withTrustCacheLock(ctx context.Context, dir string, load func() ([]byte, error)) ([]byte, error) {
	lock := flock.New(filepath.Join(dir, "trust.lock"))
	locked, err := lock.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil || !locked {
		if err == nil {
			err = ctx.Err()
		}
		return nil, fmt.Errorf("lock GitHub release trust cache: %w", err)
	}
	defer func() { _ = lock.Unlock() }()
	return load()
}

func verificationRecord(result *verify.VerificationResult, repository, tag, tagObjectDigest, commit, assetName string) (*VerificationRecord, error) {
	if result.Statement == nil || (result.Statement.PredicateType != releasePredicateV01 && result.Statement.PredicateType != releasePredicateV02) {
		return nil, fmt.Errorf("attestation has the wrong release predicate")
	}
	predicate := result.Statement.Predicate.GetFields()
	if predicate["repository"].GetStringValue() != repository || predicate["tag"].GetStringValue() != tag {
		return nil, fmt.Errorf("attestation identifies another release")
	}
	required := map[string]string{assetName: "", "checksums.txt": ""}
	commitBound := false
	for _, subject := range result.Statement.Subject {
		if subject.Name == "" && subject.Uri != "" {
			if strings.EqualFold(subject.Digest[gitDigestAlgorithm(tagObjectDigest)], tagObjectDigest) {
				commitBound = true
			}
			continue
		}
		if _, ok := required[subject.Name]; !ok {
			continue
		}
		digest := strings.ToLower(subject.Digest["sha256"])
		if len(digest) != 64 || required[subject.Name] != "" {
			return nil, fmt.Errorf("attestation has duplicate or invalid subject %s", subject.Name)
		}
		required[subject.Name] = digest
	}
	if !commitBound || required[assetName] == "" || required["checksums.txt"] == "" {
		return nil, fmt.Errorf("attestation does not bind the release commit and required assets")
	}
	return &VerificationRecord{
		PolicyVersion: githubReleasePolicyVersion, Repository: repository, Tag: tag,
		TargetCommit: commit, AssetName: assetName, AssetSHA256: required[assetName],
		ChecksumsSHA256: required["checksums.txt"], VerifiedAt: time.Now().UTC(),
	}, nil
}

func (record VerificationRecord) ValidateForApply(tag, repository, assetName string) error {
	if record.PolicyVersion != githubReleasePolicyVersion || record.Repository != repository ||
		record.Tag != tag || record.AssetName != assetName || !validObjectDigest(record.TargetCommit) ||
		!validSHA256(record.AssetSHA256) || !validSHA256(record.ChecksumsSHA256) || record.VerifiedAt.IsZero() {
		return fmt.Errorf("staged update has invalid release verification evidence")
	}
	if record.VerifiedAt.After(time.Now().UTC().Add(time.Minute)) {
		return fmt.Errorf("staged update verification evidence is from the future")
	}
	return nil
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validObjectDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && (len(decoded) == 20 || len(decoded) == sha256.Size)
}

func gitDigestAlgorithm(commit string) string {
	if len(commit) == 64 {
		return "sha256"
	}
	return "sha1"
}

func apiURL(base *url.URL, parts ...string) *url.URL {
	copy := *base
	copy.Path = path.Join(append([]string{copy.Path}, parts...)...)
	return &copy
}

func (c Checker) getJSON(ctx context.Context, target *url.URL, limit int64, destination any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %s", resp.Status)
	}
	contents, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(contents)) > limit {
		return fmt.Errorf("response exceeds the size limit")
	}
	if err := json.Unmarshal(contents, destination); err != nil {
		return err
	}
	return nil
}

func (c Checker) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 2 * time.Minute}
}
