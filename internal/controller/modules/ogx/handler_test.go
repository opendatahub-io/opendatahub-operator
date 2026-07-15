package ogx_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	ogxModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/ogx"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

func newDSCPlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					OGX: componentApi.DSCOGX{
						ManagementSpec: common.ManagementSpec{
							ManagementState: mgmtState,
						},
					},
				},
			},
		},
	}
}

func TestIsEnabled_Managed(t *testing.T) {
	g := NewWithT(t)
	h := ogxModule.NewHandler()
	g.Expect(h.IsEnabled(newDSCPlatformCtx(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := ogxModule.NewHandler()
	g.Expect(h.IsEnabled(newDSCPlatformCtx(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := ogxModule.NewHandler()
	g.Expect(h.IsEnabled(newDSCPlatformCtx(""))).Should(BeFalse())
}

func TestIsEnabled_NilDSC_NilPlatform(t *testing.T) {
	g := NewWithT(t)
	h := ogxModule.NewHandler()
	ctx := &modules.PlatformContext{ApplicationsNamespace: "opendatahub"}
	g.Expect(h.IsEnabled(ctx)).Should(BeFalse())
}

func newPlatformModePlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		Platform: &configv1alpha1.Platform{
			Spec: configv1alpha1.PlatformSpec{
				Modules: configv1alpha1.PlatformModules{
					OGX: common.ManagementSpec{
						ManagementState: mgmtState,
					},
				},
			},
		},
	}
}

func TestIsEnabled_PlatformMode_Managed(t *testing.T) {
	g := NewWithT(t)
	h := ogxModule.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModePlatformCtx(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_NilPlatformContext(t *testing.T) {
	g := NewWithT(t)
	h := ogxModule.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := ogxModule.NewHandler()
	platform := newDSCPlatformCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.OGXInstanceName))
	g.Expect(u.GetKind()).Should(Equal(componentApi.OGXKind))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec).ShouldNot(HaveKey("managementState"),
		"managementState is a DSC-level field and must not be projected into the component CR")
}

func TestBuildModuleCR_LlamaStackOperatorConflict(t *testing.T) {
	g := NewWithT(t)
	h := ogxModule.NewHandler()
	platform := newDSCPlatformCtx(operatorv1.Managed)
	platform.DSC.Spec.Components.LlamaStackOperator.ManagementState = operatorv1.Managed

	_, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("LlamaStackOperator"))
}

func TestBuildModuleCR_LlamaStackOperatorRemoved(t *testing.T) {
	g := NewWithT(t)
	h := ogxModule.NewHandler()
	platform := newDSCPlatformCtx(operatorv1.Managed)
	platform.DSC.Spec.Components.LlamaStackOperator.ManagementState = operatorv1.Removed

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u).ShouldNot(BeNil())
}

func TestBuildModuleCR_NilPlatformContextReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := ogxModule.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_NilDSCNilPlatformReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := ogxModule.NewHandler()
	platform := &modules.PlatformContext{ApplicationsNamespace: "opendatahub"}

	_, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_PlatformMode(t *testing.T) {
	g := NewWithT(t)
	h := ogxModule.NewHandler()
	platform := newPlatformModePlatformCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.OGXInstanceName))
	g.Expect(u.GetKind()).Should(Equal(componentApi.OGXKind))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec["managementState"]).Should(Equal("Managed"))
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := ogxModule.NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.OGXComponentName))
}

func TestGetDeploymentName(t *testing.T) {
	g := NewWithT(t)
	h := ogxModule.NewHandler()
	g.Expect(h.GetDeploymentName()).Should(Equal("opendatahub-ogx-operator"))
}

func TestImageHandling(t *testing.T) {
	g := NewWithT(t)
	h := ogxModule.NewHandler()

	g.Expect(h.GetControllerImage()).Should(Equal("RELATED_IMAGE_ODH_OGX_MODULE_OPERATOR_IMAGE"))

	g.Expect(h.GetRelatedImages()).Should(ConsistOf(
		"RELATED_IMAGE_ODH_OGX_K8S_OPERATOR_IMAGE",
		"RELATED_IMAGE_ODH_OGX_CORE_IMAGE",
	))

	g.Expect(h.GetRelatedImages()).ShouldNot(ContainElement("RELATED_IMAGE_ODH_OGX_MODULE_OPERATOR_IMAGE"))
}

func TestGetOperatorManifests_PlatformOverlay(t *testing.T) {
	h := ogxModule.NewHandler()

	cases := []struct {
		name     string
		platform common.Platform
		want     string
	}{
		{"odh", cluster.OpenDataHub, "/base/ogx/overlays/odh"},
		{"self-managed-rhoai", cluster.SelfManagedRhoai, "/base/ogx/overlays/rhoai"},
		{"unknown-has-no-overlay", cluster.XKS, "/base/ogx"},
	}

	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := &modules.PlatformContext{
				ApplicationsNamespace: "opendatahub",
				ManifestsBasePath:     "/base",
				Release:               common.Release{Name: tcase.platform},
			}

			m := h.GetOperatorManifests(ctx)
			g.Expect(m.HelmCharts).Should(BeEmpty())
			g.Expect(m.Manifests).Should(HaveLen(1))
			g.Expect(m.Manifests[0].String()).Should(Equal(tcase.want))
		})
	}
}
