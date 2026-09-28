// Package manifestdiff structurally compares two versions of
// manifests-config.yaml to determine which components changed, for
// path-based e2e test scoping. It compares parsed config.ManifestsConfig
// values rather than diffing text, so reordering, comments, or formatting
// changes never produce a false positive.
package manifestdiff

import (
	"reflect"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

// Diff is ChangedNames' result. Names and UnattributedEnvVars are both
// []string with different meanings -- named fields instead of two
// positional returns rule out a caller accidentally swapping them, which
// would compile cleanly and fail silently.
type Diff struct {
	// Names is every name whose entry differs between old and new across
	// the Components, CCMCharts, and ComponentCharts sections of
	// manifests-config.yaml (added, removed, or modified), plus
	// imageOverrides entries that changed and do carry a Component field.
	Names []string
	// UnattributedEnvVars lists changed imageOverrides entries with no
	// Component field at all -- most entries are auto-discovered from a
	// ClusterServiceVersion (source: "csv") and were never assigned one
	// (see resolver.Resolve's CSV-import pass). Callers can attempt their
	// own fallback attribution for these (e.g. searching component source
	// for a reference to the env var name) before treating them as
	// unattributable.
	UnattributedEnvVars []string
	// PlatformManifestsChanged is true when the platformManifests section
	// changed. That section isn't attributable to any single component --
	// it can repoint where a whole platform's manifests come from -- so a
	// caller must treat this the same as any other unattributable change
	// and force the full suite, even when Names or UnattributedEnvVars also
	// resolved something from the rest of the same diff.
	PlatformManifestsChanged bool
}

// ChangedNames diffs oldCfg against newCfg. Every field of the result being
// empty/false means nothing in the sections it covers changed at all --
// e.g. the diff is structural/comment-only. Callers must treat
// PlatformManifestsChanged, and any unresolved UnattributedEnvVars entry,
// the same as any other unattributable change, regardless of whether Names
// also has entries.
//
// A nil oldCfg or newCfg is treated as an empty config, not a caller error.
func ChangedNames(oldCfg, newCfg *config.ManifestsConfig) Diff {
	if oldCfg == nil {
		oldCfg = &config.ManifestsConfig{}
	}
	if newCfg == nil {
		newCfg = &config.ManifestsConfig{}
	}

	var diff Diff
	diff.PlatformManifestsChanged = !reflect.DeepEqual(oldCfg.PlatformManifests, newCfg.PlatformManifests)
	seen := map[string]bool{}
	add := func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		diff.Names = append(diff.Names, n)
	}

	for _, n := range diffComponents(oldCfg.Components, newCfg.Components) {
		add(n)
	}
	for _, n := range diffComponents(oldCfg.CCMCharts, newCfg.CCMCharts) {
		add(n)
	}
	for _, n := range diffComponents(oldCfg.ComponentCharts, newCfg.ComponentCharts) {
		add(n)
	}

	for _, co := range diffImageOverrides(oldCfg.ImageOverrides, newCfg.ImageOverrides) {
		if co.Component == "" {
			diff.UnattributedEnvVars = append(diff.UnattributedEnvVars, co.EnvVar)
			continue
		}
		add(co.Component)
	}

	return diff
}

func diffComponents(oldM, newM map[string]config.Component) []string {
	var changed []string
	for name, newVal := range newM {
		if oldVal, existed := oldM[name]; !existed || !reflect.DeepEqual(oldVal, newVal) {
			changed = append(changed, name)
		}
	}
	for name := range oldM {
		if _, stillExists := newM[name]; !stillExists {
			changed = append(changed, name)
		}
	}
	return changed
}

type changedOverride struct {
	EnvVar    string
	Component string
}

// ImageOverrides are keyed by RELATED_IMAGE_* env var name, not by
// component -- a changed entry is attributed to its own Component field,
// which may be empty (see ChangedNames' unattributedEnvVars). A modified
// entry whose Component field itself changed (not just its digest) is
// attributed to both the new and the old component: the old one just lost
// this override, which is as much a real change to it as gaining one.
func diffImageOverrides(oldM, newM map[string]config.ImageOverride) []changedOverride {
	var changed []changedOverride
	for key, newVal := range newM {
		oldVal, existed := oldM[key]
		if existed && reflect.DeepEqual(oldVal, newVal) {
			continue
		}
		changed = append(changed, changedOverride{EnvVar: key, Component: newVal.Component})
		if existed && oldVal.Component != "" && oldVal.Component != newVal.Component {
			changed = append(changed, changedOverride{EnvVar: key, Component: oldVal.Component})
		}
	}
	for key, oldVal := range oldM {
		if _, stillExists := newM[key]; !stillExists {
			changed = append(changed, changedOverride{EnvVar: key, Component: oldVal.Component})
		}
	}
	return changed
}
