package devdb

// Project resolution for the DB workflow: the team-shared allowlist
// (db_projects) located under the workspace by case-insensitive folder
// name, and the database→.sqlproj lookup over the result.

import (
	"errors"
	"fmt"
)

// errWorkspaceRootUnavailable is the shared user-facing hint for every
// devdb path that needs a workspace root and doesn't have one.
var errWorkspaceRootUnavailable = errors.New("workspace root unavailable; set WORKSPACE_ROOT or configure workspace_root in settings")

// allProjects returns the SQL projects the team's shared allowlist names
// (db_projects / envs/data/db-projects.yaml), located under the
// workspace by case-insensitive folder name. No allowlist means no
// projects: the workflow never publishes a folder the team didn't
// declare, whatever else happens to sit beside it in a given checkout.
func (f *dbFeature) allProjects() ([]DevDBProject, error) {
	workspaceRoot := f.workspaceRoot()
	if workspaceRoot == "" {
		return nil, errWorkspaceRootUnavailable
	}
	allow := dbProjectAllowlist(f.host.Config())
	if len(allow) == 0 {
		return nil, nil
	}
	return listAllowedProjects(workspaceRoot, f.dbRoot(), allow)
}

// sqlProjForDatabase maps a database name to its actual .sqlproj path
// within the given projects — the one lookup the CLI (wire response)
// and the daemon both use. It rides discovery's own layout
// and naming rules (listSQLProjFiles, sqlProjDatabaseName): a listed
// database must resolve to the file that listed it. A project whose
// glob errors is skipped — the same silence discovery applies.
func sqlProjForDatabase(projects []DevDBProject, dbName string) (string, bool) {
	for _, p := range projects {
		for _, db := range p.Databases {
			if db != dbName {
				continue
			}
			files, err := listSQLProjFiles(p.Path)
			if err != nil {
				continue
			}
			for _, sqlProj := range files {
				if sqlProjDatabaseName(sqlProj) == dbName {
					return sqlProj, true
				}
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
	return "", fmt.Errorf("database %q not found — check `orbit db list`", dbName)
}

// publishTargetsForDBs maps a set of database names to their publish targets,
// in the given order, first occurrence wins. A database whose .sqlproj can't
// be resolved is skipped — it never publishes individually either. Shared by
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
