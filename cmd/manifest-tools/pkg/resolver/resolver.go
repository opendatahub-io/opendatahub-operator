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
	ConfigFile          string
	ManifestsDir        string
	CSVImportRegistries []string
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

	nodeDoc, err := config.LoadNode(opts.ConfigFile)
	if err != nil {
		return nil, err
	}

	var results []Result
	var unresolved []Result
	var csvStale []string

	csvImages, err := FetchCSVRelatedImages(ctx)
	if err != nil {
		slog.Warn("Failed to fetch CSV related images, csv fallback disabled", slog.String("error", err.Error()))
	} else {
		slog.Info("Fetched CSV related images", slog.Int("count", len(csvImages)))
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

	envNames := make([]string, 0, len(cfg.ImageOverrides))
	for envName := range cfg.ImageOverrides {
		envNames = append(envNames, envName)
	}
	sort.Strings(envNames)

	for _, envName := range envNames {
		override := cfg.ImageOverrides[envName]
		for _, platform := range []string{"odh", "rhoai"} {
			// Entries with source: csv are always updated from CSV
			if override.Source == "csv" {
				if csvImages != nil {
					if img, ok := csvImages[envName]; ok && config.DigestPattern.MatchString(img.Digest) {
						if len(opts.CSVImportRegistries) > 0 && !matchesRegistry(img.Base, opts.CSVImportRegistries) {
							slog.Info("CSV entry skipped (registry not allowed)", slog.String("env", envName), slog.String("platform", platform))
							continue
						}
						if err := nodeDoc.SetImageOverrideField(envName, platform, "base", img.Base); err != nil {
							slog.Warn("Failed to set base field", slog.String("error", err.Error()))
						}
						if err := nodeDoc.SetImageOverrideField(envName, platform, "digest", img.Digest); err != nil {
							slog.Warn("Failed to set digest field", slog.String("error", err.Error()))
						}
						results = append(results, Result{envName, platform, img.Base, img.Digest, "csv"})
						slog.Info("Updated from CSV (auto-managed)", slog.String("env", envName), slog.String("platform", platform))
					} else if platform == "odh" {
						csvStale = append(csvStale, envName)
					}
				}
				continue
			}

			comp := cfg.FindComponent(override.Component)
			if comp == nil {
				continue
			}
			pr := comp.PlatformRepo(platform)
			if pr == nil {
				continue
			}

			pi := override.PlatformImage(platform)
			if pi != nil && pi.Pinned {
				slog.Info("Pinned, skipping", slog.String("env", envName), slog.String("platform", platform))
				continue
			}

			// Defer shaFrom entries to second pass so source digests are resolved first
			if pi != nil && pi.SHAFrom != "" {
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
				resolvedTag := strings.ReplaceAll(override.TagTemplate, "{SHA}", sha)
				resolvedTag = strings.ReplaceAll(resolvedTag, "{SHORT_SHA}", shortSHA)
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

			// Priority 2: params.env
			if override.ParamsEnvKey != "" && override.Component != "" {
				componentDir := fmt.Sprintf("%s/%s", opts.ManifestsDir, override.Component)
				if imageRef, err := FindParamsEnvKey(componentDir, override.ParamsEnvKey); err == nil && imageRef != "" {
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
						pBase, _, _ := strings.Cut(imageRef, ":")
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
			}

			slog.Warn("No source found via commit-sha or params.env", slog.String("env", envName), slog.String("platform", platform))

			// Priority 3: ODH-Build-Config CSV
			if csvImages != nil {
				if img, ok := csvImages[envName]; ok && config.DigestPattern.MatchString(img.Digest) {
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

	// Third pass: import CSV entries not already in manifests-config
	if csvImages != nil {
		csvEnvNames := make([]string, 0, len(csvImages))
		for envName := range csvImages {
			csvEnvNames = append(csvEnvNames, envName)
		}
		sort.Strings(csvEnvNames)

		for _, envName := range csvEnvNames {
			if _, exists := cfg.ImageOverrides[envName]; exists {
				continue
			}
			img := csvImages[envName]
			if !config.DigestPattern.MatchString(img.Digest) {
				continue
			}
			if len(opts.CSVImportRegistries) > 0 && !matchesRegistry(img.Base, opts.CSVImportRegistries) {
				continue
			}
			if err := nodeDoc.AddImageOverride(envName, "odh", img.Base, img.Digest); err != nil {
				slog.Warn("Failed to add CSV image override", slog.String("env", envName), slog.String("error", err.Error()))
				continue
			}
			results = append(results, Result{envName, "odh", img.Base, img.Digest, "csv-imported"})
			slog.Info("Imported from CSV", slog.String("env", envName), slog.String("base", img.Base))
		}
	}

	// Remove source: csv entries no longer present in CSV
	for _, envName := range csvStale {
		if err := nodeDoc.RemoveImageOverride(envName); err != nil {
			slog.Warn("Failed to remove stale CSV entry", slog.String("env", envName), slog.String("error", err.Error()))
			continue
		}
		slog.Info("Removed stale CSV entry", slog.String("env", envName))
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
		fmt.Fprintf(os.Stderr, "\n%s%s▸ unresolved%s %s(%d)%s\n", bold, red, reset, dim, len(unresolved), reset)
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

// FindParamsEnvKey searches for params.env files recursively under dir
// and returns the value for the given key from the first file that contains it.
func FindParamsEnvKey(dir, key string) (string, error) {
	var result string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "params.env" {
			return err
		}
		val, err := ReadParamsEnvKey(path, key)
		if err == nil {
			result = val
			return filepath.SkipAll
		}
		return nil
	})
	if result != "" {
		return result, nil
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("key %q not found in any params.env under %s", key, dir)
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
