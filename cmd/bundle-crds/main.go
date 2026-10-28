// Command bundle-crds copies bundle-owned primary module CRDs from
// get-manifests output into the bundle-only Kustomize overlay.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/builtin"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

type source struct {
	name  string
	gvk   schema.GroupVersionKind
	roots []string
}

type candidate struct {
	path string
	data []byte
	rank int
}

const emptyKustomization = "# Bundle generation fills this overlay from get-manifests before rendering.\n" +
	"# Keep it empty so source-only Kustomize and lint checks can build the CSV.\n" +
	"apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources: []\n"

func main() {
	platform := flag.String("platform", "OpenDataHub", "manifest source platform: OpenDataHub or rhoai")
	outputDir := flag.String("output-dir", "config/crd/components", "generated bundle CRD overlay")
	reset := flag.Bool("reset", false, "restore the empty source-only overlay after bundle generation")
	flag.Parse()
	if *reset {
		if err := restoreEmptyOverlay(*outputDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	release := cluster.OpenDataHub
	switch *platform {
	case "OpenDataHub":
	case "rhoai":
		release = cluster.SelfManagedRhoai
	default:
		fmt.Fprintln(os.Stderr, "unsupported platform:", *platform)
		os.Exit(1)
	}

	platformContext := &modules.PlatformContext{
		Release:           common.Release{Name: release},
		ManifestsBasePath: filepath.Join("opt", "manifests"),
		ChartsBasePath:    filepath.Join("opt", "charts"),
	}
	sources := make([]source, 0, len(builtin.Registrations()))
	for _, registration := range builtin.Registrations() {
		if registration.BundleCRDName == "" {
			continue
		}
		manifests := registration.Handler.GetOperatorManifests(platformContext)
		roots := make([]string, 0, len(manifests.Manifests)+len(manifests.HelmCharts))
		for _, manifest := range manifests.Manifests {
			// Some handlers add shared platform manifests beside their module
			// source. The primary module CRD must come from the module source.
			if filepath.Clean(manifest.Path) == filepath.Clean(platformContext.ManifestsBasePath) {
				continue
			}
			roots = append(roots, manifest.Path)
		}
		for _, chart := range manifests.HelmCharts {
			roots = append(roots, chart.Chart)
		}
		sources = append(sources, source{
			name:  registration.BundleCRDName,
			gvk:   registration.Handler.GetGVK(),
			roots: roots,
		})
	}

	if err := generate(*outputDir, sources); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func restoreEmptyOverlay(outputDir string) error {
	if err := os.RemoveAll(outputDir); err != nil {
		return fmt.Errorf("removing generated bundle CRDs: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating empty bundle CRD overlay: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "kustomization.yaml"), []byte(emptyKustomization), 0o600); err != nil {
		return fmt.Errorf("writing empty bundle CRD overlay: %w", err)
	}
	return nil
}

func generate(outputDir string, sources []source) error {
	parent := filepath.Dir(outputDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("creating bundle CRD parent: %w", err)
	}
	tempDir, err := os.MkdirTemp(parent, ".components-")
	if err != nil {
		return fmt.Errorf("creating temporary bundle CRD overlay: %w", err)
	}
	defer os.RemoveAll(tempDir)

	resources := make([]string, 0, len(sources))
	seen := make(map[string]bool, len(sources))
	for _, src := range sources {
		if src.name == "" || seen[src.name] {
			return fmt.Errorf("missing or duplicate module CRD name %q", src.name)
		}
		seen[src.name] = true
		selected, err := findCRD(src)
		if err != nil {
			return err
		}
		filename := src.name + ".yaml"
		if err := os.WriteFile(filepath.Join(tempDir, filename), selected.data, 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", filename, err)
		}
		resources = append(resources, "- "+filename+"\n")
	}
	kustomization := "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n" +
		strings.Join(resources, "")
	if err := os.WriteFile(filepath.Join(tempDir, "kustomization.yaml"), []byte(kustomization), 0o600); err != nil {
		return fmt.Errorf("writing bundle CRD kustomization: %w", err)
	}
	if err := os.RemoveAll(outputDir); err != nil {
		return fmt.Errorf("removing old bundle CRD overlay: %w", err)
	}
	if err := os.Rename(tempDir, outputDir); err != nil {
		return fmt.Errorf("installing bundle CRD overlay: %w", err)
	}
	return nil
}

func findCRD(src source) (candidate, error) {
	if len(src.roots) == 0 {
		return candidate{}, fmt.Errorf("module %s has no manifest source", src.name)
	}
	var candidates []candidate
	for _, root := range src.roots {
		rootFS, err := os.OpenRoot(root)
		if err != nil {
			return candidate{}, fmt.Errorf("module %s manifest source %s: %w (run make get-manifests)", src.name, root, err)
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
				return nil
			}
			relPath, err := filepath.Rel(root, path)
			if err != nil {
				return fmt.Errorf("resolving %s in %s: %w", path, root, err)
			}
			data, err := rootFS.ReadFile(relPath)
			if err != nil {
				return fmt.Errorf("reading %s in %s: %w", relPath, root, err)
			}
			if !bytes.Contains(data, []byte("CustomResourceDefinition")) {
				return nil
			}
			reader := k8syaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
			for {
				doc, readErr := reader.Read()
				if readErr == io.EOF {
					return nil
				}
				if readErr != nil {
					return fmt.Errorf("reading YAML documents in %s: %w", path, readErr)
				}
				if !isCRDDocument(doc) || bytes.Contains(doc, []byte("{{")) {
					continue // Ignore unrelated YAML and unresolved Helm templates.
				}
				var crd apiextv1.CustomResourceDefinition
				if err := yaml.Unmarshal(doc, &crd); err != nil {
					return fmt.Errorf("parsing CRD in %s: %w", path, err)
				}
				if crd.Kind != "CustomResourceDefinition" || crd.Name != src.name ||
					crd.Spec.Group != src.gvk.Group || crd.Spec.Names.Kind != src.gvk.Kind {
					continue
				}
				served := false
				for _, version := range crd.Spec.Versions {
					if version.Name == src.gvk.Version && version.Served {
						served = true
						break
					}
				}
				if !served {
					return fmt.Errorf("module CRD %s in %s does not serve %s", src.name, path, src.gvk.Version)
				}
				candidates = append(candidates, candidate{path: path, data: doc, rank: sourceRank(path)})
			}
		})
		closeErr := rootFS.Close()
		if err != nil {
			return candidate{}, fmt.Errorf("searching module CRD %s in %s: %w", src.name, root, err)
		}
		if closeErr != nil {
			return candidate{}, fmt.Errorf("closing manifest source %s: %w", root, closeErr)
		}
	}
	if len(candidates) == 0 {
		return candidate{}, fmt.Errorf("module CRD %s (%v) missing from %v", src.name, src.gvk, src.roots)
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		if a.rank != b.rank {
			return b.rank - a.rank
		}
		return strings.Compare(a.path, b.path)
	})
	for _, other := range candidates[1:] {
		if other.rank == candidates[0].rank && !bytes.Equal(other.data, candidates[0].data) {
			return candidate{}, fmt.Errorf("ambiguous module CRD %s: %s and %s", src.name, candidates[0].path, other.path)
		}
	}
	return candidates[0], nil
}

// isCRDDocument inspects the top-level kind without parsing unrelated YAML,
// which can include malformed templates and comments mentioning CRDs.
func isCRDDocument(doc []byte) bool {
	lines := bytes.Split(doc, []byte("\n"))
	minIndent := len(doc)
	for _, line := range lines {
		trimmed := bytes.TrimLeft(line, " \t")
		if len(trimmed) == 0 || trimmed[0] == '#' {
			continue
		}
		minIndent = min(minIndent, len(line)-len(trimmed))
	}
	for _, line := range lines {
		trimmed := bytes.TrimLeft(line, " \t")
		if len(trimmed) == 0 || trimmed[0] == '#' || len(line)-len(trimmed) != minIndent {
			continue
		}
		key, value, ok := bytes.Cut(trimmed, []byte(":"))
		key = bytes.Trim(bytes.TrimSpace(key), "\"'")
		if !ok || !bytes.Equal(key, []byte("kind")) {
			continue
		}
		value, _, _ = bytes.Cut(value, []byte("#"))
		value = bytes.Trim(bytes.TrimSpace(value), "\"'")
		if bytes.Equal(value, []byte("CustomResourceDefinition")) {
			return true
		}
	}
	return false
}

func sourceRank(path string) int {
	path = filepath.ToSlash(path)
	switch {
	case strings.Contains(path, "/crds/"):
		return 3
	case strings.Contains(path, "/crd/bases/"):
		return 2
	case strings.Contains(path, "/crd/"):
		return 1
	default:
		return 0
	}
}
