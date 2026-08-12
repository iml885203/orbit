// Maps common database operation failure output to one actionable hint.
// sqlpackage/docker errors are verbose and land in a log modal most users
// never open — the classifier pulls the "what do I do now" out of them.
// Patterns are checked from the end of the output (failures conclude a run).

// Data-loss blocks: publish's wording has one owner (the error-code
// map below) — the regex is named so the override in dbOpHint can't
// silently rebind if HINTS is reordered.
const DATA_LOSS_RE = /BlockOnPossibleDataLoss|might result in data loss|rows were detected/i

const HINTS: Array<{ re: RegExp; hint: string }> = [
  {
    re: DATA_LOSS_RE,
    hint: 'blocked to avoid data loss — inspect the change, then use `orbit sqlserver publish <db> --force` if it is intentional; use `orbit sqlserver reset <db>` to discard all local data',
  },
  {
    re: /Login failed|Cannot open database|error connecting|connection was refused|target platform|network-related/i,
    hint: 'configured SQL Server target is unreachable — check `orbit status` and restart that target if degraded',
  },
  {
    re: /deadlocked|is in use|exclusive access|being used by another/i,
    hint: 'database is busy — close DbGate / other connections and retry',
  },
  {
    re: /dotnet.*(not found|no such file)|MSB\d{4}|error CS\d{4}/i,
    hint: 'the dbproject build failed — fix the build errors above and retry',
  },
]

const ERROR_CODE_HINTS: Record<string, string> = {
  toolchain_missing: 'sqlpackage is missing — install it with `dotnet tool install -g microsoft.sqlpackage`, then retry',
  sql_project_not_found: 'no SQL project was found for this database — add its .sqlproj path under `sqlserver.projects`',
  dacpac_artifact_missing: 'the supplied dacpac artifacts are incomplete — check the artifact root, project directory, and expected leaf named in the log',
  build_failed: 'the SQL project build failed — fix the build errors in the log, then retry',
  publish_blocked_data_loss: 'publish was blocked to avoid data loss — review what would be dropped (view the log), then use "Publish anyway" on the row (or `orbit sqlserver publish <db> --force`) if the change is intentional',
  sql_server_unavailable: 'the configured SQL Server target is unavailable — run `orbit status`, start the target, then retry',
  database_busy: 'the database is busy — close DbGate and other active connections, then retry',
  publish_failed: 'publish failed — inspect the operation log for the sqlpackage error, then retry',
  reference_unresolved: 'a referenced dacpac is missing or unresolved — check that every referenced artifact is beside the project dacpac, then retry',
  reset_partial: 'Reset discarded local data, but the schema update failed. Fix the publish error in the log, then run Reset again.',
  reset_clean_state_missing: 'Reset could not find a saved clean state. Run Reset again to rebuild the database from its SQL project.',
  reset_restore_failed: 'Reset could not restore the clean state — close active database connections, check disk space, then retry',
  reset_prepare_failed: 'Reset could not save the clean state — close active database connections, check disk space, then retry',
}

// dbOpHint scans op output for a known failure signature, newest lines first,
// and returns the matching hint or null when the failure is unrecognized.
export function dbOpHint(op: string, lines: string[], errorCode?: string): string | null {
  if (errorCode && ERROR_CODE_HINTS[errorCode]) return ERROR_CODE_HINTS[errorCode]

  for (let i = lines.length - 1; i >= 0; i--) {
    for (const { re, hint } of HINTS) {
      if (re.test(lines[i])) {
        // The publish wording has one owner — the error-code map.
        if (op === 'publish' && re === DATA_LOSS_RE) {
          return ERROR_CODE_HINTS.publish_blocked_data_loss
        }
        return hint
      }
    }
  }
  return null
}
