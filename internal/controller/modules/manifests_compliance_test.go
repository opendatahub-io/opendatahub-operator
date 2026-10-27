package modules_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	modulebuiltin "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/builtin"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/databaseservice"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/manifests/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

var allowedChartResourceKinds = sets.New(
	gvk.Deployment.GroupKind(),
	gvk.Service.GroupKind(),
	gvk.ServiceAccount.GroupKind(),
	gvk.ClusterRole.GroupKind(),
	gvk.ClusterRoleBinding.GroupKind(),
	gvk.Role.GroupKind(),
	gvk.RoleBinding.GroupKind(),
	gvk.ConfigMap.GroupKind(),
	gvk.CustomResourceDefinition.GroupKind(),
	gvk.MutatingWebhookConfiguration.GroupKind(),
	gvk.ValidatingWebhookConfiguration.GroupKind(),
	gvk.ValidatingAdmissionPolicy.GroupKind(),
	gvk.ValidatingAdmissionPolicyBinding.GroupKind(),
	gvk.CertManagerIssuer.GroupKind(),
	gvk.CertManagerCertificate.GroupKind(),
	gvk.NetworkPolicy.GroupKind(),
)

// allowedResourceKindsByModule contains resource kinds that are valid only for
// specific module manifests. Keep exceptions scoped to the module that owns
// the resource instead of weakening the allowlist for every module.
var allowedResourceKindsByModule = map[string]sets.Set[schema.GroupKind]{
	"ray": sets.New(gvk.SecurityContextConstraints.GroupKind()),
	// The db-operator chart ships the Internal Postgres SCC for OpenShift installs.
	"databaseservice": sets.New(gvk.SecurityContextConstraints.GroupKind()),
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

				assertOperatorManifestCompliance(g, name, name, resources)
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

// TestModuleChartCompliance_DatabaseServiceSCCGate covers the platform-gated
// SCC render that TestModuleChartCompliance cannot exercise: its shared
// PlatformContext has no Release, and the databaseservice handler fails closed
// for unknown/empty platform names.
func TestModuleChartCompliance_DatabaseServiceSCCGate(t *testing.T) {
	chartsRoot := requireAssetsRoot(t, "DEFAULT_CHARTS_PATH", filepath.Join("opt", "charts"))
	h := databaseservice.NewHandler()

	render := func(t *testing.T, release common.Platform) []unstructured.Unstructured {
		t.Helper()
		g := NewWithT(t)

		m := h.GetOperatorManifests(&modules.PlatformContext{
			ApplicationsNamespace: "test-ns",
			ChartsBasePath:        chartsRoot,
			Release:               common.Release{Name: release},
		})
		g.Expect(m.HelmCharts).Should(HaveLen(1))

		renderer, err := helmRenderer.New([]helmRenderer.Source{{
			Chart:       m.HelmCharts[0].Chart,
			ReleaseName: m.HelmCharts[0].ReleaseName,
			Values:      m.HelmCharts[0].Values,
		}})
		if err != nil {
			t.Fatalf("failed to create helm renderer: %v", err)
		}

		resources, err := renderer.Process(t.Context(), nil)
		if err != nil {
			t.Fatalf("failed to render chart: %v", err)
		}
		return resources
	}

	hasSCC := func(resources []unstructured.Unstructured) bool {
		for _, res := range resources {
			if res.GroupVersionKind() == gvk.SecurityContextConstraints &&
				res.GetName() == "odh-db-internal-postgres" {
				return true
			}
		}
		return false
	}

	t.Run("OpenShift release renders the Internal Postgres SCC", func(t *testing.T) {
		g := NewWithT(t)
		g.Expect(hasSCC(render(t, cluster.OpenDataHub))).Should(BeTrue(),
			"chart should render SecurityContextConstraints/odh-db-internal-postgres for ODH")
	})

	t.Run("XKS release omits the SCC", func(t *testing.T) {
		g := NewWithT(t)
		g.Expect(hasSCC(render(t, cluster.XKS))).Should(BeFalse(),
			"chart must not render the SCC for XKS (vanilla Kubernetes)")
	})
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

				assertOperatorManifestCompliance(g, handler.GetName(), name, resources)
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

func assertOperatorManifestCompliance(g Gomega, moduleName, source string, resources []unstructured.Unstructured) {
	deploymentCount := 0
	for _, res := range resources {
		g.Expect(resourceKindAllowed(moduleName, res.GroupVersionKind())).Should(BeTrue(),
			"%s contains disallowed resource %s (name: %s)",
			source, res.GroupVersionKind(), res.GetName())

		if res.GroupVersionKind().GroupKind() == gvk.Deployment.GroupKind() {
			deploymentCount++
		}
	}

	g.Expect(deploymentCount).Should(Equal(1),
		"%s should contain exactly 1 Deployment, found %d",
		source, deploymentCount)
}

func resourceKindAllowed(moduleName string, resourceGVK schema.GroupVersionKind) bool {
	groupKind := resourceGVK.GroupKind()
	if allowedChartResourceKinds.Has(groupKind) {
		return true
	}

	// A missing module entry yields a nil set, whose Has call safely returns false.
	return allowedResourceKindsByModule[moduleName].Has(groupKind)
}

func TestModuleSpecificResourceKindAllowlist(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		moduleName string
		gvk        schema.GroupVersionKind
		allowed    bool
	}{
		"Ray allows SCC": {
			moduleName: "ray",
			gvk:        gvk.SecurityContextConstraints,
			allowed:    true,
		},
		"DatabaseService allows SCC": {
			moduleName: "databaseservice",
			gvk:        gvk.SecurityContextConstraints,
			allowed:    true,
		},
		"other module rejects SCC": {
			moduleName: "aipipelines",
			gvk:        gvk.SecurityContextConstraints,
			allowed:    false,
		},
		"unknown module rejects SCC": {
			gvk:     gvk.SecurityContextConstraints,
			allowed: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)
			g.Expect(resourceKindAllowed(tc.moduleName, tc.gvk)).Should(Equal(tc.allowed))
		})
	}
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
