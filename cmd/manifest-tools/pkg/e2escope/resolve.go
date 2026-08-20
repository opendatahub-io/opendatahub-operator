// Package e2escope figures out which e2e components and services a change
// affects, based on the files it touches, so a PR only runs the tests its
// change could actually break.
//
// It classifies each changed file's path, diffs manifests-config.yaml when
// that changed, falls back to searching source code for an env var
// reference when a manifest entry has no owner, applies aliases and ignore
// rules, and expands dependencies. All of that lives here, in one place,
// instead of being split across several packages that can drift apart.
package e2escope

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/opendatahub-io/opendatahub-operator/pkg/scoperules"
)

// Options configures Resolve.
type Options struct {
	// RepoRoot is the repository root. Diff-base detection, git diff, and
	// the source-reference search all run relative to it.
	RepoRoot string
	// RulesPath is the path to tests/e2e/scripts/e2e-scope-rules.yaml.
	RulesPath string
	// ConfigFile is the path to manifests-config.yaml.
	ConfigFile string
	// changedFiles overrides git-diff-based file discovery. Tests set
	// this to a fixed list so they don't need a real git history.
	changedFiles []string
	// base overrides diff-base detection (PULL_BASE_SHA, or a merge-base
	// against upstream/main or origin/main). Mainly for tests.
	base string
	// resolveManifest overrides how a manifest_files change resolves to
	// names. Defaults to ResolveManifestNames; tests swap in a stub to
	// avoid needing a real git history to diff manifests-config.yaml.
	resolveManifest func(ctx context.Context, repoRoot, configFile, base string, patterns *scoperules.CompiledPatterns) (Result, error)
}

// Result is the set of components and services a change affects, split by
// kind. Either slice can be empty -- Resolve only returns a Result once
// every changed file classified cleanly, so empty means "confirmed zero",
// not "unknown". The caller should skip that dimension entirely, not treat
// it as "nothing extra to add".
type Result struct {
	Components []string
	Services   []string
}

// Resolve computes which e2e components/services a change affects.
//
// Any doubt -- shared or unrecognized code, a manifest change that can't
// be cleanly attributed, an unknown dependency name -- returns an error
// instead of a partial Result, and the caller must run the full suite.
// Resolve never trades correctness for a narrower scope.
//
// A change that classifies cleanly but resolves to nothing selectable --
// every file ignored, or every matched name explicitly excluded
// (covered=false or an ignored name) -- is not doubt: it returns an empty,
// non-error Result, and the caller should run neither dimension rather than
// falling back to the full suite.
func Resolve(ctx context.Context, opts Options) (*Result, error) {
	rules, err := scoperules.Load(opts.RulesPath)
	if err != nil {
		return nil, err
	}
	patterns, err := rules.CompilePatterns()
	if err != nil {
		return nil, err
	}

	base := opts.base
	files := opts.changedFiles
	if files == nil {
		if base == "" {
			base, err = gitDiffBase(ctx, opts.RepoRoot)
			if err != nil {
				return nil, err
			}
		}
		files, err = changedFiles(ctx, opts.RepoRoot, base)
		if err != nil {
			return nil, err
		}
	}
	if len(files) == 0 {
		return nil, errors.New("no changed files")
	}

	components := map[string]bool{}
	services := map[string]bool{}
	sawManifest := false
	sawShared := false

	// Classify every file before deciding anything. A shared or
	// unrecognized file forces the full suite, but the loop still runs to
	// completion so every such file gets logged, not just the first one.
	for _, f := range files {
		c := patterns.ClassifyChangedFile(f)
		switch c.Kind {
		case scoperules.KindComponent:
			components[c.Name] = true
			Logf("%s -> component:%s", f, c.Name)
		case scoperules.KindService:
			services[c.Name] = true
			Logf("%s -> service:%s", f, c.Name)
		case scoperules.KindManifest:
			sawManifest = true
		case scoperules.KindIgnored:
			Logf("%s -> ignored", f)
		case scoperules.KindShared:
			Logf("%s -> shared (run all)", f)
			sawShared = true
		}
	}
	if sawShared {
		return nil, errors.New("shared/unrecognized code changed -- forcing full suite")
	}

	if sawManifest {
		if base == "" {
			base, err = gitDiffBase(ctx, opts.RepoRoot)
			if err != nil {
				return nil, fmt.Errorf("manifests-config.yaml changed but no diff base is available: %w", err)
			}
		}
		resolveManifest := opts.resolveManifest
		if resolveManifest == nil {
			resolveManifest = ResolveManifestNames
		}
		manifestResult, err := resolveManifest(ctx, opts.RepoRoot, opts.ConfigFile, base, patterns)
		if err != nil {
			return nil, err
		}
		for _, n := range manifestResult.Components {
			components[n] = true
			Logf("manifests-config.yaml -> component:%s", n)
		}
		for _, n := range manifestResult.Services {
			services[n] = true
			Logf("manifests-config.yaml -> service:%s", n)
		}
	}

	// Apply alias redirection and ignored-name/covered=false filtering the
	// same way to components and services -- ignored.names isn't
	// restricted to one kind, so both must honor it. names precomputes the
	// alias/ignored lookups once, so expandDependencies can apply the exact
	// same rules to a deps: target as filterAndValidate applies here.
	names, err := newNameIndex(rules)
	if err != nil {
		return nil, err
	}

	resolvedComponents, err := names.filterAndValidate(components, rules.Components, "component")
	if err != nil {
		return nil, err
	}
	resolvedServices, err := names.filterAndValidate(services, rules.Services, "service")
	if err != nil {
		return nil, err
	}

	if err := names.expandDependencies(resolvedComponents, resolvedServices, rules); err != nil {
		return nil, err
	}

	return &Result{
		Components: sortedKeys(resolvedComponents),
		Services:   sortedKeys(resolvedServices),
	}, nil
}

// nameIndex precomputes the alias-to-canonical and ignored-name lookups
// once per Resolve call, so filterAndValidate and expandDependencies apply
// the exact same resolution rules to every name they see, regardless of
// whether it arrived from a changed file, a manifest diff, or a deps:
// edge.
type nameIndex struct {
	componentAliases map[string]string
	serviceAliases   map[string]string
	ignored          map[string]bool
}

func newNameIndex(rules *scoperules.Rules) (*nameIndex, error) {
	ignored := make(map[string]bool, len(rules.Ignored.Names))
	for _, n := range rules.Ignored.Names {
		ignored[n] = true
	}
	componentAliases, err := aliasMap(rules.Components)
	if err != nil {
		return nil, fmt.Errorf("components: %w", err)
	}
	serviceAliases, err := aliasMap(rules.Services)
	if err != nil {
		return nil, fmt.Errorf("services: %w", err)
	}
	return &nameIndex{
		componentAliases: componentAliases,
		serviceAliases:   serviceAliases,
		ignored:          ignored,
	}, nil
}

// aliasMap builds the alias-to-canonical lookup for one kind (components or
// services). Each alias must resolve unambiguously: two entries can't
// declare the same alias (map iteration order would decide the winner),
// and an alias can't collide with another entry's own canonical name (the
// alias would silently shadow that entry).
func aliasMap(entries map[string]scoperules.Entry) (map[string]string, error) {
	aliasToCanonical := make(map[string]string, len(entries))
	for canonical, entry := range entries {
		for _, alias := range entry.Aliases {
			if _, isCanonical := entries[alias]; isCanonical {
				return nil, fmt.Errorf("alias %q on %q is itself a canonical entry", alias, canonical)
			}
			if existing, ok := aliasToCanonical[alias]; ok && existing != canonical {
				return nil, fmt.Errorf("alias %q is declared by both %q and %q", alias, existing, canonical)
			}
			aliasToCanonical[alias] = canonical
		}
	}
	return aliasToCanonical, nil
}

// resolve follows an alias to its canonical name, then checks it against
// ignored.names and covered=false. known is false only when canonical
// matches no entry at all -- callers that accept either kind combine this
// with a check against the other kind's entries before treating a name as
// genuinely unrecognized. drop is true when canonical is real but must
// never be selected on its own (ignored or covered=false); reason explains
// why, for logging.
func (idx *nameIndex) resolve(name string, aliases map[string]string, entries map[string]scoperules.Entry) (string, string, bool, bool) {
	canonical := name
	if c, ok := aliases[name]; ok {
		canonical = c
	}
	if idx.ignored[canonical] {
		return canonical, "belongs to a separately-tested subsystem", true, true
	}
	entry, ok := entries[canonical]
	if !ok {
		return canonical, "", false, false
	}
	if !entry.IsCovered() {
		return canonical, "no e2e coverage exists", true, true
	}
	return canonical, "", false, true
}

// filterAndValidate resolves aliases and drops ignored/covered=false names
// from a raw set of classified names. A name that's genuinely unknown --
// not registered, not covered=false, not ignored -- forces the full-suite
// fallback instead of passing through, since an unknown name would
// otherwise crash the e2e job at TestGroup.Validate().
func (idx *nameIndex) filterAndValidate(raw map[string]bool, entries map[string]scoperules.Entry, kind string) (map[string]bool, error) {
	aliases := idx.componentAliases
	if kind == "service" {
		aliases = idx.serviceAliases
	}

	resolved := make(map[string]bool, len(raw))
	for name := range raw {
		canonical, reason, drop, known := idx.resolve(name, aliases, entries)
		if !known {
			return nil, fmt.Errorf("%q does not match any known %s -- forcing full suite", canonical, kind)
		}
		if drop {
			Logf("dropping %s (%s): %s", canonical, kind, reason)
			continue
		}
		resolved[canonical] = true
	}
	return resolved, nil
}

// expandDependencies expands resolvedComponents/resolvedServices to a fixed
// point using a worklist, so each name's own deps are only ever looked up
// once, however many other names also depend on it. A dependency can be
// either kind regardless of which map lists it, and goes through the same
// alias/ignored/covered=false resolution as any other name -- a deps:
// entry naming an alias, an ignored name, or a covered=false target is
// handled identically to that same string arriving from a changed file. A
// dependency matching neither a known component nor service forces the
// full-suite fallback, the same as any other unrecognized name.
func (idx *nameIndex) expandDependencies(resolvedComponents, resolvedServices map[string]bool, rules *scoperules.Rules) error {
	pending := make([]string, 0, len(resolvedComponents)+len(resolvedServices))
	for n := range resolvedComponents {
		pending = append(pending, n)
	}
	for n := range resolvedServices {
		pending = append(pending, n)
	}

	for len(pending) > 0 {
		name := pending[len(pending)-1]
		pending = pending[:len(pending)-1]

		deps := append(append([]string{}, rules.Components[name].Deps...), rules.Services[name].Deps...)
		for _, dep := range deps {
			canonical, reason, drop, known := idx.resolve(dep, idx.componentAliases, rules.Components)
			target := resolvedComponents
			if !known {
				canonical, reason, drop, known = idx.resolve(dep, idx.serviceAliases, rules.Services)
				target = resolvedServices
			}
			if !known {
				return fmt.Errorf("%q (dependency of %s) does not match any known component or service -- forcing full suite", dep, name)
			}
			if drop {
				Logf("dropping %s (dependency of %s): %s", canonical, name, reason)
				continue
			}
			if !target[canonical] {
				target[canonical] = true
				Logf("+ %s (dependency of %s)", canonical, name)
				pending = append(pending, canonical)
			}
		}
	}
	return nil
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
