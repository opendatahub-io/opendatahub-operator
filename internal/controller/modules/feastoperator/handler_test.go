package feastoperator_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/feastoperator"

	. "github.com/onsi/gomega"
)

func newFeastPlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					FeastOperator: componentApi.DSCFeastOperator{
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
	h := feastoperator.NewHandler()
	g.Expect(h.IsEnabled(newFeastPlatformCtx(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()
	g.Expect(h.IsEnabled(newFeastPlatformCtx(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_NilPlatformContext(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestIsEnabled_NilDSC(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()
	ctx := &modules.PlatformContext{}
	g.Expect(h.IsEnabled(ctx)).Should(BeFalse())
}

func TestBuildModuleCR_NilPlatformReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_BasicCR(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()
	platform := newFeastPlatformCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.FeastOperatorInstanceName))
	g.Expect(u.GetKind()).Should(Equal(componentApi.FeastOperatorKind))
	g.Expect(u.GetAPIVersion()).Should(Equal("components.platform.opendatahub.io/v1"))
}

func TestGetRelatedImages(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()
	images := h.GetRelatedImages()

	g.Expect(images).Should(ConsistOf(
		"RELATED_IMAGE_ODH_FEAST_OPERATOR_IMAGE",
		"RELATED_IMAGE_ODH_FEATURE_SERVER_IMAGE",
	))
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.FeastOperatorComponentName))
}

func TestGetGVK(t *testing.T) {
	g := NewWithT(t)
	h := feastoperator.NewHandler()
	gvk := h.GetGVK()
	g.Expect(gvk.Group).Should(Equal("components.platform.opendatahub.io"))
	g.Expect(gvk.Version).Should(Equal("v1"))
	g.Expect(gvk.Kind).Should(Equal("FeastOperator"))
}
