package trustyai_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/trustyai"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"

	. "github.com/onsi/gomega"
)

func newPlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					TrustyAI: componentApi.DSCTrustyAI{
						ManagementSpec: common.ManagementSpec{
							ManagementState: mgmtState,
						},
					},
				},
			},
		},
	}
}

func newPlatformModePlatformCtx() *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		Platform: &configv1alpha1.Platform{
			Spec: configv1alpha1.PlatformSpec{
				Modules: configv1alpha1.PlatformModules{
					// TrustyAI not yet supported in Platform mode
				},
			},
		},
	}
}

func TestNewHandler(t *testing.T) {
	g := NewWithT(t)
	h := trustyai.NewHandler()

	g.Expect(h.GetName()).Should(Equal(componentApi.TrustyAIComponentName))
	g.Expect(h.GetGVK()).Should(Equal(gvk.TrustyAI))
}

func TestIsEnabled_Managed(t *testing.T) {
	g := NewWithT(t)
	h := trustyai.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := trustyai.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := trustyai.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(""))).Should(BeFalse())
}

func TestIsEnabled_NilDSC_NilPlatform(t *testing.T) {
	g := NewWithT(t)
	h := trustyai.NewHandler()
	ctx := &modules.PlatformContext{ApplicationsNamespace: "opendatahub"}
	g.Expect(h.IsEnabled(ctx)).Should(BeFalse())
}

func TestIsEnabled_NilPlatformContext(t *testing.T) {
	g := NewWithT(t)
	h := trustyai.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestIsEnabled_PlatformMode_NotSupported(t *testing.T) {
	g := NewWithT(t)
	h := trustyai.NewHandler()
	// TrustyAI is not yet supported in Platform mode, should always return false
	g.Expect(h.IsEnabled(newPlatformModePlatformCtx())).Should(BeFalse())
}

func TestBuildModuleCR_NilPlatformContextReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := trustyai.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := trustyai.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal("trustyai"))
	g.Expect(u.GetKind()).Should(Equal(componentApi.TrustyAIKind))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec["managementState"]).Should(Equal("Managed"))
}

func TestBuildModuleCR_EmptyManagementStatePassedThrough(t *testing.T) {
	g := NewWithT(t)
	h := trustyai.NewHandler()
	platform := newPlatformCtx("")

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")

	if got, exists := spec["managementState"]; exists {
		g.Expect(got).Should(BeEmpty(), "managementState should be empty when not set")
	}
}

func TestBuildModuleCR_PlatformMode_NotSupported(t *testing.T) {
	g := NewWithT(t)
	h := trustyai.NewHandler()
	platform := newPlatformModePlatformCtx()

	_, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("not supported in Platform mode"))
}

func TestGetRelatedImages(t *testing.T) {
	g := NewWithT(t)
	h := trustyai.NewHandler()

	images := h.GetRelatedImages()
	g.Expect(images).Should(HaveLen(12))
	g.Expect(images).Should(ContainElement("RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_IMAGE"))
	g.Expect(images).Should(ContainElement("RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_OPERATOR_IMAGE"))
	g.Expect(images).Should(ContainElement("RELATED_IMAGE_ODH_TA_LMES_DRIVER_IMAGE"))
	g.Expect(images).Should(ContainElement("RELATED_IMAGE_ODH_TA_LMES_JOB_IMAGE"))
	g.Expect(images).Should(ContainElement("RELATED_IMAGE_ODH_FMS_GUARDRAILS_ORCHESTRATOR_IMAGE"))
	g.Expect(images).Should(ContainElement("RELATED_IMAGE_ODH_TRUSTYAI_VLLM_ORCHESTRATOR_GATEWAY_IMAGE"))
	g.Expect(images).Should(ContainElement("RELATED_IMAGE_ODH_BUILT_IN_DETECTOR_IMAGE"))
	g.Expect(images).Should(ContainElement("RELATED_IMAGE_ODH_PYTHON_312_IMAGE"))
	g.Expect(images).Should(ContainElement("RELATED_IMAGE_ODH_TRUSTYAI_GARAK_LLS_PROVIDER_DSP_IMAGE"))
	g.Expect(images).Should(ContainElement("RELATED_IMAGE_ODH_TRUSTYAI_NEMO_GUARDRAILS_SERVER_IMAGE"))
	g.Expect(images).Should(ContainElement("RELATED_IMAGE_ODH_EVAL_HUB_IMAGE"))
	g.Expect(images).Should(ContainElement("RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE"))
}

func TestBuildModuleCR_NilDSCAndPlatformReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := trustyai.NewHandler()
	platform := &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
	}

	_, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("neither DSC nor Platform is available"))
}
