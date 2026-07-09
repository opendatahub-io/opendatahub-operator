package modules_test

import (
	"os"
	"path/filepath"
	"testing"

	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/monitoring"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/manifests/kustomize"

	. "github.com/onsi/gomega"
)

var allowedKinds = map[string]bool{
	"Deployment":               true,
	"ServiceAccount":           true,
	"ClusterRole":              true,
	"ClusterRoleBinding":       true,
	"Role":                     true,
	"RoleBinding":              true,
	"ConfigMap":                true,
	"CustomResourceDefinition": true,
	"Service":                  true,
}

// moduleHandlers returns every module handler that the platform operator
// registers. Keep this list in sync with existingModules in cmd/main.go.
// Adding a handler here automatically includes it in the compliance check.
func moduleHandlers() []modules.ModuleHandler {
	return []modules.ModuleHandler{
		aigateway.NewHandler(),
		monitoring.NewHandler(),
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

	chartsExist := true
	if _, err := os.Stat(absChartsRoot); os.IsNotExist(err) {
		chartsExist = false
	}

	manifestsExist := true
	if _, err := os.Stat(absManifestsRoot); os.IsNotExist(err) {
		manifestsExist = false
	}

	if !chartsExist && !manifestsExist {
		t.Skipf("neither charts (%s) nor manifests (%s) found (run get_all_manifests.sh first)",
			absChartsRoot, absManifestsRoot)
	}

	handlers := moduleHandlers()
	if len(handlers) == 0 {
		t.Skipf("no module handlers registered; skipping manifest rendering test")
	}

	platforms := testPlatformContexts(absChartsRoot, absManifestsRoot)

	testedCount := 0

	for _, handler := range handlers {
		for _, platform := range platforms {
			manifests := handler.GetOperatorManifests(platform)

			for _, chartInfo := range manifests.HelmCharts {
				if _, err := os.Stat(chartInfo.Chart); os.IsNotExist(err) {
					t.Logf("chart directory %s not found for module %s, skipping (run get_all_manifests.sh)",
						chartInfo.Chart, handler.GetName())
					continue
				}

				testedCount++

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
					t.Logf("manifest directory %s not found for module %s, skipping (run get_all_manifests.sh)",
						manifestInfo.Path, handler.GetName())
					continue
				}

				testedCount++

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

	if testedCount == 0 {
		t.Skipf("no module artifacts available for testing (run get_all_manifests.sh first)")
	}
}
