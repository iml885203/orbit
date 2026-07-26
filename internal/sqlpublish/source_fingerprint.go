package sqlpublish

// Project source scanning and fingerprinting — the change-detection
// primitives shared by the dacpac build cache (dacpac_cache.go) and the
// quick-diff state records (publish_state.go). A fingerprint answers
// "did anything that feeds the built dacpac change?" from file metadata
// alone; the per-file walk also powers Tier 3's file-level change list.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// buildExts are the source files whose contents feed the dacpac. A change
// to any of them (or the .sqlproj itself) must invalidate fingerprints.
var buildExts = map[string]bool{
	".sqlproj":     true,
	".sql":         true,
	".refactorlog": true,
	".dacpac":      true, // referenced dacpacs checked into the project tree
}

// toolFingerprint identifies the build toolchain cheaply: the sqlpackage
// binary's path+size+mtime. Reading it beats spawning `sqlpackage /Version`
// on every build, and a tool upgrade changes size or mtime so fingerprints
// still invalidate. Absent/unreadable binary yields a stable sentinel —
// buildDacpac re-checks the toolchain for real before using the cache.
func toolFingerprint() string {
	p, err := SqlpackagePath()
	if err != nil {
		return "no-tool"
	}
	info, err := os.Stat(p)
	if err != nil {
		return "no-tool"
	}
	return p + ":" + strconv.FormatInt(info.Size(), 10) + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
}

// sourceFile is one build-relevant source file as seen by a walk:
// identity (absolute slash path), cheap change signals (size, mtime),
// and the source root it was found under (for display-relative paths).
type sourceFile struct {
	Abs     string
	Size    int64
	MtimeNs int64
	Root    string
}

// collectSourceFiles walks the project AND its transitively referenced
// projects (see projectSourceRoots) and returns every build-relevant
// source file, sorted by absolute path. Content isn't read —
// path/size/mtime is enough to detect edits and far cheaper on a large
// project.
//
// Referenced projects matter for correctness, not just coverage: a
// <ProjectReference> (e.g. a shared CommonFiles project) contributes objects
// to the built dacpac via composite deployment, so an edit there changes the
// output. Walking only the leaf project's own tree would let a stale
// cache entry survive a change in a shared project — serving the wrong
// dacpac.
func collectSourceFiles(sqlProj string) ([]sourceFile, error) {
	roots, err := projectSourceRoots(sqlProj)
	if err != nil {
		return nil, err
	}
	var files []sourceFile
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// bin/ and obj/ hold build output, not source — skip so a prior
				// build's artifacts don't perturb the fingerprint.
				if name := d.Name(); name == "bin" || name == "obj" {
					return filepath.SkipDir
				}
				return nil
			}
			if !buildExts[strings.ToLower(filepath.Ext(path))] {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			files = append(files, sourceFile{
				Abs:     filepath.ToSlash(path),
				Size:    info.Size(),
				MtimeNs: info.ModTime().UnixNano(),
				Root:    root,
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Abs < files[j].Abs })
	return files, nil
}

// fingerprintFiles digests an already-collected source set plus the DB
// name and toolchain fingerprint. Files must be sorted (collectSourceFiles
// guarantees it) for a stable digest.
func fingerprintFiles(files []sourceFile, db string) string {
	h := sha256.New()
	fmt.Fprintf(h, "db=%s\x00tool=%s\x00", db, toolFingerprint())
	for _, f := range files {
		fmt.Fprintf(h, "%s\x00%s:%s\x00", f.Abs,
			strconv.FormatInt(f.Size, 10), strconv.FormatInt(f.MtimeNs, 10))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// projectFingerprint hashes every build-relevant source file across the
// project and its references, by path + size + mtime, plus the DB name
// and toolchain fingerprint.
func projectFingerprint(sqlProj, db string) (string, error) {
	files, err := collectSourceFiles(sqlProj)
	if err != nil {
		return "", err
	}
	return fingerprintFiles(files, db), nil
}

// projectRef mirrors the one bit of the .sqlproj we read: ProjectReference
// Include paths (MSBuild-relative, backslash-separated even on Unix).
type projectRef struct {
	References []struct {
		Include string `xml:"Include,attr"`
	} `xml:"ItemGroup>ProjectReference"`
}

// projectSourceRoots returns the source directories to fingerprint: the
// project's own directory plus every transitively referenced project's
// directory. Deduped by absolute path; cycles terminate. A reference whose
// .sqlproj can't be read is skipped — the same silence build/diff apply when
// a missing reference simply fails later with a clearer error.
func projectSourceRoots(sqlProj string) ([]string, error) {
	abs, err := filepath.Abs(sqlProj)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var roots []string
	queue := []string{abs}
	for len(queue) > 0 {
		proj := queue[0]
		queue = queue[1:]
		if seen[proj] {
			continue
		}
		seen[proj] = true
		roots = append(roots, filepath.Dir(proj))

		data, err := os.ReadFile(proj)
		if err != nil {
			continue // unreadable reference — skip, don't fail the whole fingerprint
		}
		var ref projectRef
		if xml.Unmarshal(data, &ref) != nil {
			continue
		}
		base := filepath.Dir(proj)
		for _, r := range ref.References {
			if r.Include == "" {
				continue
			}
			// MSBuild paths use backslashes; normalise for the host FS.
			rel := filepath.FromSlash(strings.ReplaceAll(r.Include, `\`, "/"))
			queue = append(queue, filepath.Clean(filepath.Join(base, rel)))
		}
	}
	sort.Strings(roots)
	return roots, nil
}
