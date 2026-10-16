package kserve_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/kserve"

	. "github.com/onsi/gomega"
)

func newPlatformModules(mgmtState operatorv1.ManagementState) *configApi.PlatformModules {
	return &configApi.PlatformModules{
		Kserve: common.ManagementSpec{
			ManagementState: mgmtState,
		},
	}
}

func newDSC(mgmtState operatorv1.ManagementState) *dscApi.DataScienceCluster {
	return &dscApi.DataScienceCluster{
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
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
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(""))).Should(BeFalse())
}

func TestIsEnabled_NilModules(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestIsEnabled_EmptyModules(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(&configApi.PlatformModules{})).Should(BeFalse())
}

func TestIsEnabled_PlatformMode_Managed(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_PlatformMode_Removed(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_PlatformMode_Empty(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(""))).Should(BeFalse())
}

func TestBuildModuleCR_NilDSCContextReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	_, err := h.BuildModuleCR(t.Context(), nil, nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_NilDSCReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	dscCtx := &modules.DSCContext{}

	_, err := h.BuildModuleCR(t.Context(), nil, dscCtx, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestGetRelatedImages(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	images := h.GetRelatedImages()

	g.Expect(images).Should(ContainElements(
		"RELATED_IMAGE_ODH_KSERVE_CONTROLLER_IMAGE",
		"RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
		"RELATED_IMAGE_RHAII_VLLM_CUDA_IMAGE",
		"RELATED_IMAGE_RHAII_VLLM_OMNI_CUDA_IMAGE",
	))
	g.Expect(images).ShouldNot(ContainElement(h.GetControllerImage()))
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := kserve.NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.KserveComponentName))
}
