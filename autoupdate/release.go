package autoupdate

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const CheckInterval = 24 * time.Hour

type Channel struct {
	ReleaseAPIURL     string
	ReleaseRepository string
}

type githubRelease struct {
	TagName   string `json:"tag_name"`
	Immutable bool   `json:"immutable"`
	Assets    []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

type Checker struct {
	Client          *http.Client
	Channel         Channel
	Now             func() time.Time
	Explicit        bool
	verifyRelease   func(context.Context, githubRelease) (*VerificationRecord, error)
	trustedRootJSON func(context.Context) ([]byte, error)
}

func (c Checker) CheckAndStage(ctx context.Context, launchPath, currentVersion string) (State, error) {
	if strings.TrimSpace(c.Channel.ReleaseAPIURL) == "" {
		return State{}, errors.New("this Orbit build has no automatic update channel")
	}
	state, err := Load(launchPath)
	if err != nil {
		return State{}, err
	}
	if state.Policy == PolicyOff && !c.Explicit {
		return state, nil
	}
	if state.Owner != OwnerDirect {
		return c.checkManaged(ctx, state, currentVersion)
	}
	release, err := c.fetchRelease(ctx)
	if err != nil {
		return c.recordCheckFailure(launchPath, err)
	}
	if !newerRelease(release.TagName, currentVersion) {
		return c.recordCurrent(launchPath, currentVersion)
	}
	assetName, err := platformAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return c.recordCheckFailure(launchPath, err)
	}
	assetURL, checksumURL := releaseAssets(release, assetName)
	if assetURL == "" || checksumURL == "" {
		return c.recordCheckFailure(launchPath, fmt.Errorf("release %s is missing %s or checksums.txt", release.TagName, assetName))
	}
	verify := c.verifyRelease
	if verify == nil {
		verify = c.verifyGitHubRelease
	}
	evidence, err := verify(ctx, release)
	if err != nil {
		return c.recordCheckFailure(launchPath, err)
	}
	if evidence.AssetName != assetName {
		return c.recordCheckFailure(launchPath, fmt.Errorf("verified release evidence is missing %s", assetName))
	}
	staged, err := c.downloadAndVerify(ctx, state.InstallationID, release.TagName, assetName, assetURL, checksumURL, evidence)
	if err != nil {
		return c.recordCheckFailure(launchPath, err)
	}
	return Update(launchPath, func(next *State) error {
		now := c.now()
		next.CurrentVersion = currentVersion
		next.TargetVersion = release.TagName
		next.Phase = "ready"
		next.ApplyEligible = false
		next.DeferReason = "eligibility_pending"
		next.StagedBinary = staged
		next.StagedEvidence = evidence
		next.LastCheckedAt = &now
		following := now.Add(CheckInterval + checkJitter(next.InstallationID))
		next.NextCheckAt = &following
		next.LastError = ""
		next.CheckFailures = 0
		return nil
	})
}

func (c Checker) CheckDue(launchPath string) (bool, error) {
	state, err := Load(launchPath)
	if err != nil {
		return false, err
	}
	if state.Policy == PolicyOff {
		return false, nil
	}
	return state.NextCheckAt == nil || !c.now().Before(*state.NextCheckAt), nil
}

func (c Checker) ClaimBackgroundCheck(launchPath string) (bool, error) {
	claimed := false
	_, err := Update(launchPath, func(state *State) error {
		if state.Policy == PolicyOff || strings.TrimSpace(c.Channel.ReleaseAPIURL) == "" {
			return nil
		}
		now := c.now()
		if state.NextCheckAt != nil && now.Before(*state.NextCheckAt) {
			return nil
		}
		claimed = true
		state.Phase = "checking"
		lease := now.Add(5 * time.Minute)
		state.NextCheckAt = &lease
		return nil
	})
	return claimed, err
}

func (c Checker) fetchRelease(ctx context.Context) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Channel.ReleaseAPIURL, nil)
	if err != nil {
		return githubRelease{}, fmt.Errorf("build update request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return githubRelease{}, fmt.Errorf("check release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("check release: server returned %s", resp.Status)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("decode release metadata: %w", err)
	}
	return release, nil
}

func (c Checker) checkManaged(ctx context.Context, state State, currentVersion string) (State, error) {
	release, err := c.fetchRelease(ctx)
	if err != nil {
		return c.recordCheckFailure(state.LaunchPath, err)
	}
	return Update(state.LaunchPath, func(next *State) error {
		now := c.now()
		next.Owner = state.Owner
		next.CurrentVersion = currentVersion
		next.LastCheckedAt = &now
		following := now.Add(CheckInterval + checkJitter(next.InstallationID))
		next.NextCheckAt = &following
		next.LastError = ""
		next.CheckFailures = 0
		if newerRelease(release.TagName, currentVersion) {
			next.TargetVersion = release.TagName
			next.Phase = "available"
		} else {
			next.TargetVersion = ""
			next.Phase = "current"
		}
		return nil
	})
}

func (c Checker) recordCurrent(launchPath, currentVersion string) (State, error) {
	return Update(launchPath, func(state *State) error {
		now := c.now()
		state.CurrentVersion = currentVersion
		state.TargetVersion = ""
		state.StagedBinary = ""
		state.StagedEvidence = nil
		state.Phase = "current"
		state.ApplyEligible = false
		state.DeferReason = ""
		state.LastCheckedAt = &now
		next := now.Add(CheckInterval + checkJitter(state.InstallationID))
		state.NextCheckAt = &next
		state.LastError = ""
		state.CheckFailures = 0
		return nil
	})
}

func (c Checker) recordCheckFailure(launchPath string, checkErr error) (State, error) {
	state, err := Update(launchPath, func(state *State) error {
		now := c.now()
		if state.StagedBinary == "" {
			state.Phase = "failed"
		}
		state.LastCheckedAt = &now
		state.CheckFailures++
		exponent := state.CheckFailures - 1
		if exponent > 4 {
			exponent = 4
		}
		delay := 15*time.Minute*time.Duration(1<<exponent) + checkJitter(state.InstallationID)
		next := now.Add(delay)
		state.NextCheckAt = &next
		state.LastError = checkErr.Error()
		return nil
	})
	if err != nil {
		return State{}, err
	}
	return state, checkErr
}

func checkJitter(installationID string) time.Duration {
	sum := sha256.Sum256([]byte(installationID))
	return time.Duration(int(sum[0])%61) * time.Minute
}

func (c Checker) downloadAndVerify(ctx context.Context, installationID, targetVersion, assetName, assetURL, checksumURL string, evidence *VerificationRecord) (string, error) {
	dir, err := GlobalDir()
	if err != nil {
		return "", err
	}
	stageDir := filepath.Join(dir, "updates", installationID, strings.TrimPrefix(targetVersion, "v"))
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		return "", fmt.Errorf("create update staging directory: %w", err)
	}
	tempDir, err := os.MkdirTemp(stageDir, ".download-")
	if err != nil {
		return "", fmt.Errorf("create staged download: %w", err)
	}
	defer os.RemoveAll(tempDir)
	candidate := filepath.Join(tempDir, assetName)
	checksums := filepath.Join(tempDir, "checksums.txt")
	if err := c.download(ctx, assetURL, candidate); err != nil {
		return "", err
	}
	if err := c.download(ctx, checksumURL, checksums); err != nil {
		return "", err
	}
	checksumsDigest, err := fileSHA256(checksums)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(checksumsDigest, evidence.ChecksumsSHA256) {
		return "", fmt.Errorf("checksums.txt does not match verified release evidence")
	}
	expected, err := checksumFor(checksums, assetName)
	if err != nil {
		return "", err
	}
	actual, err := fileSHA256(candidate)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(expected, actual) {
		return "", fmt.Errorf("checksum mismatch for %s", assetName)
	}
	if !strings.EqualFold(actual, evidence.AssetSHA256) {
		return "", fmt.Errorf("%s does not match verified release evidence", assetName)
	}
	if err := os.Chmod(candidate, 0o700); err != nil {
		return "", fmt.Errorf("make staged Orbit executable: %w", err)
	}
	version, err := probeBinaryVersion(ctx, candidate)
	if err != nil {
		return "", err
	}
	if normalizeVersion(version) != normalizeVersion(targetVersion) {
		return "", fmt.Errorf("staged binary reports %s, expected %s", version, targetVersion)
	}
	finalPath := filepath.Join(stageDir, "orbit")
	if runtime.GOOS == "windows" {
		finalPath += ".exe"
	}
	if err := os.Rename(candidate, finalPath); err != nil {
		return "", fmt.Errorf("commit staged update: %w", err)
	}
	removeSupersededStages(filepath.Dir(stageDir), stageDir)
	return finalPath, nil
}

func removeSupersededStages(root, keep string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if filepath.Clean(path) != filepath.Clean(keep) {
			_ = os.RemoveAll(path)
		}
	}
}

// RemoveStagedBinary confines cleanup to Orbit-owned staging directories so a
// malformed registry path cannot remove an arbitrary parent directory.
func RemoveStagedBinary(path string) {
	dir, err := GlobalDir()
	if err != nil {
		return
	}
	root := filepath.Join(dir, "updates")
	stageDir := filepath.Dir(filepath.Clean(path))
	relative, err := filepath.Rel(root, stageDir)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return
	}
	_ = os.RemoveAll(stageDir)
}

func (c Checker) download(ctx context.Context, url, destination string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build artifact request: %w", err)
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download update artifact: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download update artifact: server returned %s", resp.Status)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create update artifact: %w", err)
	}
	_, copyErr := io.Copy(file, io.LimitReader(resp.Body, 256<<20))
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("write update artifact: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close update artifact: %w", closeErr)
	}
	return nil
}

func (c Checker) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func releaseAssets(release githubRelease, binaryName string) (string, string) {
	var binary, checksums string
	for _, asset := range release.Assets {
		switch asset.Name {
		case binaryName:
			binary = asset.BrowserDownloadURL
		case "checksums.txt":
			checksums = asset.BrowserDownloadURL
		}
	}
	return binary, checksums
}

func platformAsset(goos, goarch string) (string, error) {
	arch := map[string]string{"amd64": "amd64", "arm64": "arm64"}[goarch]
	if arch == "" {
		return "", fmt.Errorf("automatic updates do not support architecture %s", goarch)
	}
	if goos != "darwin" && goos != "linux" && goos != "windows" {
		return "", fmt.Errorf("automatic updates do not support %s", goos)
	}
	name := fmt.Sprintf("orbit-%s-%s", goos, arch)
	if goos == "windows" {
		name += ".exe"
	}
	return name, nil
}

func PlatformAssetName() (string, error) {
	return platformAsset(runtime.GOOS, runtime.GOARCH)
}

func checksumFor(path, assetName string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open checksums: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == assetName {
			if len(fields[0]) != sha256.Size*2 {
				break
			}
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read checksums: %w", err)
	}
	return "", fmt.Errorf("checksum missing for %s", assetName)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open staged binary: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash staged binary: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func probeBinaryVersion(ctx context.Context, path string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, path, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("verify staged binary version: %w", err)
	}
	return strings.Fields(string(out))[0], nil
}

var releaseVersionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

func newerRelease(candidate, current string) bool {
	a, okA := releaseVersionParts(candidate)
	b, okB := releaseVersionParts(strings.Fields(current)[0])
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func releaseVersionParts(value string) ([3]int, bool) {
	match := releaseVersionPattern.FindStringSubmatch(value)
	if match == nil {
		return [3]int{}, false
	}
	var parts [3]int
	for i := range parts {
		parsed, err := strconv.Atoi(match[i+1])
		if err != nil {
			return [3]int{}, false
		}
		parts[i] = parsed
	}
	return parts, true
}

func normalizeVersion(value string) string {
	return strings.TrimPrefix(strings.Fields(value)[0], "v")
}

// VersionsMatch keeps post-replacement verification on the same normalization
// rule used when a downloaded candidate is first staged.
func VersionsMatch(actual, expected string) bool {
	return normalizeVersion(actual) == normalizeVersion(expected)
}
