// Hand-maintained (tygo skips internal/sqlpublish: diff.go's xml-tagged
// report structs confuse its loader). Mirrors the JSON wire shape of the
// exported diff types in internal/sqlpublish/diff.go — keep in sync by hand
// if those structs change.

/**
 * DiffOp is one schema change a publish would apply.
 */
export interface DiffOp {
  action: string /* "Create" | "Alter" | "Drop" */;
  object_type: string;
  name: string;
  data_loss: boolean;
}

/**
 * DiffAlert is a warning sqlpackage raised about the deployment (most
 * commonly possible data loss or data motion).
 */
export interface DiffAlert {
  kind: string;
  message: string;
}

/**
 * FileChange is one source-file difference versus the last publish —
 * the fast approximation of "what changed" when the engine hasn't run.
 */
export interface FileChange {
  action: string /* "Added" | "Modified" | "Deleted" */;
  path: string;
}

/**
 * DiffResult is the structured outcome of a schema diff.
 */
export interface DiffResult {
  db: string;
  in_sync: boolean;
  created: number /* int */;
  altered: number /* int */;
  dropped: number /* int */;
  data_loss: boolean;
  ops: DiffOp[];
  alerts: DiffAlert[];
  /**
   * Quick marks a result produced without running the engine: either
   * "unchanged since last publish" (in_sync) or a file-level change
   * list (file_changes).
   */
  quick?: boolean;
  /**
   * FileChanges names the source files that moved since the last
   * publish; ops stays empty. An analyzed diff turns these into exact
   * operations.
   */
  file_changes?: FileChange[];
  /**
   * Cached marks engine operations replayed from the diff-result cache —
   * exact, just not recomputed.
   */
  cached?: boolean;
}
