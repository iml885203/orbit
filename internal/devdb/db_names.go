package devdb

// Database-name validation for the DB workflow — the one grammar every
// entry point (CLI commands and daemon handlers) checks user-supplied
// names against.

import "regexp"

// safeDBName: plain alphanumerics + underscore, matching SQL Server
// identifier rules well enough to reject injection payloads.
var safeDBName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// safeArgName is the grammar for a positional db/project argument before it's
// resolved: it must admit project names too (folder names carry '.' and '-',
// e.g. "acme.billing"), so it's looser than safeDBName — but still rejects the
// shell/SQL metacharacters an injection payload needs. The resolved database
// name is re-checked against the stricter safeDBName before any tool runs.
var safeArgName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
