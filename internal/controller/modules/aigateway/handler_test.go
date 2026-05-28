package aigateway_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"

	. "github.com/onsi/gomega"
)

func newPlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					AIGateway: componentApi.DSCAIGateway{
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
	h := aigateway.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(""))).Should(BeFalse())
}

func TestIsEnabled_NilDSC(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	ctx := &modules.PlatformContext{ApplicationsNamespace: "opendatahub"}
	g.Expect(h.IsEnabled(ctx)).Should(BeFalse())
}

func TestIsEnabled_NilPlatformContext(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.AIGatewayInstanceName))
	g.Expect(u.GetKind()).Should(Equal(componentApi.AIGatewayKind))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec).ShouldNot(HaveKey("managementState"),
		"managementState is a DSC-level field and must not be projected into the component CR")
}

func TestBuildModuleCR_NilPlatformContextReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_NilDSCReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	platform := &modules.PlatformContext{ApplicationsNamespace: "opendatahub"}

	_, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).Should(HaveOccurred())
}

func TestGetRelatedImages(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	images := h.GetRelatedImages()

	g.Expect(images).Should(ConsistOf(
		"RELATED_IMAGE_ODH_AI_GATEWAY_OPERATOR_IMAGE",
		"RELATED_IMAGE_ODH_BATCH_GATEWAY_OPERATOR_IMAGE",
		"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_APISERVER_IMAGE",
		"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_PROCESSOR_IMAGE",
		"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_GC_IMAGE",
	))
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.AIGatewayComponentName))
}
