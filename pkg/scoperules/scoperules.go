// Package scoperules parses tests/e2e/scripts/e2e-scope-rules.yaml, the
// config that maps directory conventions to component/service names for
// path-based e2e test scoping. It exposes the names the file declares and
// compiles its path patterns into ClassifyChangedFile, the one place that
// decides what a changed file's path means.
package scoperules

import (
	"bytes"
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Rules struct {
	Patterns struct {
		ComponentPatterns []string `yaml:"components"`
		ServicePatterns   []string `yaml:"services"`
	} `yaml:"patterns"`
	Ignored struct {
		Patterns []string `yaml:"patterns"`
		Names    []string `yaml:"names"`
	} `yaml:"ignored"`
	FrameworkDirs []string         `yaml:"framework_dirs"`
	ManifestFiles []string         `yaml:"manifest_files"`
	Components    map[string]Entry `yaml:"components"`
	Services      map[string]Entry `yaml:"services"`
}

// DefaultPath is this file's own location, relative to the repository root.
const DefaultPath = "tests/e2e/scripts/e2e-scope-rules.yaml"

type Entry struct {
	Deps    []string `yaml:"deps"`
	Aliases []string `yaml:"aliases"`
	Covered *bool    `yaml:"covered"`
}

// IsCovered reports whether this entry has real e2e coverage. Absent
// (nil) defaults to true -- only an explicit `covered: false` opts out.
func (e Entry) IsCovered() bool {
	return e.Covered == nil || *e.Covered
}

// DepsTargets is every name that appears in some entry's deps list. A
// dependency target counts as legitimate without its own top-level entry
// -- see Rules.Components/Services' own field docs.
func DepsTargets(entries map[string]Entry) map[string]bool {
	targets := map[string]bool{}
	for _, entry := range entries {
		for _, d := range entry.Deps {
			targets[d] = true
		}
	}
	return targets
}

func Load(path string) (*Rules, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	// KnownFields rejects a typo'd or misspelled key (e.g. "depss:" instead
	// of "deps:") instead of silently leaving the field at its zero value.
	var rules Rules
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&rules); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	return &rules, nil
}

// KnownNames is every name this file recognizes: a components/services
// entry (regardless of covered status), one of its aliases, or an
// explicitly ignored name. "Known" means "accounted for", not
// "selectable" -- an ignored or covered=false name is known but will never
// actually run.
func (r *Rules) KnownNames() map[string]bool {
	known := make(map[string]bool)
	addEntries := func(entries map[string]Entry) {
		for name, entry := range entries {
			known[name] = true
			for _, a := range entry.Aliases {
				known[a] = true
			}
		}
	}
	addEntries(r.Components)
	addEntries(r.Services)
	for _, n := range r.Ignored.Names {
		known[n] = true
	}
	return known
}

// CompiledPatterns holds the component/service/ignored path patterns
// compiled as Go regexps, ready to classify a file path.
type CompiledPatterns struct {
	Components     []*regexp.Regexp
	Services       []*regexp.Regexp
	Ignored        []*regexp.Regexp
	frameworkDirs  map[string]bool
	manifestFiles  map[string]bool
	componentNames map[string]bool
	serviceNames   map[string]bool
}

func (r *Rules) CompilePatterns() (*CompiledPatterns, error) {
	comp, err := compileAll(r.Patterns.ComponentPatterns)
	if err != nil {
		return nil, err
	}
	svc, err := compileAll(r.Patterns.ServicePatterns)
	if err != nil {
		return nil, err
	}
	ign, err := compileAll(r.Ignored.Patterns)
	if err != nil {
		return nil, err
	}

	frameworkDirs := make(map[string]bool, len(r.FrameworkDirs))
	for _, d := range r.FrameworkDirs {
		frameworkDirs[d] = true
	}
	manifestFiles := make(map[string]bool, len(r.ManifestFiles))
	for _, f := range r.ManifestFiles {
		manifestFiles[f] = true
	}
	componentNames := make(map[string]bool, len(r.Components))
	for name := range r.Components {
		componentNames[name] = true
	}
	serviceNames := make(map[string]bool, len(r.Services))
	for name := range r.Services {
		serviceNames[name] = true
	}

	return &CompiledPatterns{
		Components:     comp,
		Services:       svc,
		Ignored:        ign,
		frameworkDirs:  frameworkDirs,
		manifestFiles:  manifestFiles,
		componentNames: componentNames,
		serviceNames:   serviceNames,
	}, nil
}

func compileAll(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("compiling pattern %q: %w", p, err)
		}
		compiled[i] = re
	}
	return compiled, nil
}

// Classification is the name a pattern captured from a path, and whether
// it came from a component or a service pattern.
type Classification struct {
	Name      string
	IsService bool
}

// Classify returns the name a component or service pattern captures from
// path, and which kind matched. framework_dirs matches (e.g. "registry", a
// shared mechanism, not a real component) are excluded, so ok is false for
// those too. Used both by ClassifyChangedFile below and to classify a
// source file found during a repo-wide search.
//
// A pattern shared by both kinds (e.g. tests/e2e/*_test.go, listed once
// under components) can capture a name that's only ever registered as a
// service, or vice versa. Whichever entry map actually has the name wins
// over the pattern list it happened to match, so that name isn't
// misreported as an unknown component/service and doesn't force the full
// suite for no reason.
func (p *CompiledPatterns) Classify(path string) (Classification, bool) {
	for _, re := range p.Components {
		if m := re.FindStringSubmatch(path); len(m) > 1 {
			name := m[1]
			if p.frameworkDirs[name] {
				return Classification{}, false
			}
			isService := p.serviceNames[name] && !p.componentNames[name]
			return Classification{Name: name, IsService: isService}, true
		}
	}
	for _, re := range p.Services {
		if m := re.FindStringSubmatch(path); len(m) > 1 {
			name := m[1]
			if p.frameworkDirs[name] {
				return Classification{}, false
			}
			isService := !p.componentNames[name] || p.serviceNames[name]
			return Classification{Name: name, IsService: isService}, true
		}
	}
	return Classification{}, false
}

// FileKind is the outcome of classifying one changed file's path.
type FileKind int

const (
	// KindShared is the fallthrough for a path matching no other rule --
	// treated as cross-cutting, forcing a full-suite run.
	KindShared FileKind = iota
	// KindIgnored never affects e2e scoping: it doesn't select anything,
	// and it never forces the full-suite fallback either.
	KindIgnored
	// KindManifest is an exact match against Rules.ManifestFiles. Its
	// content, not just the fact that it changed, decides which names it
	// affects -- see manifestdiff for that diff.
	KindManifest
	KindComponent
	KindService
)

// FileClassification is ClassifyChangedFile's result. Name is set only for
// KindComponent/KindService.
type FileClassification struct {
	Kind FileKind
	Name string
}

// ClassifyChangedFile classifies one changed file's path: an ignored
// pattern, then an exact Rules.ManifestFiles match, then a component/
// service pattern via Classify, then KindShared as the fallthrough. This
// is the one place every changed file gets classified.
func (p *CompiledPatterns) ClassifyChangedFile(path string) FileClassification {
	for _, re := range p.Ignored {
		if re.MatchString(path) {
			return FileClassification{Kind: KindIgnored}
		}
	}
	if p.manifestFiles[path] {
		return FileClassification{Kind: KindManifest}
	}
	if c, ok := p.Classify(path); ok {
		if c.IsService {
			return FileClassification{Kind: KindService, Name: c.Name}
		}
		return FileClassification{Kind: KindComponent, Name: c.Name}
	}
	return FileClassification{Kind: KindShared}
}
