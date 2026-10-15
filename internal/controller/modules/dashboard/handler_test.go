package dashboard_test

import (
	"context"
	"path/filepath"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/dashboard"

	. "github.com/onsi/gomega"
)

func newPlatformModules(mgmtState operatorv1.ManagementState) *configApi.PlatformModules {
	return &configApi.PlatformModules{
		Dashboard: common.ManagementSpec{
			ManagementState: mgmtState,
		},
	}
}

func newDSC(mgmtState operatorv1.ManagementState) *dscApi.DataScienceCluster {
	return &dscApi.DataScienceCluster{
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

// populatedState runs PopulatePlatformModule for the given core Dashboard and
// portal management states and returns the resulting PlatformModules state.
func populatedState(coreState, portalState operatorv1.ManagementState) operatorv1.ManagementState {
	h := dashboard.NewHandler()
	dsc := newDSC(coreState)
	dsc.Spec.Components.Dashboard.MaaSPortal.ManagementState = portalState
	pm := &configApi.PlatformModules{}
	h.PopulatePlatformModule(pm, &modules.DSCContext{DSC: dsc})
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

	enabled := newDSC(operatorv1.Removed)
	enabled.Spec.Components.Dashboard.MaaSPortal.ManagementState = operatorv1.Managed
	g.Expect(sm.IsEnabled(&modules.DSCContext{DSC: enabled})).Should(BeTrue())

	disabled := newDSC(operatorv1.Managed)
	disabled.Spec.Components.Dashboard.MaaSPortal.ManagementState = operatorv1.Removed
	g.Expect(sm.IsEnabled(&modules.DSCContext{DSC: disabled})).Should(BeFalse())
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
