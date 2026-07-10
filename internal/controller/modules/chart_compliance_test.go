package modules_test

import (
	"os"
	"path/filepath"
	"testing"

	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/mcplifecycleoperator"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/manifests/kustomize"

	. "github.com/onsi/gomega"
)

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

var allowedKinds = map[string]bool{
	"Deployment":               true,
	"ServiceAccount":           true,
	"ClusterRole":              true,
	"ClusterRoleBinding":       true,
	"Role":                     true,
	"RoleBinding":              true,
	"ConfigMap":                true,
	"CustomResourceDefinition": true,
	"Namespace":                true,
	"Service":                  true,
}

// moduleHandlers returns every onboarded module handler.
// Keep in sync with existingModules in cmd/main.go.
func moduleHandlers() []modules.ModuleHandler {
	return []modules.ModuleHandler{
		aigateway.NewHandler(),
		mcplifecycleoperator.NewHandler(),
	}
}

func TestModuleManifestRendering(t *testing.T) {
	chartsRoot := os.Getenv("DEFAULT_CHARTS_PATH")
	if chartsRoot == "" {
		chartsRoot = filepath.Join("..", "..", "..", "opt", "charts")
	}

	manifestsRoot := os.Getenv("DEFAULT_MANIFESTS_PATH")
	if manifestsRoot == "" {
		manifestsRoot = filepath.Join("..", "..", "..", "opt", "manifests")
	}

	absChartsRoot, err := filepath.Abs(chartsRoot)
	if err != nil {
		t.Fatalf("failed to resolve charts root %s: %v", chartsRoot, err)
	}

	absManifestsRoot, err := filepath.Abs(manifestsRoot)
	if err != nil {
		t.Fatalf("failed to resolve manifests root %s: %v", manifestsRoot, err)
	}

	artifactsAvailable := dirExists(absChartsRoot) || dirExists(absManifestsRoot)
	if !artifactsAvailable {
		t.Skipf("neither charts (%s) nor manifests (%s) found (run get_all_manifests.sh first)",
			absChartsRoot, absManifestsRoot)
	}

	handlers := moduleHandlers()
	if len(handlers) == 0 {
		t.Skipf("no module handlers registered; skipping manifest rendering test")
	}

	platforms := testPlatformContexts(absChartsRoot, absManifestsRoot)

	for _, handler := range handlers {
		for _, platform := range platforms {
			manifests := handler.GetOperatorManifests(platform)

			for _, chartInfo := range manifests.HelmCharts {
				if _, err := os.Stat(chartInfo.Chart); os.IsNotExist(err) {
					t.Fatalf("chart directory %s not found for module %s (run get_all_manifests.sh)",
						chartInfo.Chart, handler.GetName())
				}

				t.Run(handler.GetName()+"/helm/"+string(platform.Release.Name), func(t *testing.T) {
					g := NewWithT(t)

					renderer, err := helmRenderer.New([]helmRenderer.Source{{
						Chart:       chartInfo.Chart,
						ReleaseName: chartInfo.ReleaseName,
						Values:      chartInfo.Values,
					}})
					g.Expect(err).ShouldNot(HaveOccurred(), "failed to create helm renderer for %s", handler.GetName())

					resources, err := renderer.Process(t.Context(), nil)
					g.Expect(err).ShouldNot(HaveOccurred(), "failed to render chart for %s", handler.GetName())
					g.Expect(resources).ShouldNot(BeEmpty(), "chart %s rendered zero resources", handler.GetName())

					deploymentCount := 0
					for _, res := range resources {
						kind := res.GetKind()
						g.Expect(allowedKinds).Should(HaveKey(kind),
							"chart %s contains disallowed resource kind %q (name: %s)",
							handler.GetName(), kind, res.GetName())

						if kind == "Deployment" {
							deploymentCount++
						}
					}

					g.Expect(deploymentCount).Should(Equal(1),
						"chart %s should contain exactly 1 Deployment, found %d",
						handler.GetName(), deploymentCount)
				})
			}

			for _, manifestInfo := range manifests.Manifests {
				renderPath := manifestInfo.String()
				if _, err := os.Stat(manifestInfo.Path); os.IsNotExist(err) {
					t.Fatalf("manifest directory %s not found for module %s (run get_all_manifests.sh)",
						manifestInfo.Path, handler.GetName())
				}

				t.Run(handler.GetName()+"/kustomize/"+string(platform.Release.Name), func(t *testing.T) {
					g := NewWithT(t)

					ke := kustomize.NewEngine()
					var renderOpts []kustomize.RenderOptsFn
					if platform.ApplicationsNamespace != "" {
						renderOpts = append(renderOpts, kustomize.WithNamespace(platform.ApplicationsNamespace))
					}

					resources, err := ke.Render(renderPath, renderOpts...)
					g.Expect(err).ShouldNot(HaveOccurred(), "failed to render kustomize manifests for %s", handler.GetName())
					g.Expect(resources).ShouldNot(BeEmpty(), "kustomize %s rendered zero resources", handler.GetName())

					deploymentCount := 0
					for _, res := range resources {
						kind := res.GetKind()
						g.Expect(allowedKinds).Should(HaveKey(kind),
							"kustomize %s contains disallowed resource kind %q (name: %s)",
							handler.GetName(), kind, res.GetName())

						if kind == "Deployment" {
							deploymentCount++
						}
					}

					g.Expect(deploymentCount).Should(Equal(1),
						"kustomize %s should contain exactly 1 Deployment, found %d",
						handler.GetName(), deploymentCount)
				})
			}
		}
	}

}
