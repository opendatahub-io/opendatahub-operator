package downloader

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

const githubURL = "https://github.com"

const (
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorCyan   = "\033[36m"
	colorReset  = "\033[0m"
)

type Options struct {
	ConfigFile   string
	Platform     string
	ManifestsDir string
	ChartsDir    string
	UseLocal     bool
	Overrides    map[string]string // component key → "org:repo:ref:sourcePath"
}

type componentEntry struct {
	Key  string
	Repo config.PlatformRepo
}

func Download(ctx context.Context, opts Options) error {
	cfg, err := config.Load(opts.ConfigFile)
	if err != nil {
		return err
	}

	platform := normalizePlatform(opts.Platform)

	manifests, err := collectEntries(cfg.Components, platform)
	if err != nil {
		return err
	}

	if err := applyOverrides(manifests, opts.Overrides); err != nil {
		return err
	}

	charts, err := mergeCharts(cfg.CCMCharts, cfg.ComponentCharts, platform)
	if err != nil {
		return err
	}

	if len(manifests) == 0 && len(charts) == 0 {
		return fmt.Errorf("no components or charts found for platform %q", platform)
	}

	fmt.Printf("%sDownloading manifests for %s%s%s (%d components, %d charts)%s\n",
		colorGreen, colorYellow, strings.ToUpper(platform), colorGreen, len(manifests), len(charts), colorReset)

	tmpDir, err := os.MkdirTemp("", "odh-manifests.*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := downloadAll(ctx, manifests, opts.ManifestsDir, tmpDir, opts.UseLocal, "manifest"); err != nil {
		return fmt.Errorf("downloading manifests: %w", err)
	}

	if err := downloadAll(ctx, charts, opts.ChartsDir, tmpDir, opts.UseLocal, "chart"); err != nil {
		return fmt.Errorf("downloading charts: %w", err)
	}

	if err := symlinkPlatformManifests(opts.ManifestsDir, opts.ConfigFile, cfg.PlatformManifests); err != nil {
		return fmt.Errorf("symlinking platform manifests: %w", err)
	}

	fmt.Printf("%s✓ All downloads complete%s\n", colorGreen, colorReset)
	return nil
}

func normalizePlatform(p string) string {
	if p == "" || p == "OpenDataHub" {
		return "odh"
	}
	if p == "odh" || p == "rhoai" {
		return p
	}
	return "rhoai"
}

func collectEntries(components map[string]config.Component, platform string) ([]componentEntry, error) {
	var entries []componentEntry
	for key, comp := range components {
		pr := comp.PlatformRepo(platform)
		if pr == nil || pr.Repo == "" || pr.Ref == "" {
			fmt.Printf("  %s⚠ warning%s component %q has no git repo configured for platform %q (add a %q entry to manifests-config.yaml)%s\n",
				colorYellow, colorReset, key, platform, platform, colorReset)
			continue
		}
		entries = append(entries, componentEntry{Key: key, Repo: *pr})
	}
	return entries, nil
}

func mergeCharts(ccm, component map[string]config.Component, platform string) ([]componentEntry, error) {
	merged := make(map[string]config.Component)

	for k, v := range component {
		merged[k] = v
	}
	for k, v := range ccm {
		if _, exists := merged[k]; exists {
			return nil, fmt.Errorf("duplicate chart key %q in CCM and component charts", k)
		}
		merged[k] = v
	}

	return collectEntries(merged, platform)
}

func applyOverrides(entries []componentEntry, overrides map[string]string) error {
	for key, value := range overrides {
		idx := -1
		for i, e := range entries {
			if e.Key == key {
				idx = i
				break
			}
		}
		if idx < 0 {
			keys := make([]string, 0, len(entries))
			for _, e := range entries {
				keys = append(keys, e.Key)
			}
			return fmt.Errorf("override key %q not found in components; available: %v", key, keys)
		}

		parts := strings.SplitN(value, ":", 4)
		if len(parts) != 4 {
			return fmt.Errorf("override value %q must be in format org:repo:ref:sourcePath", value)
		}

		entries[idx].Repo = config.PlatformRepo{
			Repo:       parts[0] + "/" + parts[1],
			Ref:        parts[2],
			SourcePath: parts[3],
		}
		fmt.Printf("  %s⚙ override%s %s%s%s → %s\n", colorCyan, colorReset, colorYellow, key, colorReset, value)
	}
	return nil
}

func downloadAll(ctx context.Context, entries []componentEntry, dstDir, tmpDir string, useLocal bool, kind string) error {
	if len(entries) == 0 {
		return nil
	}

	var completed atomic.Int32
	total := len(entries)
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(8)

	for _, e := range entries {
		e := e
		g.Go(func() error {
			err := downloadComponent(ctx, e, dstDir, tmpDir, useLocal)
			n := completed.Add(1)

			mu.Lock()
			if err != nil {
				fmt.Printf("  %s✗%s %s%-30s%s %sfailed: %v%s\n",
					colorRed, colorReset, colorYellow, e.Key, colorReset, colorRed, err, colorReset)
			} else {
				fmt.Printf("  %s✓%s %s%-30s%s %s[%d/%d]%s\n",
					colorGreen, colorReset, colorYellow, e.Key, colorReset, colorCyan, n, total, colorReset)
			}
			mu.Unlock()

			return err
		})
	}

	return g.Wait()
}

func downloadComponent(ctx context.Context, entry componentEntry, dstDir, tmpDir string, useLocal bool) error {
	repo := entry.Repo
	orgRepo := strings.SplitN(repo.Repo, "/", 2)
	if len(orgRepo) != 2 {
		return fmt.Errorf("invalid repo format %q for component %q", repo.Repo, entry.Key)
	}
	repoName := orgRepo[1]
	targetDir := filepath.Join(dstDir, entry.Key)

	if useLocal {
		localDir := filepath.Join("..", repoName)
		if info, err := os.Stat(localDir); err == nil && info.IsDir() {
			srcDir := filepath.Join(localDir, repo.SourcePath)
			return copyDir(srcDir, targetDir)
		}
	}

	cloneDir := filepath.Join(tmpDir, "clone", entry.Key)
	repoURL := fmt.Sprintf("%s/%s", githubURL, repo.Repo)

	if err := gitFetchRef(ctx, repoURL, repo.Ref, cloneDir); err != nil {
		return fmt.Errorf("%w", err)
	}

	srcDir := filepath.Join(cloneDir, repo.SourcePath)
	return copyDir(srcDir, targetDir)
}

var (
	safeRefPattern = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)
	safeSHAPattern = regexp.MustCompile(`^[a-f0-9]{7,40}$`)
)

func validateRef(ref string) error {
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("ref %q must not start with a dash", ref)
	}
	if !safeRefPattern.MatchString(ref) {
		return fmt.Errorf("ref %q contains invalid characters", ref)
	}
	return nil
}

func gitFetchRef(ctx context.Context, repoURL, ref, dir string) error {
	if err := validateRef(config.ExtractBranch(ref)); err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating clone dir: %w", err)
	}

	if err := git(ctx, dir, "init", "-q"); err != nil {
		return fmt.Errorf("git init: %w", err)
	}

	sha := config.ExtractSHA(ref)
	if sha != "" {
		if !safeSHAPattern.MatchString(sha) {
			return fmt.Errorf("invalid SHA format: %q", sha)
		}
		if err := git(ctx, dir, "remote", "add", "origin", repoURL); err != nil {
			return fmt.Errorf("git remote add: %w", err)
		}
		if err := git(ctx, dir, "fetch", "--depth", "1", "-q", "origin", sha); err != nil {
			return fmt.Errorf("fetch SHA %s: %w", sha[:min(8, len(sha))], err)
		}
		if err := git(ctx, dir, "reset", "-q", "--hard", sha); err != nil {
			return fmt.Errorf("reset to SHA %s: %w", sha[:min(8, len(sha))], err)
		}
		return nil
	}

	// Try tag first, then branch
	if tryFetchRef(ctx, dir, repoURL, "tags", ref) == nil {
		return nil
	}
	if tryFetchRef(ctx, dir, repoURL, "heads", ref) == nil {
		return nil
	}

	return fmt.Errorf("%q is not a valid branch, tag, or commit SHA in %s", ref, repoURL)
}

func tryFetchRef(ctx context.Context, dir, repoURL, refType, ref string) error {
	gitRef := fmt.Sprintf("refs/%s/%s", refType, ref)

	if err := git(ctx, dir, "ls-remote", "--exit-code", repoURL, gitRef); err != nil {
		return err
	}
	if err := git(ctx, dir, "fetch", "-q", "--depth", "1", repoURL, gitRef); err != nil {
		return err
	}
	return git(ctx, dir, "reset", "-q", "--hard", "FETCH_HEAD")
}

func git(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	var stderr bytes.Buffer
	cmd.Stdout = nil
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return err
	}
	return nil
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("creating target dir: %w", err)
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dst, relPath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		return os.WriteFile(targetPath, data, info.Mode())
	})
}

func symlinkPlatformManifests(manifestsDir, configFile string, platformManifests map[string]string) error {
	baseDir := filepath.Dir(configFile)

	return symlinkPlatformManifestsFromBase(manifestsDir, platformManifests, baseDir)
}

func symlinkPlatformManifestsFromBase(manifestsDir string, platformManifests map[string]string, baseDir string) error {
	for key, sourcePath := range platformManifests {
		absSource := filepath.Join(baseDir, sourcePath)
		target := filepath.Join(manifestsDir, key)

		info, err := os.Stat(absSource)
		if err != nil || !info.IsDir() {
			continue
		}

		if _, err := os.Lstat(target); err == nil {
			continue
		}

		fmt.Printf("  %s↗%s %s%-30s%s → %s\n", colorCyan, colorReset, colorYellow, key, colorReset, sourcePath)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.Symlink(absSource, target); err != nil {
			return fmt.Errorf("symlinking %s: %w", key, err)
		}
	}

	return nil
}
