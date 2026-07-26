package devdb

// The DB workflow's daemon orchestration — moved verbatim from the core
// settings handler when the feature became extension-owned (spec B6).

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/extension"
)

// errSQLServerNotInConfig classifies the missing-container rejection so
// restartSQLServer can log it outside the writer lock (its message is
// also the error text callers persist).
var errSQLServerNotInConfig = errors.New("sql-server not found in config")

// restartSQLServer stops the sql-server container, reloads config with new env, and starts it again.
func (f *dbFeature) restartSQLServer() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	slog.Info("restarting sql-server with new settings", "component", "settings")

	// Steps 1–3 are one writer-serialized read-modify-write: config.Load's
	// output depends on process env (which we mutate here), and the final
	// Store must splice into the config published at that moment — without
	// the writer lock a concurrent env switch could be rolled back by this
	// splice (lost update), or Load could see half-adjusted env vars.
	err := f.host.UpdateConfig(func(tx extension.ConfigTx) error {
		// 1. Apply settings to env. ApplyToEnv only sets non-empty keys, so a
		// setting the user cleared would leave a stale process env behind —
		// unset those explicitly so config falls back to the env-file default.
		f.host.Settings().ApplyToEnv()
		if f.host.Settings().Get("sql_server_image") == "" {
			_ = os.Unsetenv("SQL_SERVER_IMAGE")
		}
		if f.host.Settings().Get("sql_server_pull_policy") == "" {
			_ = os.Unsetenv("SQL_SERVER_PULL_POLICY")
		}

		// 2. Reload config with updated env. Error paths log AFTER
		// UpdateConfig returns — logging can block on handler I/O and
		// doesn't belong inside the writer lock.
		newCfg, err := tx.Load(f.host.ConfigPath())
		if err != nil {
			return err
		}

		newContainer, ok := newCfg.Containers[SQLServerContainerName]
		if !ok {
			return errSQLServerNotInConfig
		}

		slog.Info("applying new sql-server config", "component", "settings", "image", newContainer.Image, "pull_policy", newContainer.PullPolicy)

		// 3. Publish: splice only the sql-server entry into the current
		// snapshot — deliberately NOT the whole fresh Load, so unrelated env
		// file edits don't ride along outside an explicit env switch.
		tx.Store(tx.Current().WithContainer(SQLServerContainerName, newContainer))
		return nil
	})
	if err != nil {
		if errors.Is(err, errSQLServerNotInConfig) {
			slog.Warn("sql-server not found in config", "component", "settings")
		} else {
			slog.Error("reload config failed", "component", "settings", "err", err)
		}
		return err
	}

	// 4. Stop existing container
	if err := f.host.Containers().Stop(ctx, SQLServerContainerName); err != nil {
		slog.Debug("stop sql-server (may not exist)", "component", "settings", "err", err)
	}

	// 5. Restart via orchestrator — it will create container with new image
	if err := f.host.Restarter().RestartService(ctx, SQLServerContainerName); err != nil {
		slog.Error("restart sql-server", "component", "settings", "err", err)
		return err
	}

	slog.Info("sql-server restart initiated", "component", "settings")
	return nil
}

// settingsPUTHook restarts sql-server when the SQL image setting changes so
// the new image takes effect without a manual `orbit up` cycle. Other
// settings save normally (the hook returns false and the default path runs).
func (f *dbFeature) settingsPUTHook(w http.ResponseWriter, changes []daemon.SettingsChange) bool {
	imageChanged := false
	for _, ch := range changes {
		if ch.Key == "sql_server_image" {
			imageChanged = true
		}
	}
	if !imageChanged {
		return false
	}
	f.drift.markAllStale()
	f.host.Settings().ApplyToEnv()
	go func() {
		if err := f.restartSQLServer(); err != nil {
			slog.Error("restart sql-server", "component", "settings", "err", err)
		}
	}()
	daemon.WriteJSON(w, http.StatusOK, daemon.SettingsResponse{OK: true, Message: "restarting sql-server with the new image"})
	return true
}
