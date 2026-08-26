package autoupdate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func Replace(launchPath, stagedPath string) (string, error) {
	launchPath = filepath.Clean(launchPath)
	stagedPath = filepath.Clean(stagedPath)
	if launchPath == stagedPath {
		return "", fmt.Errorf("staged update is the active Orbit binary")
	}
	targetDir := filepath.Dir(launchPath)
	temp, err := os.CreateTemp(targetDir, ".orbit-update-*")
	if err != nil {
		return "", fmt.Errorf("stage update beside target: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	source, err := os.Open(stagedPath)
	if err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("open verified update: %w", err)
	}
	_, copyErr := io.Copy(temp, source)
	closeSourceErr := source.Close()
	closeTempErr := temp.Close()
	if copyErr != nil {
		return "", fmt.Errorf("copy verified update: %w", copyErr)
	}
	if closeSourceErr != nil {
		return "", fmt.Errorf("close verified update: %w", closeSourceErr)
	}
	if closeTempErr != nil {
		return "", fmt.Errorf("close replacement: %w", closeTempErr)
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
