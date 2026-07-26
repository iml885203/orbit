package devdb

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sqlProject pairs a discovered project directory with the databases it
// declares.
type sqlProject struct {
	Name      string
	Path      string
	Databases []string
}

// sqlProjectSubdirs walks root's immediate subdirectories and returns
// those that are SQL projects — carrying the <project>/*/*.sqlproj
// layout (discoverSQLProjects owns that rule). Single owner of the
// "which dirs under here are projects?" walk: the allowlist scan, init
// validation, and doctor all ride it. A missing root yields no projects
// (not an error).
func sqlProjectSubdirs(root string) ([]sqlProject, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var found []sqlProject
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		dbs, err := discoverSQLProjects(path)
		if err != nil {
			return nil, err
		}
		if len(dbs) == 0 {
			continue
		}
		found = append(found, sqlProject{Name: entry.Name(), Path: path, Databases: dbs})
	}
	return found, nil
}

// findSQLProjectDirs returns the names of SQL-project subdirectories
// under root, swallowing errors — for the init wizard's and doctor's
// human-facing validation of an ORBIT_DB_ROOT candidate.
func findSQLProjectDirs(root string) []string {
	subdirs, _ := sqlProjectSubdirs(root)
	names := make([]string, 0, len(subdirs))
	for _, sp := range subdirs {
		names = append(names, sp.Name)
	}
	return names
}

// listAllowedProjects locates the allowlisted SQL projects under the
// scan roots: every subdirectory whose FOLDED name is in allow and which
// carries the <project>/*/*.sqlproj layout. The scan is bounded by the
// allowlist — it never returns a folder the team didn't name, whatever
// else is checked out — and the case-folded match lets one shared list
// work across machines that cased a folder differently.
func listAllowedProjects(workspaceRoot, dbRoot string, allow map[string]bool) ([]DevDBProject, error) {
	scanRoots := []string{workspaceRoot, filepath.Join(workspaceRoot, "dbprojects")}
	if dbRoot != "" {
		// User-configured override takes precedence in search order.
		scanRoots = append([]string{dbRoot}, scanRoots...)
	}

	seen := make(map[string]bool)
	var projects []DevDBProject
	for _, scanRoot := range scanRoots {
		subdirs, err := sqlProjectSubdirs(scanRoot)
		if err != nil {
			return nil, err
		}
		for _, sp := range subdirs {
			if !allow[strings.ToLower(sp.Name)] || seen[sp.Path] {
				continue
			}
			seen[sp.Path] = true
			projects = append(projects, DevDBProject{
				Name:      sp.Name,
				Path:      sp.Path,
				Databases: append([]string{}, sp.Databases...),
			})
		}
	}

	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	return projects, nil
}

// listSQLProjFiles globs the <root>/*/*.sqlproj layout every SQL
// project root uses. Single owner of the layout rule — discovery and
// database→file lookup must accept the same shape.
func listSQLProjFiles(projectPath string) ([]string, error) {
	return filepath.Glob(filepath.Join(projectPath, "*", "*.sqlproj"))
}

// sqlProjDatabaseName returns the database a .sqlproj file declares:
// its base filename. Single owner of the file→database naming rule.
func sqlProjDatabaseName(sqlProj string) string {
	base := filepath.Base(sqlProj)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// discoverSQLProjects lists the databases a project directory declares
// via the <project>/*/*.sqlproj layout. A non-empty result is also the
// structural test for "is this dir a SQL project?" — the single rule the
// allowlist scan, init validation, and doctor all ride.
func discoverSQLProjects(projectPath string) ([]string, error) {
	matches, err := listSQLProjFiles(projectPath)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var dbs []string
	for _, match := range matches {
		name := sqlProjDatabaseName(match)
		if name == "CommonFiles" || seen[name] {
			continue
		}
		seen[name] = true
		dbs = append(dbs, name)
	}
	sort.Strings(dbs)
	return dbs, nil
}
