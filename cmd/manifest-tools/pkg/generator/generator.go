package generator

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

var unsafeBasePattern = regexp.MustCompile(`['";\x60$|]`)

type Options struct {
	ConfigFile string
	Platform   string
	OutputFile string
}

func Generate(opts Options) error {
	cfg, err := config.Load(opts.ConfigFile)
	if err != nil {
		return err
	}

	platform := opts.Platform
	if platform == "OpenDataHub" {
		platform = "odh"
	} else if platform != "odh" && platform != "rhoai" {
		platform = "rhoai"
	}

	if len(cfg.ImageOverrides) == 0 {
		slog.Info("No image overrides defined")
		return nil
	}

	f, err := os.Create(opts.OutputFile)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer f.Close()

	envNames := make([]string, 0, len(cfg.ImageOverrides))
	for envName := range cfg.ImageOverrides {
		envNames = append(envNames, envName)
	}
	sort.Strings(envNames)

	count := 0
	for _, envName := range envNames {
		override := cfg.ImageOverrides[envName]
		pi := override.PlatformImage(platform)
		if pi == nil {
			continue
		}

		if pi.Digest == "" {
			slog.Warn("No digest, skipping (run 'make resolve-image-digests')", slog.String("env", envName), slog.String("platform", platform))
			continue
		}

		if !config.DigestPattern.MatchString(pi.Digest) {
			slog.Warn("Invalid digest, skipping", slog.String("env", envName), slog.String("platform", platform), slog.String("digest", pi.Digest))
			continue
		}

		base := pi.Base
		if base == "" {
			slog.Warn("No base, skipping", slog.String("env", envName), slog.String("platform", platform))
			continue
		}

		if unsafeBasePattern.MatchString(base) {
			slog.Warn("Base contains unsafe characters, skipping", slog.String("env", envName), slog.String("platform", platform), slog.String("base", base))
			continue
		}

		if !strings.HasPrefix(envName, "RELATED_IMAGE_") {
			slog.Warn("Env name does not start with RELATED_IMAGE_, skipping", slog.String("env", envName))
			continue
		}

		line := fmt.Sprintf("%s=%s@%s", envName, base, pi.Digest)
		if _, err := fmt.Fprintln(f, line); err != nil {
			return fmt.Errorf("writing override: %w", err)
		}
		slog.Info("Override", slog.String("line", line))
		count++
	}

	slog.Info("Image overrides written", slog.String("file", opts.OutputFile), slog.Int("entries", count))
	return nil
}
