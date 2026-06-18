package dashboard_test

import (
	"context"
	"path/filepath"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/dashboard"

	. "github.com/onsi/gomega"
)

func newPlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		ChartsBasePath:        "/opt/charts",
		GatewayDomain:         "dashboard.example.com",
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					Dashboard: componentApi.DSCDashboard{
						ManagementSpec: common.ManagementSpec{
							ManagementState: mgmtState,
						},
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
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(""))).Should(BeFalse())
}

func TestIsEnabled_NilDSC(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	ctx := &modules.PlatformContext{ApplicationsNamespace: "opendatahub"}
	g.Expect(h.IsEnabled(ctx)).Should(BeFalse())
}

func TestIsEnabled_NilPlatformContext(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.DashboardInstanceName))
	g.Expect(u.GetKind()).Should(Equal(componentApi.DashboardKind))
	g.Expect(u.GroupVersionKind().Group).Should(Equal("components.platform.opendatahub.io"))
	g.Expect(u.GroupVersionKind().Version).Should(Equal("v1alpha1"))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec["managementState"]).Should(Equal("Managed"))

	gateway, ok := spec["gateway"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.gateway missing")
	g.Expect(gateway["domain"]).Should(Equal("dashboard.example.com"))
}

func TestBuildModuleCR_OmitsGatewayWhenDomainEmpty(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)
	platform.GatewayDomain = ""

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(spec).ShouldNot(HaveKey("gateway"))
}

func TestBuildModuleCR_EmptyManagementStatePassedThrough(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	platform := newPlatformCtx("")

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")

	if got, exists := spec["managementState"]; exists {
		g.Expect(got).Should(BeEmpty(), "managementState should be empty when not set")
	}
}

func TestBuildModuleCR_NilPlatformContextReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_NilDSCReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	platform := &modules.PlatformContext{ApplicationsNamespace: "opendatahub"}

	_, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).Should(HaveOccurred())
}

func TestGetOperatorManifests(t *testing.T) {
	g := NewWithT(t)
	h := dashboard.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)

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
		"RELATED_IMAGE_ODH_AUTORAG_IMAGE",
	))
}
