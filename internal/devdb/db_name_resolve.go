package devdb

// The db-workflow CLI takes one positional argument that names *what* to act
// on. `orbit db list` prints both project names and database names, so users
// reasonably try either — this resolves whichever they typed to the concrete
// database(s) the command should operate on.
//
// Resolution order (a database name always wins over a same-spelled project):
//   1. exact database-name match  → that one database
//   2. case-insensitive project-name match → every database that project owns
//   3. no match → an error listing the closest candidates
//
// A project that owns exactly one database resolves as unambiguously as a
// database name. A multi-database project expands to all of them; callers that
// can't act on more than one at a time (reset) reject that and ask the user to
// name a database.

import (
	"fmt"
	"sort"
	"strings"
)

// resolvedArg is what a positional db/project argument resolved to.
type resolvedArg struct {
	// DBs is the database(s) the argument names — one for a database name or a
	// single-database project, several for a multi-database project.
	DBs []string
	// Project is set (to the matched project's name) when the argument named a
	// project rather than a database; empty when it named a database directly.
	Project string
}

// FromProject reports whether the argument named a project (vs a database).
func (r resolvedArg) FromProject() bool { return r.Project != "" }

// resolveDBArg maps a positional argument to the database(s) it names, over
// the configured projects. See the file comment for the resolution order.
func resolveDBArg(projects []DevDBProject, arg string) (resolvedArg, error) {
	// 1. Exact database name — the common case, and it takes precedence so a
	//    database is never shadowed by a like-named project.
	for _, p := range projects {
		for _, db := range p.Databases {
			if db == arg {
				return resolvedArg{DBs: []string{db}}, nil
			}
		}
	}

	// 2. Project name (case-insensitive: project names mirror folder names,
	//    which vary in case across checkouts).
	for _, p := range projects {
		if strings.EqualFold(p.Name, arg) {
			if len(p.Databases) == 0 {
				return resolvedArg{}, fmt.Errorf("project %q has no databases", p.Name)
			}
			dbs := append([]string(nil), p.Databases...)
			return resolvedArg{DBs: dbs, Project: p.Name}, nil
		}
	}

	return resolvedArg{}, unknownNameError(projects, arg)
}

// resolveSingleDBArg is resolveDBArg for commands that act on exactly one
// database (reset). It rejects a multi-database project with a message naming
// the databases to pick from, so a broad argument can't fan a destructive
// operation across several databases by surprise.
func resolveSingleDBArg(projects []DevDBProject, arg string) (string, error) {
	r, err := resolveDBArg(projects, arg)
	if err != nil {
		return "", err
	}
	if len(r.DBs) > 1 {
		return "", fmt.Errorf("%q is a project with %d databases (%s) — name one of them",
			r.Project, len(r.DBs), strings.Join(r.DBs, ", "))
	}
	return r.DBs[0], nil
}

// unknownNameError builds a "not found" error that lists the closest
// candidates so the user can correct a typo without running `orbit db list`.
func unknownNameError(projects []DevDBProject, arg string) error {
	var names []string
	for _, p := range projects {
		names = append(names, p.Name)
		names = append(names, p.Databases...)
	}
	sort.Strings(names)

	near := closestNames(names, arg, 3)
	if len(near) > 0 {
		return fmt.Errorf("no project or database named %q — did you mean %s? (see `orbit db list`)",
			arg, strings.Join(near, ", "))
	}
	return fmt.Errorf("no project or database named %q — check `orbit db list`", arg)
}

// closestNames returns up to max candidate names ranked by edit distance to
// arg, keeping only reasonably-close ones (within a third of the longer
// string's length) so a wild typo doesn't get nonsense suggestions.
func closestNames(names []string, arg string, max int) []string {
	type scored struct {
		name string
		dist int
	}
	var out []scored
	for _, n := range names {
		d := levenshtein(strings.ToLower(n), strings.ToLower(arg))
		limit := len(n)
		if len(arg) > limit {
			limit = len(arg)
		}
		if d*3 <= limit {
			out = append(out, scored{n, d})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].dist != out[j].dist {
			return out[i].dist < out[j].dist
		}
		return out[i].name < out[j].name
	})
	var names2 []string
	for i, s := range out {
		if i >= max {
			break
		}
		names2 = append(names2, s.name)
	}
	return names2
}

// levenshtein is the classic edit distance, used only for typo suggestions.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
