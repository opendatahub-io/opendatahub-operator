package modules_test

import (
	"os"
	"path/filepath"
	"testing"

	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"

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
}

// moduleHandlers returns module handlers that use Helm charts for operator deployment.
// Only module handlers using HelmCharts need to be registered here for compliance testing.
// Kustomize-based modules (like AIGateway) do not require chart compliance checks.
// Keep this in sync with Helm-based entries in existingModules (cmd/main.go).
func moduleHandlers() []modules.ModuleHandler {
	return []modules.ModuleHandler{
		// AIGateway uses Kustomize, not Helm - no entry needed
		// Add future Helm-based module handlers here
	}
}

func TestModuleChartCompliance(t *testing.T) {
	chartsRoot := os.Getenv("DEFAULT_CHARTS_PATH")
	if chartsRoot == "" {
		chartsRoot = filepath.Join("..", "..", "..", "opt", "charts")
	}

	absChartsRoot, err := filepath.Abs(chartsRoot)
	if err != nil {
		t.Fatalf("failed to resolve charts root %s: %v", chartsRoot, err)
	}

	if _, err := os.Stat(absChartsRoot); os.IsNotExist(err) {
		t.Skipf("charts root %s not found (run get_all_manifests.sh first)", absChartsRoot)
	}

	handlers := moduleHandlers()
	if len(handlers) == 0 {
		t.Skipf("no module handlers registered; skipping chart compliance test")
	}

	platform := &modules.PlatformContext{
		ApplicationsNamespace: "test-ns",
		ChartsBasePath:        absChartsRoot,
	}

	testedCount := 0
	for _, handler := range handlers {
		manifests := handler.GetOperatorManifests(platform)
		if len(manifests.HelmCharts) == 0 {
			t.Logf("skipping %s: uses Kustomize manifests, not Helm charts", handler.GetName())
			continue
		}

		for _, chartInfo := range manifests.HelmCharts {
			if _, err := os.Stat(chartInfo.Chart); os.IsNotExist(err) {
				t.Fatalf("chart directory %s not found for module %s (run get_all_manifests.sh first)",
					chartInfo.Chart, handler.GetName())
			}

			testedCount++

			t.Run(handler.GetName(), func(t *testing.T) {
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
	}

	if testedCount == 0 {
		t.Skipf("no Helm-based modules to test; Kustomize modules are tested separately")
	}
}
