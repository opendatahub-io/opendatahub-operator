package agentsoperator_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/agentsoperator"

	. "github.com/onsi/gomega"
)

func newPlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					AgentsOperator: componentApi.DSCAgentsOperator{
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
	h := agentsoperator.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := agentsoperator.NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_NilDSC(t *testing.T) {
	g := NewWithT(t)
	h := agentsoperator.NewHandler()
	g.Expect(h.IsEnabled(&modules.PlatformContext{})).Should(BeFalse())
}

func TestBuildModuleCR_Managed(t *testing.T) {
	g := NewWithT(t)
	h := agentsoperator.NewHandler()
	u, err := h.BuildModuleCR(context.Background(), nil, newPlatformCtx(operatorv1.Managed))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(u.GetName()).To(Equal(componentApi.AgentsOperatorInstanceName))
	g.Expect(u.GetKind()).To(Equal(componentApi.AgentsOperatorKind))
	spec, found, err := unstructured.NestedString(u.Object, "spec", "managementState")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(spec).To(Equal(string(operatorv1.Managed)))
}
