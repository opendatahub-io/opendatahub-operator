package kserve_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/kserve"

	. "github.com/onsi/gomega"
)

func newPlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					Kserve: componentApi.DSCKserve{
						ManagementSpec: common.ManagementSpec{
							ManagementState: mgmtState,
						},
					},
				},
			},
		},
	}
}

func newPlatformModePlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		Platform: &configv1alpha1.Platform{
			Spec: configv1alpha1.PlatformSpec{
				Modules: configv1alpha1.PlatformModules{
					Kserve: common.ManagementSpec{
						ManagementState: mgmtState,
					},
				},
			},
		},
	}
}

func TestIsEnabled_Managed(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(""))).Should(BeFalse())
}

func TestIsEnabled_NilDSC_NilPlatform(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	ctx := &modules.PlatformContext{}
	g.Expect(h.IsEnabled(ctx)).Should(BeFalse())
}

func TestIsEnabled_NilPlatformContext(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestIsEnabled_PlatformMode_Managed(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModePlatformCtx(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_PlatformMode_Removed(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModePlatformCtx(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_PlatformMode_Empty(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModePlatformCtx(""))).Should(BeFalse())
}

func TestBuildModuleCR_NilPlatformContextReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_NilDSCNilPlatformReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	platform := &modules.PlatformContext{}

	_, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)
	platform.DSC.Spec.Components.Kserve.KserveCommonSpec = componentApi.KserveCommonSpec{
		RawDeploymentServiceConfig: componentApi.KserveRawHeaded,
		NIM: componentApi.NimSpec{
			ManagementState: operatorv1.Managed,
			AirGapped:       true,
		},
		WVA: componentApi.WVASpec{
			ManagementState: operatorv1.Removed,
		},
	}

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.KserveInstanceName))
	g.Expect(u.GetKind()).Should(Equal(componentApi.KserveKind))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec).ShouldNot(HaveKey("managementState"))
	g.Expect(spec["rawDeploymentServiceConfig"]).Should(Equal("Headed"))

	nim, ok := spec["nim"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.nim missing")
	g.Expect(nim["managementState"]).Should(Equal("Managed"))
	g.Expect(nim["airGapped"]).Should(BeTrue())

	wva, ok := spec["wva"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec.wva missing")
	g.Expect(wva["managementState"]).Should(Equal("Removed"))
}

func TestBuildModuleCR_PlatformMode(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	platform := newPlatformModePlatformCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.KserveInstanceName))
	g.Expect(u.GetKind()).Should(Equal(componentApi.KserveKind))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec["managementState"]).Should(Equal("Managed"))
}

func TestBuildModuleCR_HeadedRawServiceConfig(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)
	platform.DSC.Spec.Components.Kserve.RawDeploymentServiceConfig = componentApi.KserveRawHeaded

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(spec["rawDeploymentServiceConfig"]).Should(Equal("Headed"))
}

func TestGetRelatedImages(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	images := h.GetRelatedImages()

	g.Expect(images).Should(ContainElements(
		"RELATED_IMAGE_ODH_KSERVE_CONTROLLER_IMAGE",
		"RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
		"RELATED_IMAGE_ODH_WORKLOAD_VARIANT_AUTOSCALER_CONTROLLER_IMAGE",
		"RELATED_IMAGE_RHAII_VLLM_CUDA_IMAGE",
	))
	g.Expect(images).ShouldNot(ContainElement(h.GetControllerImage()))
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.KserveComponentName))
}
