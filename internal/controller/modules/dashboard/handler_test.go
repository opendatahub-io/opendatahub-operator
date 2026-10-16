package dashboard_test

import (
	"context"
	"path/filepath"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

func newPlatformModules(mgmtState operatorv1.ManagementState) *configApi.PlatformModules {
	return &configApi.PlatformModules{
		Dashboard: common.ManagementSpec{
			ManagementState: mgmtState,
		},
	}
}

func newDSCCtx(mgmtState operatorv1.ManagementState) *modules.DSCContext {
	return &modules.DSCContext{
		DSC: &dscApi.DataScienceCluster{
			Spec: dscApi.DataScienceClusterSpec{
				Components: dscApi.Components{
					Dashboard: componentApi.DSCDashboard{
						DashboardCommonSpec: componentApi.DashboardCommonSpec{
							Standard: componentApi.DashboardStandardSpec{
								ManagementSpec: common.ManagementSpec{
									ManagementState: mgmtState,
								},
							},
						},
					},
					AIHub: componentApi.DSCAIHub{
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
	g.Expect(h.IsEnabled(&configApi.PlatformModules{})).Should(BeFalse())
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

	portal, ok := spec["maasPortal"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.maasPortal missing")
	g.Expect(portal["managementState"]).Should(Equal("Removed"),
		"unset portal should project Removed")

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

func TestBuildModuleCR_EmptyStatesProjectRemoved(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtx("")

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec["managementState"]).Should(Equal("Removed"),
		"absent standard state should project Removed")
	portal, ok := spec["maasPortal"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.maasPortal missing")
	g.Expect(portal["managementState"]).Should(Equal("Removed"),
		"absent portal state should project Removed")
}

func TestBuildModuleCR_ComponentsDefaultToRemovedWhenEmpty(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtx(operatorv1.Managed)
	dscCtx.DSC.Spec.Components.AIHub.ManagementState = ""
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
	// nil dscCtx is valid for DSC-less deployments (e.g. XKS direct CR creation).
	_, err := h.BuildModuleCR(context.Background(), nil, nil, nil)
	g.Expect(err.Error()).Should(ContainSubstring("DSC is nil, cannot build Dashboard CR"))
}

func TestBuildModuleCR_NilDSCReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	// DSCContext with nil DSC is valid for DSC-less deployments.
	_, err := h.BuildModuleCR(context.Background(), nil, &modules.DSCContext{}, nil)
	g.Expect(err.Error()).Should(ContainSubstring("DSC is nil, cannot build Dashboard CR"))
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

func newDSCCtxWithNamespaces(
	mgmtState operatorv1.ManagementState,
	workbenchMgmt operatorv1.ManagementState,
	workbenchNamespace string,
	mrMgmt operatorv1.ManagementState,
	applicationNamespace string,
) *modules.DSCContext {
	ctx := newDSCCtx(mgmtState)
	ctx.DSC.Spec.Components.Workbenches = componentApi.DSCWorkbenches{
		ManagementSpec: common.ManagementSpec{ManagementState: workbenchMgmt},
		WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
			WorkbenchNamespace: workbenchNamespace,
		},
	}
	ctx.DSC.Spec.Components.AIHub = componentApi.DSCAIHub{
		ManagementSpec: common.ManagementSpec{ManagementState: mrMgmt},
		AIHubCommonSpec: componentApi.AIHubCommonSpec{
			InstancesNamespace: applicationNamespace,
		},
	}
	return ctx
}

func newModuleCRConfigWithRelease(releaseName common.Platform) *modules.ModuleCRConfig {
	return &modules.ModuleCRConfig{
		Release: common.Release{Name: releaseName},
	}
}

func TestBuildModuleCR_ProjectsNotebooksNamespace_RHOAI(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtxWithNamespaces(operatorv1.Managed, operatorv1.Managed, "", operatorv1.Removed, "")

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, newModuleCRConfigWithRelease(cluster.SelfManagedRhoai))
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, _ := u.Object["spec"].(map[string]any)
	g.Expect(spec["notebooksNamespace"]).Should(Equal(cluster.DefaultNotebooksNamespaceRHOAI))
	g.Expect(spec).ShouldNot(HaveKey("modelRegistryNamespace"))
}

func TestBuildModuleCR_ProjectsNotebooksNamespace_ODH(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtxWithNamespaces(operatorv1.Managed, operatorv1.Managed, "", operatorv1.Removed, "")

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, newModuleCRConfigWithRelease(cluster.OpenDataHub))
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, _ := u.Object["spec"].(map[string]any)
	g.Expect(spec["notebooksNamespace"]).Should(Equal(cluster.DefaultNotebooksNamespaceODH))
}

func TestBuildModuleCR_ProjectsCustomNotebooksNamespace(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtxWithNamespaces(operatorv1.Managed, operatorv1.Managed, "custom-notebooks", operatorv1.Removed, "")

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, newModuleCRConfigWithRelease(cluster.SelfManagedRhoai))
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, _ := u.Object["spec"].(map[string]any)
	g.Expect(spec["notebooksNamespace"]).Should(Equal("custom-notebooks"))
}

func TestBuildModuleCR_OmitsNotebooksNamespaceWhenWorkbenchesNotManaged(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtxWithNamespaces(operatorv1.Managed, operatorv1.Removed, "", operatorv1.Removed, "")

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, newModuleCRConfigWithRelease(cluster.SelfManagedRhoai))
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, _ := u.Object["spec"].(map[string]any)
	g.Expect(spec).ShouldNot(HaveKey("notebooksNamespace"))
}

func TestBuildModuleCR_ProjectsAIHubNamespace(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtxWithNamespaces(operatorv1.Managed, operatorv1.Removed, "", operatorv1.Managed, "rhoai-model-registries")

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, newModuleCRConfigWithRelease(cluster.SelfManagedRhoai))
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, _ := u.Object["spec"].(map[string]any)
	g.Expect(spec["modelRegistryNamespace"]).Should(Equal("rhoai-model-registries"))
	g.Expect(spec).ShouldNot(HaveKey("notebooksNamespace"))
}

func TestBuildModuleCR_OmitsAIHubNamespaceWhenNotManaged(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtxWithNamespaces(operatorv1.Managed, operatorv1.Removed, "", operatorv1.Removed, "rhoai-model-registries")

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, newModuleCRConfigWithRelease(cluster.SelfManagedRhoai))
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, _ := u.Object["spec"].(map[string]any)
	g.Expect(spec).ShouldNot(HaveKey("modelRegistryNamespace"))
}

func TestBuildModuleCR_ProjectsDefaultedAIHubNamespace(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtxWithNamespaces(operatorv1.Managed, operatorv1.Removed, "", operatorv1.Managed, componentApi.DefaultModelRegistriesNamespace)

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, newModuleCRConfigWithRelease(cluster.SelfManagedRhoai))
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, _ := u.Object["spec"].(map[string]any)
	g.Expect(spec["modelRegistryNamespace"]).Should(Equal(componentApi.DefaultModelRegistriesNamespace))
}

func TestBuildModuleCR_ProjectsNotebooksNamespace_ManagedRhoai(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtxWithNamespaces(operatorv1.Managed, operatorv1.Managed, "", operatorv1.Removed, "")

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, newModuleCRConfigWithRelease(cluster.ManagedRhoai))
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, _ := u.Object["spec"].(map[string]any)
	g.Expect(spec["notebooksNamespace"]).Should(Equal(cluster.DefaultNotebooksNamespaceRHOAI),
		"ManagedRhoai should use the same RHOAI default notebook namespace as SelfManagedRhoai")
}

func TestBuildModuleCR_ProjectsBothNamespaces(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtxWithNamespaces(
		operatorv1.Managed,
		operatorv1.Managed, "rhods-notebooks",
		operatorv1.Managed, "rhoai-model-registries",
	)

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, newModuleCRConfigWithRelease(cluster.SelfManagedRhoai))
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, _ := u.Object["spec"].(map[string]any)
	g.Expect(spec["notebooksNamespace"]).Should(Equal("rhods-notebooks"))
	g.Expect(spec["modelRegistryNamespace"]).Should(Equal("rhoai-model-registries"))
}

func TestBuildModuleCR_NilCfgWithWorkbenchesManaged_DefaultsToODH(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	dscCtx := newDSCCtxWithNamespaces(operatorv1.Managed, operatorv1.Managed, "", operatorv1.Removed, "")

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, _ := u.Object["spec"].(map[string]any)
	g.Expect(spec["notebooksNamespace"]).Should(Equal(cluster.DefaultNotebooksNamespaceODH),
		"nil cfg means no release info — should fall back to ODH default")
}

// populatedState runs PopulatePlatformModule for the given core Dashboard and
// portal management states and returns the resulting PlatformModules state.
func populatedState(coreState, portalState operatorv1.ManagementState) operatorv1.ManagementState {
	h := dashboard.NewHandler()
	dscCtx := newDSCCtx(coreState)
	dscCtx.DSC.Spec.Components.Dashboard.MaaSPortal.ManagementState = portalState
	pm := &configApi.PlatformModules{}
	h.PopulatePlatformModule(pm, dscCtx)
	return pm.Dashboard.ManagementState
}

func TestPopulatePlatformModule_Both(t *testing.T) {
	g := NewWithT(t)
	g.Expect(populatedState(operatorv1.Managed, operatorv1.Managed)).Should(Equal(operatorv1.Managed))
}

func TestPopulatePlatformModule_DashboardOnly(t *testing.T) {
	g := NewWithT(t)
	g.Expect(populatedState(operatorv1.Managed, operatorv1.Removed)).Should(Equal(operatorv1.Managed))
}

func TestPopulatePlatformModule_PortalOnly(t *testing.T) {
	g := NewWithT(t)
	// Core Dashboard Removed but portal Managed: the dashboard-operator Deployment
	// must stay up (compound OR).
	g.Expect(populatedState(operatorv1.Removed, operatorv1.Managed)).Should(Equal(operatorv1.Managed))
}

func TestPopulatePlatformModule_Neither(t *testing.T) {
	g := NewWithT(t)
	g.Expect(populatedState(operatorv1.Removed, operatorv1.Removed)).Should(Equal(operatorv1.Removed))
}

func TestPopulatePlatformModule_EmptyStatesRemoved(t *testing.T) {
	g := NewWithT(t)
	// Absent (empty) on both sides defaults to Removed.
	g.Expect(populatedState("", "")).Should(Equal(operatorv1.Removed))
}

func TestBuildModuleCR_ProjectsMaaSPortal(t *testing.T) {
	tests := []struct {
		name            string
		managementState operatorv1.ManagementState
	}{
		{name: "Managed", managementState: operatorv1.Managed},
		{name: "Removed", managementState: operatorv1.Removed},
		{name: "EmptyDefaultsToRemoved", managementState: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			h := dashboard.NewHandler()
			dscCtx := newDSCCtx(operatorv1.Managed)
			dscCtx.DSC.Spec.Components.Dashboard.MaaSPortal.ManagementState = tt.managementState

			u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, newModuleCRConfig("dashboard.example.com"))
			g.Expect(err).ShouldNot(HaveOccurred())

			spec, ok := u.Object["spec"].(map[string]any)
			g.Expect(ok).Should(BeTrue(), "spec is not a map")

			portal, ok := spec["maasPortal"].(map[string]any)
			g.Expect(ok).Should(BeTrue(), "spec.maasPortal missing")
			want := tt.managementState
			if want != operatorv1.Managed {
				want = operatorv1.Removed
			}
			g.Expect(portal["managementState"]).Should(Equal(string(want)),
				"only Managed passes through; every other state projects Removed")

			// The core state tracks only the standard Dashboard.
			g.Expect(spec["managementState"]).Should(Equal("Managed"))

			// No legacy maasConsumerPortal projection and no bool-style
			// consumerPortal translation.
			g.Expect(spec).ShouldNot(HaveKey("maasConsumerPortal"))
			g.Expect(spec).ShouldNot(HaveKey("consumerPortal"))
		})
	}
}

// TestBuildModuleCR_UsesStandardManagementState covers all four standard/portal
// combinations and distinguishes the Dashboard CR's core projection (standard
// only) from operator enablement (the compound OR computed by
// PopulatePlatformModule).
func TestBuildModuleCR_UsesStandardManagementState(t *testing.T) {
	tests := []struct {
		name         string
		standard     operatorv1.ManagementState
		portal       operatorv1.ManagementState
		wantCore     operatorv1.ManagementState
		wantPortal   operatorv1.ManagementState
		wantOperator operatorv1.ManagementState
	}{
		{name: "both managed", standard: operatorv1.Managed, portal: operatorv1.Managed, wantCore: operatorv1.Managed, wantPortal: operatorv1.Managed, wantOperator: operatorv1.Managed},
		{name: "standard only", standard: operatorv1.Managed, portal: operatorv1.Removed, wantCore: operatorv1.Managed, wantPortal: operatorv1.Removed, wantOperator: operatorv1.Managed},
		{name: "standard with absent portal", standard: operatorv1.Managed, portal: "", wantCore: operatorv1.Managed, wantPortal: operatorv1.Removed, wantOperator: operatorv1.Managed},
		{name: "portal only", standard: operatorv1.Removed, portal: operatorv1.Managed, wantCore: operatorv1.Removed, wantPortal: operatorv1.Managed, wantOperator: operatorv1.Managed},
		{name: "both removed", standard: operatorv1.Removed, portal: operatorv1.Removed, wantCore: operatorv1.Removed, wantPortal: operatorv1.Removed, wantOperator: operatorv1.Removed},
		{name: "portal with absent standard", standard: "", portal: operatorv1.Managed, wantCore: operatorv1.Removed, wantPortal: operatorv1.Managed, wantOperator: operatorv1.Managed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			h := dashboard.NewHandler()
			dscCtx := newDSCCtx(tt.standard)
			dscCtx.DSC.Spec.Components.Dashboard.MaaSPortal.ManagementState = tt.portal

			u, err := h.BuildModuleCR(t.Context(), nil, dscCtx, nil)
			g.Expect(err).NotTo(HaveOccurred())

			spec, ok := u.Object["spec"].(map[string]any)
			g.Expect(ok).To(BeTrue())
			g.Expect(spec["managementState"]).To(Equal(string(tt.wantCore)),
				"core managementState must track only the standard Dashboard")

			portal, ok := spec["maasPortal"].(map[string]any)
			g.Expect(ok).To(BeTrue(), "spec.maasPortal missing")
			g.Expect(portal["managementState"]).To(Equal(string(tt.wantPortal)))

			// Operator enablement stays the compound OR of standard and portal.
			pm := &configApi.PlatformModules{}
			h.PopulatePlatformModule(pm, dscCtx)
			g.Expect(pm.Dashboard.ManagementState).To(Equal(tt.wantOperator),
				"operator enablement must stay up while either workload is Managed")
		})
	}
}

func TestGetSubmoduleConditions_MaaSPortal(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()

	subs := h.GetSubmoduleConditions()
	g.Expect(subs).Should(HaveLen(1))

	sm := subs[0]
	g.Expect(sm.SourceConditionType).Should(Equal("MaaSPortalAvailable"))
	g.Expect(sm.DSCConditionType).Should(Equal("MaaSPortalAvailable"))
	g.Expect(sm.StatusFieldName).Should(Equal("MaaSPortal"))
	g.Expect(sm.IsEnabled).ShouldNot(BeNil())

	enabledCtx := newDSCCtx(operatorv1.Removed)
	enabledCtx.DSC.Spec.Components.Dashboard.MaaSPortal.ManagementState = operatorv1.Managed
	g.Expect(sm.IsEnabled(enabledCtx)).Should(BeTrue())

	disabledCtx := newDSCCtx(operatorv1.Managed)
	disabledCtx.DSC.Spec.Components.Dashboard.MaaSPortal.ManagementState = operatorv1.Removed
	g.Expect(sm.IsEnabled(disabledCtx)).Should(BeFalse())
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
		"RELATED_IMAGE_ODH_MOD_ARCH_NOTEBOOKS_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_MAAS_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_EVAL_HUB_IMAGE",
		"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
		"RELATED_IMAGE_ODH_MODEL_REGISTRY_JOB_ASYNC_UPLOAD_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_AUTOML_IMAGE",
		"RELATED_IMAGE_ODH_AUTOML_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_AUTORAG_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_DATA_CONNECT_HUB_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_AGENT_OPS_IMAGE",
		"RELATED_IMAGE_ODH_MOD_ARCH_DATA_REGISTRY_IMAGE",
		"RELATED_IMAGE_ODH_AUTORAG_IMAGE",
		"RELATED_IMAGE_ODH_CORE_BFF_IMAGE",
		"RELATED_IMAGE_POSTGRESQL_16_IMAGE",
		"RELATED_IMAGE_ODH_OGX_CORE_IMAGE",
	))
}

// newCleanupTestDSC builds the owning DataScienceCluster for cleanup tests.
func newCleanupTestDSC() *dscApi.DataScienceCluster {
	dsc := &dscApi.DataScienceCluster{}
	dsc.SetName("default-dsc")
	dsc.SetUID("test-dsc-uid")
	return dsc
}

// newDashboardCleanupCR builds a Dashboard CR controlled by the test DSC,
// with the given spec, for cleanup tests.
func newDashboardCleanupCR(gvk schema.GroupVersionKind, spec map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	u.SetName(componentApi.DashboardInstanceName)
	u.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: "datasciencecluster.opendatahub.io/v3",
		Kind:       "DataScienceCluster",
		Name:       "default-dsc",
		UID:        "test-dsc-uid",
		Controller: ptr.To(true),
	}})
	u.Object["spec"] = spec
	return u
}

// newDashboardCleanupScheme registers the Dashboard GVK for the fake client.
func newDashboardCleanupScheme(gvk schema.GroupVersionKind) *runtime.Scheme {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(gvk.Kind+"List"), &unstructured.UnstructuredList{})
	return scheme
}

func portalSpec(legacy, canonical string) map[string]any {
	spec := map[string]any{
		"managementState": string(operatorv1.Managed),
	}
	if legacy != "" {
		spec["maasConsumerPortal"] = map[string]any{"managementState": legacy}
	}
	if canonical != "" {
		spec["maasPortal"] = map[string]any{"managementState": canonical}
	}
	return spec
}

func getDashboardCR(g *WithT, cli client.Client, gvk schema.GroupVersionKind) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	g.Expect(cli.Get(context.Background(), clientObjectKey(), u)).Should(Succeed())
	return u
}

func TestCleanupDeprecatedModuleCRFields_RemovesConflictingLegacyValue(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()

	// Legacy says Managed, canonical says Removed: the canonical value is
	// authoritative and the legacy field must be removed.
	cli := fake.NewClientBuilder().
		WithScheme(newDashboardCleanupScheme(h.GetGVK())).
		WithObjects(newDashboardCleanupCR(h.GetGVK(), portalSpec("Managed", "Removed"))).
		Build()

	g.Expect(h.CleanupDeprecatedModuleCRFields(context.Background(), cli, newCleanupTestDSC())).Should(Succeed())

	u := getDashboardCR(g, cli, h.GetGVK())
	_, legacy, err := unstructured.NestedMap(u.Object, "spec", "maasConsumerPortal")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(legacy).Should(BeFalse(), "deprecated maasConsumerPortal must be removed")
	canonical, found, err := unstructured.NestedString(u.Object, "spec", "maasPortal", "managementState")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).Should(BeTrue())
	g.Expect(canonical).Should(Equal("Removed"), "canonical maasPortal must remain authoritative")
}

func TestCleanupDeprecatedModuleCRFields_RemovesRedundantLegacyValue(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()

	// Matching values in both fields: the redundant legacy field is still removed.
	cli := fake.NewClientBuilder().
		WithScheme(newDashboardCleanupScheme(h.GetGVK())).
		WithObjects(newDashboardCleanupCR(h.GetGVK(), portalSpec("Managed", "Managed"))).
		Build()

	g.Expect(h.CleanupDeprecatedModuleCRFields(context.Background(), cli, newCleanupTestDSC())).Should(Succeed())

	u := getDashboardCR(g, cli, h.GetGVK())
	_, legacy, err := unstructured.NestedMap(u.Object, "spec", "maasConsumerPortal")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(legacy).Should(BeFalse())
}

func TestCleanupDeprecatedModuleCRFields_PreservesLegacyOnlyConfiguration(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()

	// Legacy-only (no canonical field yet): existing behavior must be supported,
	// the legacy field must not be removed.
	cli := fake.NewClientBuilder().
		WithScheme(newDashboardCleanupScheme(h.GetGVK())).
		WithObjects(newDashboardCleanupCR(h.GetGVK(), portalSpec("Managed", ""))).
		Build()

	g.Expect(h.CleanupDeprecatedModuleCRFields(context.Background(), cli, newCleanupTestDSC())).Should(Succeed())

	u := getDashboardCR(g, cli, h.GetGVK())
	legacy, found, err := unstructured.NestedString(u.Object, "spec", "maasConsumerPortal", "managementState")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).Should(BeTrue(), "legacy-only configuration must be preserved")
	g.Expect(legacy).Should(Equal("Managed"))
}

func TestCleanupDeprecatedModuleCRFields_SkipsUnownedCR(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()

	// A same-name CR without the DSC owner reference is user-managed: the
	// legacy field must be preserved even when the canonical field is present.
	u := newDashboardCleanupCR(h.GetGVK(), portalSpec("Managed", "Managed"))
	u.SetOwnerReferences(nil)
	cli := fake.NewClientBuilder().
		WithScheme(newDashboardCleanupScheme(h.GetGVK())).
		WithObjects(u).
		Build()

	g.Expect(h.CleanupDeprecatedModuleCRFields(context.Background(), cli, newCleanupTestDSC())).Should(Succeed())

	stored := getDashboardCR(g, cli, h.GetGVK())
	legacy, found, err := unstructured.NestedString(stored.Object, "spec", "maasConsumerPortal", "managementState")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).Should(BeTrue(), "user-managed CRs must not be touched")
	g.Expect(legacy).Should(Equal("Managed"))
}

func TestCleanupDeprecatedModuleCRFields_NilDSCIsNotAnError(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()

	cli := fake.NewClientBuilder().
		WithScheme(newDashboardCleanupScheme(h.GetGVK())).
		WithObjects(newDashboardCleanupCR(h.GetGVK(), portalSpec("Managed", "Managed"))).
		Build()

	g.Expect(h.CleanupDeprecatedModuleCRFields(context.Background(), cli, nil)).Should(Succeed())
}

func TestCleanupDeprecatedModuleCRFields_ConflictOnConcurrentChange(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()

	// A stale read (another writer bumped the resourceVersion after the Get)
	// must surface as a patch conflict so the reconcile retries with fresh
	// state instead of removing the legacy field from an outdated view.
	cli := fake.NewClientBuilder().
		WithScheme(newDashboardCleanupScheme(h.GetGVK())).
		WithObjects(newDashboardCleanupCR(h.GetGVK(), portalSpec("Managed", "Managed"))).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if err := c.Get(ctx, key, obj, opts...); err != nil {
					return err
				}
				obj.SetResourceVersion("stale")
				return nil
			},
		}).
		Build()

	err := h.CleanupDeprecatedModuleCRFields(context.Background(), cli, newCleanupTestDSC())
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("removing deprecated"))

	// The stored CR must be untouched.
	stored := getDashboardCR(g, cli, h.GetGVK())
	legacy, found, err := unstructured.NestedString(stored.Object, "spec", "maasConsumerPortal", "managementState")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).Should(BeTrue())
	g.Expect(legacy).Should(Equal("Managed"))
}

func TestCleanupDeprecatedModuleCRFields_IdempotentOnCanonicalOnly(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()

	// Canonical-only CR: repeated reconciliation must succeed without writes.
	cli := fake.NewClientBuilder().
		WithScheme(newDashboardCleanupScheme(h.GetGVK())).
		WithObjects(newDashboardCleanupCR(h.GetGVK(), portalSpec("", "Removed"))).
		Build()

	before := getDashboardCR(g, cli, h.GetGVK())

	g.Expect(h.CleanupDeprecatedModuleCRFields(context.Background(), cli, newCleanupTestDSC())).Should(Succeed())
	g.Expect(h.CleanupDeprecatedModuleCRFields(context.Background(), cli, newCleanupTestDSC())).Should(Succeed())

	after := getDashboardCR(g, cli, h.GetGVK())
	g.Expect(after.GetResourceVersion()).Should(Equal(before.GetResourceVersion()),
		"no cleanup write should be issued when the legacy field is absent")
}

func TestCleanupDeprecatedModuleCRFields_AbsentCRIsNotAnError(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()

	cli := fake.NewClientBuilder().
		WithScheme(newDashboardCleanupScheme(h.GetGVK())).
		Build()

	g.Expect(h.CleanupDeprecatedModuleCRFields(context.Background(), cli, newCleanupTestDSC())).Should(Succeed())
}

func TestCleanupDeprecatedModuleCRFields_ManagedRemovedSwitching(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()

	// Switching between Managed and Removed: after cleanup, only the canonical
	// field is maintained across repeated reconciliations.
	cli := fake.NewClientBuilder().
		WithScheme(newDashboardCleanupScheme(h.GetGVK())).
		WithObjects(newDashboardCleanupCR(h.GetGVK(), portalSpec("Removed", "Managed"))).
		Build()

	g.Expect(h.CleanupDeprecatedModuleCRFields(context.Background(), cli, newCleanupTestDSC())).Should(Succeed())

	u := getDashboardCR(g, cli, h.GetGVK())
	_, legacy, err := unstructured.NestedMap(u.Object, "spec", "maasConsumerPortal")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(legacy).Should(BeFalse())
	canonical, found, err := unstructured.NestedString(u.Object, "spec", "maasPortal", "managementState")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).Should(BeTrue())
	g.Expect(canonical).Should(Equal("Managed"))
}

func clientObjectKey() client.ObjectKey {
	return client.ObjectKey{Name: componentApi.DashboardInstanceName}
}
