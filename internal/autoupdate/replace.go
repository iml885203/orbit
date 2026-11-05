package autoupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type VerifiedCandidate struct {
	file           *os.File
	openedInfo     os.FileInfo
	sourcePath     string
	expectedSHA256 string
}

func OpenVerifiedCandidate(path, expectedSHA256 string) (*VerifiedCandidate, error) {
	if len(expectedSHA256) != sha256.Size*2 {
		return nil, fmt.Errorf("staged release evidence has an invalid SHA-256 digest")
	}
	sourcePath := filepath.Clean(path)
	file, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("open verified update: %w", err)
	}
	actual, err := digestOpenFile(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !strings.EqualFold(actual, expectedSHA256) {
		_ = file.Close()
		return nil, fmt.Errorf("staged Orbit digest does not match verified release evidence")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("inspect verified update: staged Orbit is not a regular file")
	}
	return &VerifiedCandidate{file: file, openedInfo: info, sourcePath: sourcePath, expectedSHA256: strings.ToLower(expectedSHA256)}, nil
}

func digestOpenFile(file *os.File) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek staged Orbit: %w", err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash staged Orbit: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind staged Orbit: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (candidate *VerifiedCandidate) Close() error { return candidate.file.Close() }

func ReplaceCandidate(launchPath string, candidate *VerifiedCandidate) (string, error) {
	launchPath = filepath.Clean(launchPath)
	if candidate == nil || candidate.file == nil {
		return "", fmt.Errorf("verified update candidate is unavailable")
	}
	if launchPath == candidate.sourcePath {
		return "", fmt.Errorf("staged update is the active Orbit binary")
	}
	currentInfo, err := os.Stat(candidate.sourcePath)
	if err != nil || !os.SameFile(candidate.openedInfo, currentInfo) {
		return "", fmt.Errorf("staged Orbit path changed after verification")
	}
	targetDir := filepath.Dir(launchPath)
	temp, err := os.CreateTemp(targetDir, ".orbit-update-*")
	if err != nil {
		return "", fmt.Errorf("stage update beside target: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(temp, hash), candidate.file)
	closeTempErr := temp.Close()
	if copyErr != nil {
		return "", fmt.Errorf("copy verified update: %w", copyErr)
	}
	if closeTempErr != nil {
		return "", fmt.Errorf("close replacement: %w", closeTempErr)
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), candidate.expectedSHA256) {
		return "", fmt.Errorf("staged Orbit digest does not match verified release evidence")
	}
	if err := os.Chmod(tempPath, 0o755); err != nil {
		return "", fmt.Errorf("make replacement executable: %w", err)
	}
	backup := launchPath + ".prev"
	backupTemp := backup + ".replacing"
	_ = os.Remove(backupTemp)
	if err := copyFile(launchPath, backupTemp, 0o755); err != nil {
		return "", fmt.Errorf("back up current Orbit: %w", err)
	}
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("rotate previous Orbit backup: %w", err)
	}
	if err := os.Rename(backupTemp, backup); err != nil {
		return "", fmt.Errorf("commit Orbit backup: %w", err)
	}
	displaced := launchPath + ".replacing"
	_ = os.Remove(displaced)
	if err := os.Rename(launchPath, displaced); err != nil {
		return "", fmt.Errorf("move current Orbit for replacement: %w", err)
	}
	if err := os.Rename(tempPath, launchPath); err != nil {
		_ = os.Rename(displaced, launchPath)
		return "", fmt.Errorf("replace Orbit binary: %w", err)
	}
	_ = os.Remove(displaced)
	return backup, nil
}

func Restore(launchPath string) error {
	backup := launchPath + ".prev"
	failed := launchPath + ".prev.failed"
	_ = os.Remove(failed)
	if err := os.Rename(launchPath, failed); err != nil {
		return fmt.Errorf("move failed Orbit aside: %w", err)
	}
	if err := os.Rename(backup, launchPath); err != nil {
		_ = os.Rename(failed, launchPath)
		return fmt.Errorf("restore previous Orbit: %w", err)
	}
	return nil
}

func UndoRestore(launchPath string) error {
	failed := launchPath + ".prev.failed"
	backup := launchPath + ".prev"
	if err := os.Rename(launchPath, backup); err != nil {
		return fmt.Errorf("preserve failed rollback target: %w", err)
	}
	if err := os.Rename(failed, launchPath); err != nil {
		_ = os.Rename(backup, launchPath)
		return fmt.Errorf("restore pre-rollback Orbit: %w", err)
	}
	return nil
}

func copyFile(source, destination string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
