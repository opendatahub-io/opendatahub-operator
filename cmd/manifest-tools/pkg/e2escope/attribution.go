package e2escope

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/pkg/scoperules"
)

// skippedSourceSearchDirs are top-level directories that never contain Go
// source but can be large (bin/ holds downloaded tool binaries, opt/ holds
// downloaded manifests/charts) -- descending into them wastes a directory
// walk that can never find a match. Checked only at the repo root: a
// directory that happens to share one of these names deeper in the tree
// (e.g. a component's own build/) is real source and must still be
// searched. .git is skipped separately, at any depth, in case a submodule
// ever nests one.
var skippedSourceSearchDirs = map[string]bool{
	"bin":   true,
	"build": true,
	"opt":   true,
}

// logWriter defaults to os.Stderr; tests reassign it directly (see
// resolve_test.go) to capture and assert on Logf output.
var logWriter io.Writer = os.Stderr

// Logf writes a diagnostic line prefixed with "SELECTIVE-E2E:", so a Prow
// job log can be grepped for every decision this package makes.
func Logf(format string, args ...any) {
	fmt.Fprintf(logWriter, "SELECTIVE-E2E: "+format+"\n", args...)
}

// ResolveUnattributedEnvVars is the fallback for imageOverrides entries
// with no Component field (most are auto-discovered and never get one --
// see manifestdiff.ChangedNames). It searches source code for a reference
// to the env var and classifies whichever file contains it, using the same
// path patterns as a regular changed file.
//
// An env var referenced from more than one real owner (say, a shared image
// used by both a component and a service) is attributed to all of them,
// each keeping its own kind, not assumed to always be a component.
//
// patterns is the caller's already-compiled rules, passed in so a single
// Resolve call only parses the rules file once no matter how many lookups
// it needs.
//
// Returns an error if any envVar has no reference anywhere. The whole
// batch is then discarded, even the vars that did resolve, so the caller
// falls back to the full suite instead of shipping an incomplete scope.
// Every envVar is still checked and logged first, so a batch with several
// misses shows all of them, not just the first.
func ResolveUnattributedEnvVars(repoRoot string, envVars []string, patterns *scoperules.CompiledPatterns) (Result, error) {
	owners, err := findOwnersBySourceReference(repoRoot, envVars, patterns)
	if err != nil {
		return Result{}, err
	}

	var result Result
	var unresolved []string
	for _, envVar := range envVars {
		classifications := owners[envVar]
		if len(classifications) == 0 {
			Logf("%s has no Component field and no source reference found — not attributed", envVar)
			unresolved = append(unresolved, envVar)
			continue
		}

		names := make([]string, len(classifications))
		for i, c := range classifications {
			names[i] = c.Name
			if c.IsService {
				result.Services = append(result.Services, c.Name)
			} else {
				result.Components = append(result.Components, c.Name)
			}
		}
		Logf("%s -> %s (via source reference)", envVar, strings.Join(names, ","))
	}
	if len(unresolved) > 0 {
		return Result{}, fmt.Errorf("no source reference found for: %s", strings.Join(unresolved, ", "))
	}
	return result, nil
}

// findOwnersBySourceReference walks repoRoot once, checking every pending
// env var against every relevant file, so a bulk digest-bump PR touching
// many unattributed entries only needs one walk.
//
// "Relevant file" means patterns.Classify matches its path -- the same
// patterns used to classify a regular changed file, not a separate list to
// keep in sync by hand. Test files (_test.go) are skipped: a reference
// that only exists in a test isn't evidence the component needs the image
// at runtime.
//
// Each env var is matched on a word boundary, not as a substring, so a
// longer name that contains a shorter one doesn't produce a false match.
//
// Returns a map from env var to every classification (component or
// service) whose files reference it. An env var missing from the result
// was found nowhere. Returns an error if the search pattern itself can't
// be compiled -- envVars is caller-sized, and a very large batch could in
// principle exceed the regexp engine's limits, so this must fail closed
// through an error rather than panic. Also returns an error if repoRoot
// itself can't be walked (doesn't exist, isn't readable), so that failure
// surfaces as a filesystem error instead of every env var silently
// reporting "not attributed".
func findOwnersBySourceReference(repoRoot string, envVars []string, patterns *scoperules.CompiledPatterns) (map[string][]scoperules.Classification, error) {
	if len(envVars) == 0 {
		return nil, nil
	}

	names := make([]string, len(envVars))
	for i, v := range envVars {
		names[i] = regexp.QuoteMeta(v)
	}
	combined, err := regexp.Compile(`\b(?:` + strings.Join(names, "|") + `)\b`)
	if err != nil {
		return nil, fmt.Errorf("compiling env var search pattern for %d env vars: %w", len(envVars), err)
	}

	found := make(map[string]map[scoperules.Classification]bool, len(envVars)) // envVar -> set of classifications referencing it

	walkErr := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == repoRoot {
				// The root itself failed to stat -- repoRoot doesn't exist or
				// isn't readable. Stop the walk and surface this, instead of
				// letting it masquerade as "no source reference found" below.
				return err
			}
			// Don't abort the whole search over one bad entry, but log it --
			// a silent skip here could produce a wrong "not attributed" result.
			Logf("skipping %s during source search: %v", path, err)
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || (filepath.Dir(path) == repoRoot && skippedSourceSearchDirs[d.Name()]) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		relPath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			Logf("skipping %s during source search: %v", path, err)
			return nil
		}
		classification, matched := patterns.Classify(relPath)
		if !matched {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			Logf("skipping %s during source search: %v", path, err)
			return nil
		}
		for _, m := range combined.FindAll(data, -1) {
			envVar := string(m)
			if found[envVar] == nil {
				found[envVar] = map[scoperules.Classification]bool{}
			}
			found[envVar][classification] = true
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("searching %s for source references: %w", repoRoot, walkErr)
	}

	resolved := make(map[string][]scoperules.Classification, len(found))
	for envVar, set := range found {
		list := make([]scoperules.Classification, 0, len(set))
		for c := range set {
			list = append(list, c)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		resolved[envVar] = list
	}
	return resolved, nil
}
