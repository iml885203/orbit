package devdb

// Explicit SQL Server project resolution and database→.sqlproj lookup.

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// errWorkspaceRootUnavailable is the shared user-facing hint for every
// devdb path that needs a workspace root and doesn't have one.
var errWorkspaceRootUnavailable = errors.New("workspace unavailable; remove the source and add it again with --workspace <path>")

// allProjects resolves only .sqlproj files declared in the sqlserver section.
// Paths are workspace-relative so the environment has one shared source of
// truth without scanning sibling checkouts or requiring per-machine DB roots.
func (f *dbFeature) allProjects() ([]DevDBProject, error) {
	workspaceRoot := f.workspaceRoot()
	if workspaceRoot == "" {
		return nil, errWorkspaceRootUnavailable
	}
	section := SQLServerFrom(f.host.Config())
	if section == nil {
		return nil, nil
	}
	projects := make([]DevDBProject, 0, len(section.Projects))
	for _, configured := range section.Projects {
		path := filepath.Join(workspaceRoot, configured.Path)
		databases := databaseNamesForProject(configured)
		projects = append(projects, DevDBProject{
			Name:      strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
			Path:      path,
			Databases: databases,
		})
	}
	return projects, nil
}

// sqlProjForDatabase maps a database name to its actual .sqlproj path
// within the given projects — the one lookup the CLI (wire response)
// and the daemon both use.
func sqlProjForDatabase(projects []DevDBProject, dbName string) (string, bool) {
	for _, p := range projects {
		for _, db := range p.Databases {
			if db == dbName {
				return p.Path, true
			}
		}
	}
	return "", false
}

// publishTargetRef pairs a database with the .sqlproj that declares it.
type publishTargetRef struct {
	DB      string
	SQLProj string
}

// sqlProjForDatabaseOrError is sqlProjForDatabase with the shared not-found
// error the CLI commands (publish/reset/diff) return when a resolved database
// name has no .sqlproj in the fetched project list — the one place that
// message lives. (Named to pair with sqlProjForDatabase and to avoid
// colliding with the daemon-side dbFeature.resolveSQLProj method, which
// resolves over f.allProjects() with its own message.)
func sqlProjForDatabaseOrError(projects []DevDBProject, dbName string) (string, error) {
	if proj, ok := sqlProjForDatabase(projects, dbName); ok {
		return proj, nil
	}
	return "", fmt.Errorf("database %q not found — check `orbit sqlserver list`", dbName)
}

// publishTargetsForDBs maps a set of database names to their publish targets,
// in the given order, first occurrence wins. A database absent from the
// explicit project list is skipped. Shared by
// the `--all` work list and a single project's fan-out.
func publishTargetsForDBs(projects []DevDBProject, dbs []string) []publishTargetRef {
	seen := map[string]bool{}
	var targets []publishTargetRef
	for _, db := range dbs {
		if seen[db] {
			continue
		}
		if proj, ok := sqlProjForDatabase(projects, db); ok {
			seen[db] = true
			targets = append(targets, publishTargetRef{DB: db, SQLProj: proj})
		}
	}
	return targets
}

// publishTargetsFrom lists every publishable database across projects,
// in project order, first declaration wins. The unit `publish --all`
// iterates, shared by the CLI fetch and the daemon handler.
func publishTargetsFrom(projects []DevDBProject) []publishTargetRef {
	var dbs []string
	for _, p := range projects {
		dbs = append(dbs, p.Databases...)
	}
	return publishTargetsForDBs(projects, dbs)
}

// allPublishTargets is publishTargetsFrom over the daemon's own
// project merge.
func (f *dbFeature) allPublishTargets() ([]publishTargetRef, error) {
	projects, err := f.allProjects()
	if err != nil {
		return nil, err
	}
	return publishTargetsFrom(projects), nil
}
