package dashboard_test

import (
	"context"
	"path/filepath"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/dashboard"

	. "github.com/onsi/gomega"
)

func newPlatformModules(mgmtState operatorv1.ManagementState) *configv1alpha1.PlatformModules {
	return &configv1alpha1.PlatformModules{
		Dashboard: common.ManagementSpec{
			ManagementState: mgmtState,
		},
	}
}

func newDSCCtx(mgmtState operatorv1.ManagementState) *modules.DSCContext {
	return &modules.DSCContext{
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					Dashboard: componentApi.DSCDashboard{
						ManagementSpec: common.ManagementSpec{
							ManagementState: mgmtState,
						},
					},
					ModelRegistry: componentApi.DSCModelRegistry{
						ManagementSpec: common.ManagementSpec{
							ManagementState: operatorv1.Managed,
						},
					},
					MLflowOperator: componentApi.DSCMLflowOperator{
						ManagementSpec: common.ManagementSpec{
							ManagementState: operatorv1.Removed,
						},
					},
					TrustyAI: componentApi.DSCTrustyAI{
						ManagementSpec: common.ManagementSpec{
							ManagementState: operatorv1.Managed,
						},
					},
					AIPipelines: componentApi.DSCDataSciencePipelines{
						ManagementSpec: common.ManagementSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}
}

func newModuleCRConfig(gatewayDomain string) *modules.ModuleCRConfig {
	return &modules.ModuleCRConfig{
		GatewayDomain: gatewayDomain,
	}
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.DashboardComponentName))
}

func TestGetGVK_UsesPlatformAPIGroup(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	gvk := h.GetGVK()
	g.Expect(gvk.Group).Should(Equal("components.platform.opendatahub.io"))
	g.Expect(gvk.Version).Should(Equal("v1alpha1"))
	g.Expect(gvk.Kind).Should(Equal(componentApi.DashboardKind))
}

func TestDashboardInstanceName_MatchesOperatorCEL(t *testing.T) {
	g := NewWithT(t)
	// dashboard-operator CRD enforces metadata.name == default-dashboard (odh-dashboard#8093).
	g.Expect(componentApi.DashboardInstanceName).Should(Equal("default-dashboard"))
}

func TestIsEnabled_Managed(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(""))).Should(BeFalse())
}

func TestIsEnabled_EmptyModules(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	g.Expect(h.IsEnabled(&configv1alpha1.PlatformModules{})).Should(BeFalse())
}

func TestIsEnabled_NilModules(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, newModuleCRConfig("dashboard.example.com"))
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.DashboardInstanceName))
	g.Expect(u.GetKind()).Should(Equal(componentApi.DashboardKind))
	g.Expect(u.GroupVersionKind().Group).Should(Equal("components.platform.opendatahub.io"))
	g.Expect(u.GroupVersionKind().Version).Should(Equal("v1alpha1"))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec["managementState"]).Should(Equal("Managed"))
	g.Expect(spec["deploymentMode"]).Should(Equal("Standalone"))

	gateway, ok := spec["gateway"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.gateway missing")
	g.Expect(gateway["domain"]).Should(Equal("dashboard.example.com"))

	components, ok := spec["components"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.components missing")
	g.Expect(components).Should(HaveLen(4))

	mrComp, ok := components["modelregistry"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(mrComp["managementState"]).Should(Equal("Managed"))

	mlflowComp, ok := components["mlflowoperator"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(mlflowComp["managementState"]).Should(Equal("Removed"))

	trustyComp, ok := components["trustyai"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(trustyComp["managementState"]).Should(Equal("Managed"))

	pipComp, ok := components["aipipelines"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(pipComp["managementState"]).Should(Equal("Managed"))
}

func TestBuildModuleCR_OmitsGatewayWhenDomainEmpty(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, newModuleCRConfig(""))
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(spec).ShouldNot(HaveKey("gateway"))
}

func TestBuildModuleCR_EmptyManagementStateOmitted(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtx("")

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec).ShouldNot(HaveKey("managementState"),
		"empty ManagementState should be omitted by unstructured conversion (omitempty)")
}

func TestBuildModuleCR_ComponentsDefaultToRemovedWhenEmpty(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtx(operatorv1.Managed)
	dscCtx.DSC.Spec.Components.ModelRegistry.ManagementState = ""
	dscCtx.DSC.Spec.Components.AIPipelines.ManagementState = ""

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())

	components, ok := spec["components"].(map[string]any)
	g.Expect(ok).Should(BeTrue())

	mrComp, ok := components["modelregistry"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(mrComp["managementState"]).Should(Equal("Removed"))

	pipComp, ok := components["aipipelines"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(pipComp["managementState"]).Should(Equal("Removed"))
}

func TestBuildModuleCR_NilDSCContextReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_NilDSCReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()

	_, err := h.BuildModuleCR(context.Background(), nil, &modules.DSCContext{}, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestGetOperatorManifests(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	platform := &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		ChartsBasePath:        "/opt/charts",
		GatewayDomain:         "dashboard.example.com",
	}

	manifests := h.GetOperatorManifests(platform)
	g.Expect(manifests.HelmCharts).Should(HaveLen(1))
	g.Expect(manifests.HelmCharts[0].ReleaseName).Should(Equal("dashboard-operator"))
	g.Expect(manifests.HelmCharts[0].Chart).Should(Equal(
		filepath.Join("/opt/charts", "dashboard-operator"),
	))
	g.Expect(manifests.Manifests).Should(BeEmpty())

	vals, err := manifests.HelmCharts[0].Values(context.Background())
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(vals["namespace"]).Should(Equal("opendatahub"))
	g.Expect(vals["namePrefix"]).Should(Equal(""))

	relatedImages, ok := vals["relatedImages"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "relatedImages values missing")
	g.Expect(relatedImages).Should(HaveKey("RELATED_IMAGE_ODH_DASHBOARD_IMAGE"))
	g.Expect(relatedImages).Should(HaveKey("RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE"))
	for k, v := range relatedImages {
		g.Expect(v).Should(Equal(""), "relatedImages[%s] should be empty string, got %v", k, v)
	}
}

func TestGetControllerImage(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	g.Expect(h.GetControllerImage()).Should(Equal("RELATED_IMAGE_ODH_DASHBOARD_OPERATOR_IMAGE"))
}

func TestGetRelatedImages(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	images := h.GetRelatedImages()

	g.Expect(images).Should(ConsistOf(
		"RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_MODEL_REGISTRY_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_GEN_AI_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_MLFLOW_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_MAAS_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_EVAL_HUB_IMAGE",
		"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
		"RELATED_IMAGE_ODH_MODEL_REGISTRY_JOB_ASYNC_UPLOAD_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_AUTOML_IMAGE",
		"RELATED_IMAGE_ODH_AUTOML_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_AUTORAG_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_AGENT_OPS_IMAGE",
		"RELATED_IMAGE_ODH_AUTORAG_IMAGE",
		"RELATED_IMAGE_ODH_CORE_BFF_IMAGE",
		"RELATED_IMAGE_POSTGRESQL_16_IMAGE",
	))
}
