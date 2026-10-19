//nolint:testpackage // Verifies handler internals such as defaults and projected fields.
package workbenches

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

func newPlatformModules(mgmtState operatorv1.ManagementState) *configApi.PlatformModules {
	return &configApi.PlatformModules{
		Workbenches: common.ManagementSpec{
			ManagementState: mgmtState,
		},
	}
}

func newDSC(mgmtState operatorv1.ManagementState) *dscApi.DataScienceCluster {
	return &dscApi.DataScienceCluster{
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Workbenches: componentApi.DSCWorkbenches{
					ManagementSpec: common.ManagementSpec{
						ManagementState: mgmtState,
					},
					WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
						WorkbenchNamespace: "my-workbenches",
						WorkbenchesV2: componentApi.WorkbenchesV2Spec{
							ManagementState: operatorv1.Removed,
						},
					},
				},
				MLflowOperator: componentApi.DSCMLflowOperator{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}
}

func newModuleCRConfig() *modules.ModuleCRConfig {
	return &modules.ModuleCRConfig{
		GatewayConfig: &serviceApi.GatewayConfig{
			Status: serviceApi.GatewayConfigStatus{Domain: "apps.example.com"},
		},
		Release: common.Release{
			Name: cluster.OpenDataHub,
		},
	}
}

func TestIsEnabled_Managed(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_NilModules(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestIsEnabled_PlatformMode_Managed(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Managed))).Should(BeTrue())
}

func TestSubmoduleIsEnabled_WorkbenchesV2(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	subs := h.GetSubmoduleConditions()
	g.Expect(subs).Should(HaveLen(1))

	v2Enabled := subs[0].IsEnabled
	g.Expect(v2Enabled(&modules.DSCContext{DSC: newDSC(operatorv1.Managed)})).
		Should(BeFalse(), "workbenchesV2 defaults to Removed in test fixture")
	g.Expect(v2Enabled(nil)).Should(BeFalse())

	managed := newDSC(operatorv1.Managed)
	managed.Spec.Components.Workbenches.WorkbenchesV2.ManagementState = operatorv1.Managed
	g.Expect(v2Enabled(&modules.DSCContext{DSC: managed})).Should(BeTrue())
}

func TestBuildModuleCR_NilPlatformContextReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestGetDeploymentName(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.GetDeploymentName()).Should(Equal(ControllerDeploymentName))
}

func TestImageHandling(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()

	g.Expect(h.GetControllerImage()).Should(Equal("RELATED_IMAGE_ODH_WORKBENCHES_OPERATOR_IMAGE"))
	g.Expect(h.GetRelatedImages()).Should(ContainElement("RELATED_IMAGE_ODH_NOTEBOOK_CONTROLLER_IMAGE"))
	g.Expect(h.GetRelatedImages()).Should(ContainElement("RELATED_IMAGE_ODH_WORKBENCHES_CONTROLLER_IMAGE"))
	g.Expect(h.GetRelatedImages()).ShouldNot(ContainElement("RELATED_IMAGE_ODH_WORKBENCHES_OPERATOR_IMAGE"))
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.WorkbenchesComponentName))
}

func TestWriteLegacyStatusFields_MirrorsFromDSCSpec(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	dsc := &dscApi.DataScienceCluster{
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Workbenches: componentApi.DSCWorkbenches{
					WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
						WorkbenchNamespace: "rhods-notebooks",
					},
				},
			},
		},
	}

	err := h.WriteLegacyStatusFields(context.Background(), nil, dsc, true)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dsc.Status.Components.Workbenches.WorkbenchNamespace).Should(Equal("rhods-notebooks"))
}

func TestWriteLegacyStatusFields_ClearsWhenDisabled(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	dsc := &dscApi.DataScienceCluster{
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Workbenches: componentApi.DSCWorkbenches{
					WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
						WorkbenchNamespace: "rhods-notebooks",
					},
				},
			},
		},
	}

	err := h.WriteLegacyStatusFields(context.Background(), nil, dsc, true)
	g.Expect(err).NotTo(HaveOccurred())

	err = h.WriteLegacyStatusFields(context.Background(), nil, dsc, false)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dsc.Status.Components.Workbenches.WorkbenchNamespace).Should(BeEmpty())
}

func TestWriteLegacyStatusFields_ClearsWhenSpecEmpty(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	dsc := &dscApi.DataScienceCluster{
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Workbenches: componentApi.DSCWorkbenches{
					WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
						WorkbenchNamespace: "rhods-notebooks",
					},
				},
			},
		},
	}

	err := h.WriteLegacyStatusFields(context.Background(), nil, dsc, true)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dsc.Status.Components.Workbenches.WorkbenchNamespace).Should(Equal("rhods-notebooks"))

	dsc.Spec.Components.Workbenches.WorkbenchNamespace = ""
	err = h.WriteLegacyStatusFields(context.Background(), nil, dsc, true)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dsc.Status.Components.Workbenches.WorkbenchNamespace).Should(BeEmpty())
}
