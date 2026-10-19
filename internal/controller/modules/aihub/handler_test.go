package aihub_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aihub"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"

	. "github.com/onsi/gomega"
)

func newPlatformModules(mgmtState operatorv1.ManagementState) *configApi.PlatformModules {
	return &configApi.PlatformModules{
		AIHub: common.ManagementSpec{
			ManagementState: mgmtState,
		},
	}
}

func newDSC(mgmtState operatorv1.ManagementState) *dscApi.DataScienceCluster {
	return &dscApi.DataScienceCluster{
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				AIHub: componentApi.DSCAIHub{
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
	h := aihub.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(""))).Should(BeFalse())
}

func TestIsEnabled_NilModules(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestPopulatePlatformModule_Managed(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()

	pm := &configApi.PlatformModules{}
	h.PopulatePlatformModule(pm, &modules.DSCContext{DSC: newDSC(operatorv1.Managed)})

	g.Expect(pm.AIHub.ManagementState).Should(Equal(operatorv1.Managed))
}

func TestPopulatePlatformModule_EmptyDefaultsToRemoved(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()

	pm := &configApi.PlatformModules{}
	h.PopulatePlatformModule(pm, &modules.DSCContext{DSC: newDSC("")})

	g.Expect(pm.AIHub.ManagementState).Should(Equal(operatorv1.Removed))
}

func TestPopulatePlatformModule_NilGuards(t *testing.T) {
	h := aihub.NewHandler()

	// Should not panic with nil args.
	h.PopulatePlatformModule(nil, nil)
	h.PopulatePlatformModule(&configApi.PlatformModules{}, nil)
	h.PopulatePlatformModule(nil, &modules.DSCContext{})
}

func TestGetReadyConditionType(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()
	g.Expect(h.GetReadyConditionType()).Should(Equal("AIHubReady"))
}

func TestBuildModuleCR_NilDSCContextReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_NilDSCReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, &modules.DSCContext{}, nil)
	g.Expect(err).Should(HaveOccurred())
}

// Off-contract behavior tests retained per the user-approved exception: with
// a nil cfg the handler emits an empty applicationNamespace, which the module
// CRD rejects (minLength: 1). They assert the handler's input contract only;
// schema-valid projections are covered by TestBuildModuleCRProjection in
// handler_projection_test.go.
func TestBuildModuleCR_NilCfg(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()

	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.InstancesNamespace = "my-registries"

	u, err := h.BuildModuleCR(context.Background(), nil, &modules.DSCContext{DSC: dsc}, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(spec["applicationNamespace"]).Should(Equal(""))
	g.Expect(spec["instancesNamespace"]).Should(Equal("my-registries"))
}

// EmptyManagementStateNormalized is the nil-cfg variant retained per the
// user-approved exception (see TestBuildModuleCR_NilCfg); a valid-cfg
// empty-management-state projection is covered in handler_projection_test.go.
func TestBuildModuleCR_EmptyManagementStateNormalized(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()

	dsc := newDSC("") // empty management state

	u, err := h.BuildModuleCR(context.Background(), nil, &modules.DSCContext{DSC: dsc}, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(u.GetAnnotations()[annotations.ManagementStateAnnotation]).Should(Equal(string(operatorv1.Removed)))
}

func TestWriteDSCComponentStatus_Enabled(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()

	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.InstancesNamespace = componentApi.DefaultModelRegistriesNamespace
	releases := []common.ComponentRelease{
		{Name: "platform", Version: "1.0.0"},
	}

	h.WriteDSCComponentStatus(dsc, true, releases)

	g.Expect(dsc.Status.Components.AIHub.ManagementState).Should(Equal(operatorv1.Managed))
	g.Expect(dsc.Status.Components.AIHub.AIHubCommonStatus).ShouldNot(BeNil())
	g.Expect(dsc.Status.Components.AIHub.InstancesNamespace).Should(Equal(componentApi.DefaultModelRegistriesNamespace))
	g.Expect(dsc.Status.Components.AIHub.AIHubCommonStatus.Releases).Should(HaveLen(1))
	g.Expect(dsc.Status.Components.AIHub.AIHubCommonStatus.Releases[0].Version).Should(Equal("1.0.0"))
}

func TestWriteDSCComponentStatus_Disabled(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()

	dsc := &dscApi.DataScienceCluster{}

	h.WriteDSCComponentStatus(dsc, false, nil)

	g.Expect(dsc.Status.Components.AIHub.ManagementState).Should(Equal(operatorv1.Removed))
	g.Expect(dsc.Status.Components.AIHub.AIHubCommonStatus).Should(BeNil())
}

func TestWriteDSCComponentStatus_NilDSC(t *testing.T) {
	h := aihub.NewHandler()
	// Should not panic.
	h.WriteDSCComponentStatus(nil, true, nil)
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()
	g.Expect(h.GetName()).Should(Equal("aihub"))
}

func TestWriteDSCComponentStatus_MirrorsNamespaceFromDSCSpec(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()
	dsc := &dscApi.DataScienceCluster{
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				AIHub: componentApi.DSCAIHub{
					AIHubCommonSpec: componentApi.AIHubCommonSpec{
						InstancesNamespace: "odh-model-registries",
					},
				},
			},
		},
	}

	h.WriteDSCComponentStatus(dsc, true, nil)
	g.Expect(dsc.Status.Components.AIHub.InstancesNamespace).Should(Equal("odh-model-registries"))
}

func TestWriteDSCComponentStatus_ClearsNamespaceWhenDisabled(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()
	dsc := &dscApi.DataScienceCluster{
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				AIHub: componentApi.DSCAIHub{
					AIHubCommonSpec: componentApi.AIHubCommonSpec{
						InstancesNamespace: "odh-model-registries",
					},
				},
			},
		},
	}

	releases := []common.ComponentRelease{{Name: "model-registry", Version: "1.0.0"}}
	h.WriteDSCComponentStatus(dsc, true, releases)

	h.WriteDSCComponentStatus(dsc, false, nil)
	g.Expect(dsc.Status.Components.AIHub.InstancesNamespace).Should(BeEmpty())
	g.Expect(dsc.Status.Components.AIHub.ManagementState).Should(Equal(operatorv1.Removed))
	g.Expect(dsc.Status.Components.AIHub.Releases).Should(BeEmpty())
}

func TestWriteDSCComponentStatus_ClearsNamespaceWhenSpecEmpty(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()
	dsc := &dscApi.DataScienceCluster{
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				AIHub: componentApi.DSCAIHub{
					AIHubCommonSpec: componentApi.AIHubCommonSpec{
						InstancesNamespace: "odh-model-registries",
					},
				},
			},
		},
	}

	h.WriteDSCComponentStatus(dsc, true, nil)
	g.Expect(dsc.Status.Components.AIHub.InstancesNamespace).Should(Equal("odh-model-registries"))

	dsc.Spec.Components.AIHub.InstancesNamespace = ""
	h.WriteDSCComponentStatus(dsc, true, nil)
	g.Expect(dsc.Status.Components.AIHub.InstancesNamespace).Should(BeEmpty())
	g.Expect(dsc.Spec.Components.AIHub.InstancesNamespace).Should(BeEmpty())
}

// TestWriteDSCComponentStatus_DefaultNamespaceAllocatesStatus covers the
// webhook-defaulted namespace path: a DSC carrying the defaulted
// InstancesNamespace must get a status entry on first write.
func TestWriteDSCComponentStatus_DefaultNamespaceAllocatesStatus(t *testing.T) {
	g := NewWithT(t)
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.InstancesNamespace = componentApi.DefaultModelRegistriesNamespace

	aihub.NewHandler().WriteDSCComponentStatus(dsc, true, nil)

	g.Expect(dsc.Status.Components.AIHub.AIHubCommonStatus).ShouldNot(BeNil())
	g.Expect(dsc.Status.Components.AIHub.InstancesNamespace).Should(Equal(componentApi.DefaultModelRegistriesNamespace))
}
