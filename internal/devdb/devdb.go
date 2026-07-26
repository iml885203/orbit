package devdb

// Wire types of the DB-workflow read surface: the project list and the
// env metadata the dashboard and CLI consume. (The devdb rebuild
// manager that used to live here retired with the image-build flow.)

type DevDBProject struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Databases []string `json:"databases"`
}

type DevDBProjectsResponse struct {
	Projects []DevDBProject `json:"projects"`
}

type DevDBMetaResponse struct {
	EnvironmentPath string `json:"environment_path"`
	EnvironmentName string `json:"environment_name"`
	SQLServerImage  string `json:"sql_server_image"`
	// SQLServerPort is the container's published host port — the target
	// `orbit db publish` connects to from the host.
	SQLServerPort int `json:"sql_server_port,omitempty"`
	// SQLServerTarget is the publish target's runtime docker name. The
	// CLI resolves SA_PASSWORD by inspecting THIS container so port and
	// credentials always come from the same declared target. Empty on
	// daemons predating the field — consumers fall back to the legacy
	// sql-server name.
	SQLServerTarget string `json:"sql_server_target,omitempty"`
	WorkspaceRoot   string `json:"workspace_root"`
	// DBConfigured mirrors the DB-workflow gate: false when the active
	// env has neither a sql-server container nor a declared sql_projects
	// target. CLI and dashboard hide the
	// DB workflow behind it. A pointer so consumers can fail OPEN when the
	// field is absent — a CLI newer than its daemon must not lock
	// users out of db commands just because the old daemon doesn't report it.
	// Interpret via WorkflowConfigured, not by dereferencing directly.
	DBConfigured *bool `json:"db_configured,omitempty"`
	// ClaimConfigured is false when the active env has no claim section
	// (Tunlease tunnel support). The dashboard hides the Tunnels tab behind
	// it, the same way DBConfigured gates Local DB. Same pointer +
	// fail-open rule: nil (a daemon predating the field) counts as
	// configured so the tab never wrongly vanishes for
	// users on an older daemon. Interpret via ClaimSupported.
	//
	// This DB-named response carries the tunnel gate too — the pragmatic
	// choice over a second endpoint. If a third feature needs a UI gate,
	// promote this to a general feature-meta type.
	ClaimConfigured *bool `json:"claim_configured,omitempty"`
}

// WorkflowConfigured interprets DBConfigured fail-open: nil (a daemon that
// predates the field) counts as configured. The daemon type owns this
// version-skew rule so consumers can't each re-derive it differently.
func (m *DevDBMetaResponse) WorkflowConfigured() bool {
	return m.DBConfigured == nil || *m.DBConfigured
}

// ClaimSupported interprets ClaimConfigured fail-open, mirroring
// WorkflowConfigured.
func (m *DevDBMetaResponse) ClaimSupported() bool {
	return m.ClaimConfigured == nil || *m.ClaimConfigured
}
