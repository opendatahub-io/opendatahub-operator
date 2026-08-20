package e2escope

import (
	"context"
	"fmt"
	"path/filepath"

	"golang.org/x/sync/errgroup"

	"github.com/opendatahub-io/opendatahub-operator/pkg/scoperules"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/manifestdiff"
)

// ResolveManifestNames diffs manifests-config.yaml between base and the
// working tree and returns the components/services the diff is
// attributable to. See manifestdiff.ChangedNames for what "attributable"
// means.
//
// manifestdiff's own names (components, ccmCharts, componentCharts keys,
// and imageOverrides' explicit Component field) are always components.
// Only the source-reference fallback for entries with no Component field
// can resolve to a service.
//
// Returns an error when nothing in the diff is attributable, or when
// platformManifests changed at all (even alongside names that did resolve)
// -- the caller must run the full suite instead of shipping a partial
// scope.
func ResolveManifestNames(ctx context.Context, repoRoot, configFile, base string, patterns *scoperules.CompiledPatterns) (Result, error) {
	var oldCfg, newCfg *config.ManifestsConfig

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		cfg, err := loadManifestsConfigAtRef(gctx, base, configFile)
		if err != nil {
			return fmt.Errorf("loading %s at %s: %w", configFile, base, err)
		}
		oldCfg = cfg
		return nil
	})
	g.Go(func() error {
		cfg, err := config.Load(configFile)
		if err != nil {
			return fmt.Errorf("loading %s: %w", configFile, err)
		}
		newCfg = cfg
		return nil
	})
	if err := g.Wait(); err != nil {
		return Result{}, err
	}

	diff := manifestdiff.ChangedNames(oldCfg, newCfg)
	if diff.PlatformManifestsChanged {
		// Fail closed even if other names in the same diff did resolve --
		// platformManifests can repoint an entire platform's manifests, so
		// a partial scope from the rest of the diff would still miss it.
		return Result{}, fmt.Errorf("%s changed platformManifests, which isn't attributable to a single component", configFile)
	}
	result := Result{Components: diff.Names}

	if len(diff.UnattributedEnvVars) > 0 {
		resolved, err := ResolveUnattributedEnvVars(repoRoot, diff.UnattributedEnvVars, patterns)
		if err != nil {
			// Fail closed: don't ship a partial scope just because some
			// names resolved. Force the full suite instead.
			return Result{}, fmt.Errorf("%s changed one or more imageOverrides entries that could not be attributed to a component: %w",
				configFile, err)
		}
		result.Components = append(result.Components, resolved.Components...)
		result.Services = append(result.Services, resolved.Services...)
	}

	if len(result.Components) == 0 && len(result.Services) == 0 {
		return Result{}, fmt.Errorf("%s changed but nothing in components/ccmCharts/componentCharts/imageOverrides is attributable to it", configFile)
	}

	return result, nil
}

// loadManifestsConfigAtRef reads path's content as of ref via `git show`.
// git's ref:path syntax resolves path relative to the current working
// directory, so this runs from path's own directory and refers to it by
// basename -- that works whether path was given as relative or absolute.
func loadManifestsConfigAtRef(ctx context.Context, ref, path string) (*config.ManifestsConfig, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	out, err := runGit(ctx, dir, "show", fmt.Sprintf("%s:%s", ref, base))
	if err != nil {
		return nil, err
	}

	cfg, err := config.Parse([]byte(out))
	if err != nil {
		return nil, fmt.Errorf("parsing %s at %s: %w", path, ref, err)
	}

	return cfg, nil
}
