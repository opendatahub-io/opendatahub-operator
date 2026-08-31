package modules_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	modulebuiltin "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/builtin"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/manifests/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

var allowedChartResourceKinds = map[string]bool{
	"Deployment":                     true,
	"Service":                        true,
	"ServiceAccount":                 true,
	"ClusterRole":                    true,
	"ClusterRoleBinding":             true,
	"Role":                           true,
	"RoleBinding":                    true,
	"ConfigMap":                      true,
	"CustomResourceDefinition":       true,
	"MutatingWebhookConfiguration":   true,
	"ValidatingWebhookConfiguration": true,
	"Issuer":                         true,
	"Certificate":                    true,
}

func TestModuleChartCompliance(t *testing.T) {
	chartsRoot := requireAssetsRoot(t, "DEFAULT_CHARTS_PATH", filepath.Join("opt", "charts"))
	reg := requireBuiltinRegistry(t)

	platform := &modules.PlatformContext{
		ApplicationsNamespace: "test-ns",
		ChartsBasePath:        chartsRoot,
	}

	testedCount := 0
	err := reg.ForAll(func(handler modules.ModuleHandler, _ bool) error {
		manifests := handler.GetOperatorManifests(platform)
		for _, chartInfo := range manifests.HelmCharts {
			if _, err := os.Stat(chartInfo.Chart); os.IsNotExist(err) {
				t.Fatalf("chart directory %s not found for module %s (run make get-manifests first)",
					chartInfo.Chart, handler.GetName())
			}

			testedCount++
			name := handler.GetName()

			t.Run(name, func(t *testing.T) {
				g := NewWithT(t)

				renderer, err := helmRenderer.New([]helmRenderer.Source{{
					Chart:       chartInfo.Chart,
					ReleaseName: chartInfo.ReleaseName,
					Values:      chartInfo.Values,
				}})
				g.Expect(err).ShouldNot(HaveOccurred(), "failed to create helm renderer for %s", name)

				resources, err := renderer.Process(t.Context(), nil)
				g.Expect(err).ShouldNot(HaveOccurred(), "failed to render chart for %s", name)
				g.Expect(resources).ShouldNot(BeEmpty(), "chart %s rendered zero resources", name)

				assertOperatorManifestCompliance(g, name, toRenderedResources(resources))
			})
		}

		return nil
	})
	if err != nil {
		t.Fatalf("iterating built-in module handlers: %v", err)
	}
	if testedCount == 0 {
		t.Fatal("no module handlers have Helm charts to test")
	}
}

func TestModuleManifestCompliance(t *testing.T) {
	manifestsRoot := requireAssetsRoot(t, "DEFAULT_MANIFESTS_PATH", filepath.Join("opt", "manifests"))
	reg := requireBuiltinRegistry(t)

	platform := &modules.PlatformContext{
		ApplicationsNamespace: "test-ns",
		ManifestsBasePath:     manifestsRoot,
		Release:               common.Release{Name: cluster.OpenDataHub},
	}

	testedCount := 0
	err := reg.ForAll(func(handler modules.ModuleHandler, _ bool) error {
		manifests := handler.GetOperatorManifests(platform)
		for manifestIdx, manifestInfo := range manifests.Manifests {
			if manifestInfo.ContextDir != "" && manifestInfo.Path == platform.ManifestsBasePath {
				continue
			}

			manifestPath := manifestInfo.String()
			if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
				t.Fatalf("manifest directory %s not found for module %s (run make get-manifests first)",
					manifestPath, handler.GetName())
			}

			testedCount++
			name := manifestSubtestName(handler.GetName(), manifestInfo, manifestIdx, len(manifests.Manifests))

			t.Run(name, func(t *testing.T) {
				g := NewWithT(t)

				ns := platform.ApplicationsNamespace
				if manifestInfo.Namespace != "" {
					ns = manifestInfo.Namespace
				}

				var renderOpts []kustomize.RenderOptsFn
				if ns != "" {
					renderOpts = append(renderOpts, kustomize.WithNamespace(ns))
				}

				resources, err := kustomize.NewEngine().Render(manifestPath, renderOpts...)
				g.Expect(err).ShouldNot(HaveOccurred(), "failed to render manifest for %s", name)
				g.Expect(resources).ShouldNot(BeEmpty(), "manifest %s rendered zero resources", name)

				assertOperatorManifestCompliance(g, name, toRenderedResources(resources))
			})
		}

		return nil
	})
	if err != nil {
		t.Fatalf("iterating built-in module handlers: %v", err)
	}
	if testedCount == 0 {
		t.Fatal("no module handlers have Kustomize manifests to test")
	}
}

func requireAssetsRoot(t *testing.T, envVar, defaultRelPath string) string {
	t.Helper()

	root := os.Getenv(envVar)
	if root == "" {
		projectRoot, err := envtestutil.FindProjectRoot()
		if err != nil {
			t.Fatalf("failed to resolve project root: %v", err)
		}
		root = filepath.Join(projectRoot, defaultRelPath)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("failed to resolve assets root %s: %v", root, err)
	}
	if _, err := os.Stat(absRoot); os.IsNotExist(err) {
		t.Skipf("assets root %s not found (run make get-manifests first)", absRoot)
	}

	return absRoot
}

func requireBuiltinRegistry(t *testing.T) *modules.Registry {
	t.Helper()

	reg := &modules.Registry{}
	modulebuiltin.Register(reg)
	if !reg.HasEntries() {
		t.Skip("no built-in module handlers registered")
	}

	return reg
}

type renderedResource struct {
	kind string
	name string
}

func toRenderedResources(resources []unstructured.Unstructured) []renderedResource {
	out := make([]renderedResource, len(resources))
	for i, res := range resources {
		out[i] = renderedResource{kind: res.GetKind(), name: res.GetName()}
	}
	return out
}

func assertOperatorManifestCompliance(g Gomega, source string, resources []renderedResource) {
	deploymentCount := 0
	for _, res := range resources {
		g.Expect(allowedChartResourceKinds).Should(HaveKey(res.kind),
			"%s contains disallowed resource kind %q (name: %s)",
			source, res.kind, res.name)

		if res.kind == "Deployment" {
			deploymentCount++
		}
	}

	g.Expect(deploymentCount).Should(Equal(1),
		"%s should contain exactly 1 Deployment, found %d",
		source, deploymentCount)
}

func manifestSubtestName(moduleName string, manifestInfo types.ManifestInfo, idx, total int) string {
	switch {
	case manifestInfo.ContextDir != "":
		return fmt.Sprintf("%s/%s", moduleName, manifestInfo.ContextDir)
	case manifestInfo.SourcePath != "":
		return fmt.Sprintf("%s/%s", moduleName, filepath.Base(manifestInfo.SourcePath))
	case total > 1:
		return fmt.Sprintf("%s/manifest-%d", moduleName, idx)
	default:
		return moduleName
	}
}
