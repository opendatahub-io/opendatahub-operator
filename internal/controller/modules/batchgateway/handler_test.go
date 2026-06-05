package batchgateway_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/batchgateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"

	. "github.com/onsi/gomega"
)

func newPlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	dsc := &dscv2.DataScienceCluster{}
	dsc.Spec.Components.BatchGateway.ManagementState = mgmtState

	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		DSC:                   dsc,
	}
}

func TestIsEnabled_Managed(t *testing.T) {
	g := NewWithT(t)
	h := batchgateway.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := batchgateway.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := batchgateway.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(""))).Should(BeFalse())
}

func TestIsEnabled_NilDSC(t *testing.T) {
	g := NewWithT(t)
	h := batchgateway.NewHandler()
	g.Expect(h.IsEnabled(&modules.PlatformContext{})).Should(BeFalse())
}

func TestIsEnabled_NilPlatformContext(t *testing.T) {
	g := NewWithT(t)
	h := batchgateway.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestBuildModuleCR_NilPlatformContextReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := batchgateway.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_NilDSCReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := batchgateway.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, &modules.PlatformContext{})
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := batchgateway.NewHandler()

	u, err := h.BuildModuleCR(context.Background(), nil, newPlatformCtx(operatorv1.Managed))
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.BatchGatewayInstanceName))
	g.Expect(u.GroupVersionKind()).Should(Equal(gvk.BatchGateway))

	_, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := batchgateway.NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.BatchGatewayComponentName))
}

func TestGetGVK(t *testing.T) {
	g := NewWithT(t)
	h := batchgateway.NewHandler()
	g.Expect(h.GetGVK()).Should(Equal(gvk.BatchGateway))
}
