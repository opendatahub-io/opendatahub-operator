package modules_test

import (
	"os"
	"path/filepath"
	"testing"

	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	aigatewayModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"
	dashboardModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/dashboard"
	feastModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/feastoperator"
	mcplifecycleoperatorModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/mcplifecycleoperator"
	monitoringModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/monitoring"
	workbenchesModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/workbenches"

	. "github.com/onsi/gomega"
)

var allowedKinds = map[string]bool{
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

// moduleHandlers returns every module handler that the platform operator
// registers. Keep this list in sync with existingModules in cmd/main.go.
// Adding a handler here automatically includes it in the compliance check.
// Handlers without Helm charts (Kustomize-only) are skipped by the test loop.
func moduleHandlers() []modules.ModuleHandler {
	return []modules.ModuleHandler{
		aigatewayModule.NewHandler(),
		dashboardModule.NewHandler(),
		mcplifecycleoperatorModule.NewHandler(),
		monitoringModule.NewHandler(),
		workbenchesModule.NewHandler(),
		feastModule.NewHandler(),
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

	// opt/charts is committed with a .gitkeep, so the directory always exists even
	// when charts have not been downloaded. Detect "not downloaded" by the absence
	// of any chart subdirectory rather than the absence of the root itself, so the
	// skip below only covers a genuinely empty tree — a populated-but-partial tree
	// falls through and a missing individual chart is treated as a failure.
	entries, err := os.ReadDir(absChartsRoot)
	if os.IsNotExist(err) {
		t.Skipf("charts root %s not found (run make get-manifests first)", absChartsRoot)
	}
	if err != nil {
		t.Fatalf("failed to read charts root %s: %v", absChartsRoot, err)
	}

	hasChartDir := false
	for _, e := range entries {
		if e.IsDir() {
			hasChartDir = true
			break
		}
	}
	if !hasChartDir {
		t.Skipf("charts root %s is empty (run make get-manifests first)", absChartsRoot)
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
			continue
		}

		for _, chartInfo := range manifests.HelmCharts {
			if _, err := os.Stat(chartInfo.Chart); os.IsNotExist(err) {
				// The charts tree is populated (guarded above) but this module's
				// declared chart is missing — a real defect (broken get-manifests
				// config or a wrong chart path), so fail rather than skip.
				t.Fatalf("chart directory %s not found for module %s (run make get-manifests first)",
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
		t.Skip("no module charts available to test (run make get-manifests first)")
	}
}
