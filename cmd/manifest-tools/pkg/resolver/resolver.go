package resolver

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

type Options struct {
	ConfigFile              string
	RelatedImagesConfigFile string
	ManifestsDir            string
	CSVImportRegistries     []string
	// FetchCSVImages overrides the real HTTP fetch; used in tests only.
	FetchCSVImages func(ctx context.Context, source config.BuildConfigRepo) (map[string]CSVImage, error)
}

type Result struct {
	EnvName  string
	Platform string
	Base     string
	Digest   string
	Source   string // "commit-sha", "Build-Config", "params.env", "params.env+registry", "shaFrom"
}

func Resolve(ctx context.Context, opts Options) ([]Result, error) {
	cfg, err := config.Load(opts.ConfigFile)
	if err != nil {
		return nil, err
	}
	relatedImagesConfig := &config.RelatedImagesConfig{}
	if opts.RelatedImagesConfigFile != "" {
		relatedImagesConfig, err = config.LoadRelatedImagesConfig(opts.RelatedImagesConfigFile)
		if err != nil {
			return nil, err
		}
	}

	envNames := make([]string, 0, len(cfg.ImageOverrides))
	for envName := range cfg.ImageOverrides {
		envNames = append(envNames, envName)
	}
	sort.Strings(envNames)

	// Look up params.env before any digest work or CSV download so a missing
	// key fails without rewriting the file or hitting the network.
	slog.Info("Checking imageOverrides",
		slog.String("file", opts.ConfigFile),
		slog.String("manifestsDir", opts.ManifestsDir),
		slog.Int("count", len(envNames)))
	paramsEnvValue := make(map[string]string, len(envNames))
	for _, envName := range envNames {
		override := cfg.ImageOverrides[envName]
		if err := cfg.CheckImageOverride(envName, override); err != nil {
			return nil, err
		}
		val, err := paramsEnvKeyLookup(opts, override, envName)
		if err != nil {
			return nil, err
		}
		paramsEnvValue[envName] = val
	}
	slog.Info("imageOverrides check passed", slog.Int("count", len(envNames)))

	if err := cfg.CheckBuildConfig(); err != nil {
		return nil, err
	}

	nodeDoc, err := config.LoadNode(opts.ConfigFile)
	if err != nil {
		return nil, err
	}

	var results []Result
	var unresolved []Result

	fetchCSV := opts.FetchCSVImages
	if fetchCSV == nil {
		fetchCSV = FetchCSVRelatedImages
	}
	csvImages := make(map[string]map[string]CSVImage, 2)
	for _, platform := range []string{"odh", "rhoai"} {
		source := cfg.BuildConfig.PlatformRepo(platform)
		images, err := fetchCSV(ctx, *source)
		if err != nil {
			return nil, fmt.Errorf("fetching %s Build-Config CSV: %w", platform, err)
		}
		csvImages[platform] = NormalizeCSVImages(platform, images)
		slog.Info("Fetched CSV related images",
			slog.String("platform", platform), slog.Int("count", len(images)))
	}

	// Track resolved digests so shaFrom can reference freshly-resolved values
	type resolvedImage struct {
		Base   string
		Digest string
	}
	resolved := map[string]map[string]resolvedImage{} // envName -> platform -> image

	// Collect shaFrom entries for second pass
	type shaFromEntry struct {
		envName  string
		platform string
		source   string
	}
	var shaFromDeferred []shaFromEntry

	for _, envName := range envNames {
		override := cfg.ImageOverrides[envName]
		for _, platform := range []string{"odh", "rhoai"} {
			platformCSVImages := csvImages[platform]
			// Entries with source: csv are always updated from CSV
			if override.Source == "csv" {
				// Existing platform blocks are refreshed here. Missing platform
				// blocks are reconciled against the import allowlist below.
				if override.PlatformImage(platform) == nil {
					continue
				}
				if img, ok := platformCSVImages[envName]; ok && config.DigestPattern.MatchString(img.Digest) {
					if err := nodeDoc.SetImageOverrideField(envName, platform, "base", img.Base); err != nil {
						slog.Warn("Failed to set base field", slog.String("error", err.Error()))
					}
					if err := nodeDoc.SetImageOverrideField(envName, platform, "digest", img.Digest); err != nil {
						slog.Warn("Failed to set digest field", slog.String("error", err.Error()))
					}
					results = append(results, Result{envName, platform, img.Base, img.Digest, "csv"})
					slog.Info("Updated from CSV (auto-managed)", slog.String("env", envName), slog.String("platform", platform))
				}
				continue
			}

			comp := cfg.FindComponent(override.Component)
			if comp == nil {
				if override.Component == "" {
					slog.Warn("No component specified for image override", slog.String("env", envName))
				} else {
					slog.Warn("Unknown component, cannot resolve image",
						slog.String("env", envName), slog.String("component", override.Component))
				}
				unresolved = append(unresolved, Result{envName, platform, "", "", "unknown-component"})
				continue
			}
			pr := comp.PlatformRepo(platform)
			if pr == nil {
				slog.Info("No platform repo, skipping", slog.String("env", envName), slog.String("platform", platform), slog.String("component", override.Component))
				continue
			}

			pi := override.PlatformImage(platform)
			if pi != nil && pi.Pinned {
				slog.Info("Pinned, skipping", slog.String("env", envName), slog.String("platform", platform))
				continue
			}

			// Defer shaFrom entries to second pass so source digests are resolved first
			if pi != nil && pi.SHAFrom != "" {
				slog.Info("Deferring shaFrom until source digest is resolved",
					slog.String("env", envName), slog.String("platform", platform), slog.String("shaFrom", pi.SHAFrom))
				shaFromDeferred = append(shaFromDeferred, shaFromEntry{envName, platform, pi.SHAFrom})
				continue
			}

			sha := config.ExtractSHA(pr.Ref)

			// Priority 1: tagTemplate + base → registry lookup
			effectiveBase := override.Base
			if effectiveBase == "" && pi != nil {
				effectiveBase = pi.Base
			}
			if effectiveBase != "" && override.TagTemplate != "" && sha != "" {
				shortSHA := sha
				if len(shortSHA) > 7 {
					shortSHA = shortSHA[:7]
				}
				resolvedTag := strings.ReplaceAll(override.TagTemplate, config.TagSHA, sha)
				resolvedTag = strings.ReplaceAll(resolvedTag, config.TagShortSHA, shortSHA)
				imageRef := effectiveBase + ":" + resolvedTag

				digest, err := ResolveDigestViaRegistry(ctx, imageRef)
				if err == nil && config.DigestPattern.MatchString(digest) {
					if err := nodeDoc.SetImageOverrideField(envName, platform, "base", effectiveBase); err != nil {
						slog.Warn("Failed to set base field", slog.String("error", err.Error()))
					}
					if err := nodeDoc.SetImageOverrideField(envName, platform, "digest", digest); err != nil {
						slog.Warn("Failed to set digest field", slog.String("error", err.Error()))
					}
					results = append(results, Result{envName, platform, effectiveBase, digest, "commit-sha"})
					if resolved[envName] == nil {
						resolved[envName] = map[string]resolvedImage{}
					}
					resolved[envName][platform] = resolvedImage{effectiveBase, digest}
					slog.Info("Resolved via commit-sha",
						slog.String("env", envName), slog.String("platform", platform),
						slog.String("base", effectiveBase), slog.String("digest", digest),
						slog.String("tag", resolvedTag), slog.String("component", override.Component),
						slog.String("sha", shortSHA))
					continue
				}
				slog.Info("Registry lookup failed, falling through", slog.String("env", envName), slog.String("platform", platform), slog.String("imageRef", imageRef))
			}

			// Priority 2 for ODH only: params.env (value cached during the check above).
			// RHOAI params.env files contain ODH defaults and are not a downstream
			// release source.
			if imageRef := paramsEnvValue[envName]; platform == "odh" && imageRef != "" {
				if strings.Contains(imageRef, "@sha256:") {
					pBase, pDigest := SplitImageRef(imageRef)
					if config.DigestPattern.MatchString(pDigest) {
						if err := nodeDoc.SetImageOverrideField(envName, platform, "base", pBase); err != nil {
							slog.Warn("Failed to set base field", slog.String("error", err.Error()))
						}
						if err := nodeDoc.SetImageOverrideField(envName, platform, "digest", pDigest); err != nil {
							slog.Warn("Failed to set digest field", slog.String("error", err.Error()))
						}
						results = append(results, Result{envName, platform, pBase, pDigest, "params.env"})
						if resolved[envName] == nil {
							resolved[envName] = map[string]resolvedImage{}
						}
						resolved[envName][platform] = resolvedImage{pBase, pDigest}
						slog.Info("Resolved via params.env", slog.String("env", envName), slog.String("platform", platform), slog.String("base", pBase), slog.String("digest", pDigest))
						continue
					}
				}
				// Tagged image → registry lookup
				digest, err := ResolveDigestViaRegistry(ctx, imageRef)
				if err == nil && config.DigestPattern.MatchString(digest) {
					pBase := imageBaseWithoutTag(imageRef)
					if err := nodeDoc.SetImageOverrideField(envName, platform, "base", pBase); err != nil {
						slog.Warn("Failed to set base field", slog.String("error", err.Error()))
					}
					if err := nodeDoc.SetImageOverrideField(envName, platform, "digest", digest); err != nil {
						slog.Warn("Failed to set digest field", slog.String("error", err.Error()))
					}
					results = append(results, Result{envName, platform, pBase, digest, "params.env+registry"})
					if resolved[envName] == nil {
						resolved[envName] = map[string]resolvedImage{}
					}
					resolved[envName][platform] = resolvedImage{pBase, digest}
					slog.Info("Resolved via params.env+registry", slog.String("env", envName), slog.String("platform", platform), slog.String("base", pBase), slog.String("digest", digest))
					continue
				}
			}

			// Priority 3: CSV fallback
			if platformCSVImages != nil {
				if img, ok := platformCSVImages[envName]; ok && config.DigestPattern.MatchString(img.Digest) {
					if sha != "" {
						slog.Warn("Image for commit SHA not found in registry, falling back to CSV",
							slog.String("env", envName), slog.String("platform", platform))
					}
					if err := nodeDoc.SetImageOverrideField(envName, platform, "base", img.Base); err != nil {
						slog.Warn("Failed to set base field", slog.String("error", err.Error()))
					}
					if err := nodeDoc.SetImageOverrideField(envName, platform, "digest", img.Digest); err != nil {
						slog.Warn("Failed to set digest field", slog.String("error", err.Error()))
					}
					results = append(results, Result{envName, platform, img.Base, img.Digest, "csv"})
					if resolved[envName] == nil {
						resolved[envName] = map[string]resolvedImage{}
					}
					resolved[envName][platform] = resolvedImage{img.Base, img.Digest}
					slog.Info("Resolved via CSV", slog.String("env", envName), slog.String("platform", platform), slog.String("base", img.Base), slog.String("digest", img.Digest))
					continue
				}
			}

			unresolved = append(unresolved, Result{envName, platform, "", "", "unresolved"})
		}
	}

	// Second pass: resolve shaFrom entries using freshly-resolved digests
	for _, entry := range shaFromDeferred {
		var src resolvedImage
		if platforms, ok := resolved[entry.envName]; ok {
			if img, ok := platforms[entry.source]; ok {
				src = img
			}
		}
		if src.Digest == "" {
			// Fall back to original config value
			override := cfg.ImageOverrides[entry.envName]
			if sourcePi := override.PlatformImage(entry.source); sourcePi != nil && sourcePi.Digest != "" {
				src = resolvedImage{sourcePi.Base, sourcePi.Digest}
			}
		}
		if src.Digest == "" {
			slog.Warn("shaFrom has no digest available", slog.String("env", entry.envName), slog.String("platform", entry.platform), slog.String("shaFrom", entry.source))
			unresolved = append(unresolved, Result{entry.envName, entry.platform, "", "", "unresolved"})
			continue
		}
		if err := nodeDoc.SetImageOverrideField(entry.envName, entry.platform, "base", src.Base); err != nil {
			slog.Warn("Failed to set base field", slog.String("error", err.Error()))
		}
		if err := nodeDoc.SetImageOverrideField(entry.envName, entry.platform, "digest", src.Digest); err != nil {
			slog.Warn("Failed to set digest field", slog.String("error", err.Error()))
		}
		results = append(results, Result{entry.envName, entry.platform, src.Base, src.Digest, "shaFrom"})
		slog.Info("Copied from shaFrom source", slog.String("env", entry.envName), slog.String("platform", entry.platform), slog.String("source", entry.source))
	}

	// Third pass: import allowlisted CSV images not already in the original
	// config. Upsert lets an env var receive both platform images in one pass
	// when both image bases match the allowlist.
	for _, platform := range []string{"odh", "rhoai"} {
		platformCSVImages := csvImages[platform]
		csvEnvNames := make([]string, 0, len(platformCSVImages))
		for envName := range platformCSVImages {
			csvEnvNames = append(csvEnvNames, envName)
		}
		sort.Strings(csvEnvNames)

		for _, envName := range csvEnvNames {
			if _, exists := cfg.ImageOverrides[envName]; exists {
				continue
			}
			if relatedImagesConfig.IsPlatformException(platform, envName) {
				continue
			}
			img := platformCSVImages[envName]
			if !config.DigestPattern.MatchString(img.Digest) {
				continue
			}
			if len(opts.CSVImportRegistries) > 0 && !matchesRegistry(img.Base, opts.CSVImportRegistries) {
				continue
			}
			if err := nodeDoc.UpsertCSVImageOverride(envName, platform, img.Base, img.Digest); err != nil {
				slog.Warn("Failed to add CSV image override", slog.String("env", envName), slog.String("platform", platform), slog.String("error", err.Error()))
				continue
			}
			results = append(results, Result{envName, platform, img.Base, img.Digest, "csv-imported"})
			slog.Info("Imported from CSV", slog.String("env", envName), slog.String("platform", platform), slog.String("base", img.Base))
		}
	}

	// Reconcile platform membership for source: csv rows. Remove stale
	// configured platforms, add newly-published allowlisted platforms, and
	// remove the whole row when no platform image remains.
	for _, envName := range envNames {
		override := cfg.ImageOverrides[envName]
		if override.Source != "csv" {
			continue
		}
		remaining := 0
		for _, platform := range []string{"odh", "rhoai"} {
			img, present := csvImages[platform][envName]
			present = present && config.DigestPattern.MatchString(img.Digest)
			if override.PlatformImage(platform) == nil {
				if relatedImagesConfig.IsPlatformException(platform, envName) {
					continue
				}
				if !present || (len(opts.CSVImportRegistries) > 0 && !matchesRegistry(img.Base, opts.CSVImportRegistries)) {
					continue
				}
				if err := nodeDoc.UpsertCSVImageOverride(envName, platform, img.Base, img.Digest); err != nil {
					slog.Warn("Failed to add CSV platform", slog.String("env", envName), slog.String("platform", platform), slog.String("error", err.Error()))
					continue
				}
				results = append(results, Result{envName, platform, img.Base, img.Digest, "csv-imported"})
				slog.Info("Added newly-published CSV platform", slog.String("env", envName), slog.String("platform", platform), slog.String("base", img.Base))
				remaining++
				continue
			}
			if present {
				remaining++
				continue
			}
			if err := nodeDoc.RemoveImageOverridePlatform(envName, platform); err != nil {
				slog.Warn("Failed to remove stale CSV platform", slog.String("env", envName), slog.String("platform", platform), slog.String("error", err.Error()))
				continue
			}
			slog.Info("Removed stale CSV platform", slog.String("env", envName), slog.String("platform", platform))
		}
		if remaining == 0 {
			if err := nodeDoc.RemoveImageOverride(envName); err != nil {
				slog.Warn("Failed to remove stale CSV entry", slog.String("env", envName), slog.String("error", err.Error()))
				continue
			}
			slog.Info("Removed stale CSV entry", slog.String("env", envName))
		}
	}

	if len(unresolved) > 0 {
		printSummary(results, unresolved)
		for _, u := range unresolved {
			if u.Source == "unknown-component" {
				return results, fmt.Errorf("unknown component(s) found in imageOverrides; check logs above")
			}
		}
		slog.Warn("Some images could not be resolved, skipping", slog.Int("count", len(unresolved)))
	}

	if err := nodeDoc.Save(opts.ConfigFile); err != nil {
		return nil, fmt.Errorf("saving config: %w", err)
	}

	slog.Info("Digests updated", slog.String("file", opts.ConfigFile))
	printSummary(results, unresolved)
	return results, nil
}

func printSummary(results []Result, unresolved []Result) {
	const (
		reset   = "\033[0m"
		bold    = "\033[1m"
		green   = "\033[32m"
		red     = "\033[31m"
		cyan    = "\033[36m"
		yellow  = "\033[33m"
		magenta = "\033[35m"
		dim     = "\033[2m"
	)

	blue := "\033[34m"
	sourceOrder := []string{"commit-sha", "params.env", "params.env+registry", "csv", "shaFrom", "csv-imported"}
	sourceColor := map[string]string{
		"commit-sha":          green,
		"params.env":          yellow,
		"params.env+registry": yellow,
		"csv":                 cyan,
		"shaFrom":             magenta,
		"csv-imported":        blue,
	}

	grouped := map[string][]Result{}
	for _, r := range results {
		grouped[r.Source] = append(grouped[r.Source], r)
	}

	fmt.Fprintf(os.Stderr, "\n%s%s=== Digest Resolution Summary ===%s\n", bold, green, reset)

	for _, src := range sourceOrder {
		entries, ok := grouped[src]
		if !ok {
			continue
		}
		color := sourceColor[src]
		fmt.Fprintf(os.Stderr, "\n%s%s▸ %s%s %s(%d)%s\n", bold, color, src, reset, dim, len(entries), reset)
		for _, r := range entries {
			shortDigest := r.Digest
			if len(shortDigest) > 19 {
				shortDigest = shortDigest[:19] + "…"
			}
			fmt.Fprintf(os.Stderr, "  %s%-5s%s %s → %s%s%s\n", dim, r.Platform, reset, r.EnvName, dim, shortDigest, reset)
		}
	}

	if len(unresolved) > 0 {
		fmt.Fprintf(os.Stderr, "\n%s%s▸ unresolved (warning: these images were skipped)%s %s(%d)%s\n", bold, red, reset, dim, len(unresolved), reset)
		for _, r := range unresolved {
			fmt.Fprintf(os.Stderr, "  %s%-5s%s %s\n", dim, r.Platform, reset, r.EnvName)
		}
	}

	fmt.Fprintf(os.Stderr, "\n%sTotal: %d images resolved%s\n\n", bold, len(results), reset)
}

func SplitImageRef(ref string) (base, digest string) {
	idx := strings.LastIndex(ref, "@")
	if idx < 0 {
		return ref, ""
	}
	return ref[:idx], ref[idx+1:]
}

// imageBaseWithoutTag returns the image name without a tag. The tag is the
// colon suffix after the last slash, so a registry host:port is kept.
func imageBaseWithoutTag(ref string) string {
	if i := strings.LastIndex(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	slash := strings.LastIndex(ref, "/")
	colon := strings.LastIndex(ref, ":")
	if colon > slash {
		return ref[:colon]
	}
	return ref
}

func lookupParamsEnvKey(dir, key string) (value string, found bool, err error) {
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "params.env" {
			return err
		}
		val, lookupErr := ReadParamsEnvKey(path, key)
		if lookupErr == nil {
			value = val
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	if found {
		return value, true, nil
	}
	if err != nil {
		return "", false, err
	}
	return "", false, nil
}

func paramsEnvKeyLookup(opts Options, override config.ImageOverride, envName string) (string, error) {
	if override.ParamsEnvKey == "" {
		return "", nil
	}
	if opts.ManifestsDir == "" {
		return "", fmt.Errorf("imageOverrides.%s.paramsEnvKey: manifests directory is required to look up %q", envName, override.ParamsEnvKey)
	}
	componentDir := filepath.Join(opts.ManifestsDir, override.Component)
	val, found, err := lookupParamsEnvKey(componentDir, override.ParamsEnvKey)
	if err != nil {
		return "", fmt.Errorf("imageOverrides.%s.paramsEnvKey: looking up %q under %s: %w", envName, override.ParamsEnvKey, componentDir, err)
	}
	if !found {
		return "", fmt.Errorf("imageOverrides.%s.paramsEnvKey: %q not found in any params.env under %s", envName, override.ParamsEnvKey, componentDir)
	}
	slog.Info("Found paramsEnvKey",
		slog.String("env", envName),
		slog.String("component", override.Component),
		slog.String("paramsEnvKey", override.ParamsEnvKey),
		slog.String("dir", componentDir))
	return val, nil
}

func matchesRegistry(imageBase string, registries []string) bool {
	for _, reg := range registries {
		if strings.HasPrefix(imageBase, reg) {
			return true
		}
	}
	return false
}

func ReadParamsEnvKey(path, key string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	prefix := key + "="
	for scanner.Scan() {
		line := scanner.Text()
		if val, ok := strings.CutPrefix(line, prefix); ok {
			return val, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return "", fmt.Errorf("key %q not found in %s", key, path)
}
