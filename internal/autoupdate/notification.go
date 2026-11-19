package autoupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iml885203/orbit/internal/atomicio"
)

type notificationReceipt struct {
	PendingKey string    `json:"pending_key,omitempty"`
	PendingAt  time.Time `json:"pending_at,omitempty"`
	OutcomeID  string    `json:"outcome_id,omitempty"`
}

type notification struct {
	key     string
	outcome bool
	message string
}

// Notify uses a separate, non-waiting receipt lock: a busy update transaction
// must never make an informational message wait for the registry writer.
// Emission and acknowledgement are best effort across crashes/write failures.
func Notify(launchPath, installed string, now time.Time, out io.Writer) error {
	fields := strings.Fields(installed)
	if len(fields) == 0 {
		return nil
	}
	if _, ordered := releaseVersionParts(fields[0]); !ordered {
		return nil
	}
	state, err := Load(launchPath)
	if err != nil || state.Policy != PolicyAutomatic {
		return err
	}
	if state.announcesSatisfiedTarget(installed) {
		state.settleAt(installed)
	}
	notice := state.notification(installed)
	if notice.message == "" {
		return nil
	}
	dir, err := GlobalDir()
	if err != nil {
		return err
	}
	dir = filepath.Join(dir, "updates", state.InstallationID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create update notification directory: %w", err)
	}
	unlock, err := acquireLock(filepath.Join(dir, "cli-notice.lock"), 0)
	if err != nil {
		return err
	}
	defer unlock()
	path := filepath.Join(dir, "cli-notice.json")
	var receipt notificationReceipt
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read update notification receipt: %w", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &receipt); err != nil {
			return fmt.Errorf("decode update notification receipt: %w", err)
		}
	}
	if notice.outcome {
		if receipt.OutcomeID == notice.key {
			return nil
		}
	} else if receipt.PendingKey == notice.key && now.Sub(receipt.PendingAt) < 24*time.Hour {
		return nil
	}
	if _, err := fmt.Fprintln(out, "\n"+notice.message); err != nil {
		return fmt.Errorf("write update notification: %w", err)
	}
	if notice.outcome {
		receipt.OutcomeID = notice.key
	} else {
		receipt.PendingKey, receipt.PendingAt = notice.key, now
	}
	data, err = json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("encode update notification receipt: %w", err)
	}
	if err := atomicio.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("persist update notification receipt: %w", err)
	}
	return nil
}

func (s State) notification(installed string) notification {
	tx := s.Transaction
	if tx != nil && tx.FinishedAt == nil {
		return notification{}
	}
	if s.RecordsTransactionOutcome() {
		if tx.Phase == "partial" {
			return notification{key: "partial:" + tx.ID, message: "Orbit updated, but some environments were not restored. Run `orbit status` to inspect resources before retrying `orbit update`."}
		}
		return notification{key: "failed:" + tx.ID, message: "Orbit could not complete the update. Run `orbit update` to retry, or `orbit update --rollback` to restore the previous binary."}
	}
	if s.TargetVersion != "" && newerRelease(s.TargetVersion, installed) {
		key := s.Phase + ":" + s.TargetVersion
		if command := UpgradeCommand(s.Owner); s.Phase == "available" && command != "" {
			return notification{key: key, message: fmt.Sprintf("Orbit %s is available. Update with `%s`.", s.TargetVersion, command)}
		}
		if s.Owner == OwnerDirect && s.Phase == "ready" && s.StagedBinary != "" && s.StagedEvidence != nil {
			return notification{key: key, message: fmt.Sprintf("Orbit %s is verified and ready. It will update automatically when all product environments are idle. Update now: `orbit update`.", s.TargetVersion)}
		}
	}
	if s.LastError != "" && s.LastCheckedAt != nil {
		return notification{key: "check-failed:" + s.TargetVersion, message: "Orbit could not check for updates. Your command completed; background checks will retry."}
	}
	if tx != nil && tx.Phase == "succeeded" && tx.Error == "" && tx.TargetVersion != "" && VersionsMatch(installed, tx.TargetVersion) {
		for _, outcome := range tx.RuntimeOutcomes {
			if outcome.Error != "" || outcome.Phase != "restored" {
				return notification{}
			}
		}
		verb := "updated to"
		if tx.Operation == "rollback" {
			verb = "rolled back to"
		}
		return notification{key: tx.ID, outcome: true, message: fmt.Sprintf("Orbit %s %s.", verb, tx.TargetVersion)}
	}
	return notification{}
}
