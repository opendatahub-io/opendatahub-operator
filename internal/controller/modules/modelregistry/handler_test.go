package modelregistry_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"

	. "github.com/onsi/gomega"
)

func newPlatformModules(mgmtState operatorv1.ManagementState) *configv1alpha1.PlatformModules {
	return &configv1alpha1.PlatformModules{
		ModelRegistry: common.ManagementSpec{
			ManagementState: mgmtState,
		},
	}
}

func newDSCCtx(mgmtState operatorv1.ManagementState) *modules.DSCContext {
	return &modules.DSCContext{
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					ModelRegistry: componentApi.DSCModelRegistry{
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
	h := modelregistry.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(""))).Should(BeFalse())
}

func TestIsEnabled_NilModules(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestPopulatePlatformModule_Managed(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()

	pm := &configv1alpha1.PlatformModules{}
	dscCtx := newDSCCtx(operatorv1.Managed)
	h.PopulatePlatformModule(pm, dscCtx)

	g.Expect(pm.ModelRegistry.ManagementState).Should(Equal(operatorv1.Managed))
}

func TestPopulatePlatformModule_EmptyDefaultsToRemoved(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()

	pm := &configv1alpha1.PlatformModules{}
	dscCtx := newDSCCtx("")
	h.PopulatePlatformModule(pm, dscCtx)

	g.Expect(pm.ModelRegistry.ManagementState).Should(Equal(operatorv1.Removed))
}

func TestPopulatePlatformModule_NilGuards(t *testing.T) {
	h := modelregistry.NewHandler()

	// Should not panic with nil args.
	h.PopulatePlatformModule(nil, nil)
	h.PopulatePlatformModule(&configv1alpha1.PlatformModules{}, nil)
	h.PopulatePlatformModule(nil, &modules.DSCContext{})
}

func TestGetReadyConditionType(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()
	g.Expect(h.GetReadyConditionType()).Should(Equal("ModelRegistryReady"))
}

func TestBuildModuleCR_NilDSCContextReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_NilDSCReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, &modules.DSCContext{}, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()

	dscCtx := newDSCCtx(operatorv1.Managed)
	dscCtx.DSC.Spec.Components.ModelRegistry.RegistriesNamespace = "my-registries"

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, &modules.ModuleCRConfig{
		ApplicationsNamespace: "test-apps-ns",
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(u.GetName()).Should(Equal("default-aihub"))
	g.Expect(u.GroupVersionKind()).Should(Equal(gvk.AIHub))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec["applicationNamespace"]).Should(Equal("test-apps-ns"))
	g.Expect(spec["instancesNamespace"]).Should(Equal("my-registries"))
	g.Expect(spec).ShouldNot(HaveKey("gateway"))

	g.Expect(u.GetAnnotations()[annotations.ManagementStateAnnotation]).Should(Equal(string(operatorv1.Managed)))
}

func TestBuildModuleCR_WithGatewayDomain(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()

	dscCtx := newDSCCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, &modules.ModuleCRConfig{
		ApplicationsNamespace: "test-apps-ns",
		GatewayDomain:         "apps.example.com",
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")

	gateway, ok := spec["gateway"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "gateway is not a map")
	g.Expect(gateway["domain"]).Should(Equal("apps.example.com"))
}

func TestBuildModuleCR_WithoutGatewayDomain(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()

	dscCtx := newDSCCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, &modules.ModuleCRConfig{
		ApplicationsNamespace: "test-apps-ns",
		GatewayDomain:         "",
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec).ShouldNot(HaveKey("gateway"))
}

func TestBuildModuleCR_InstancesNamespaceDefaultsToAppNS(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()

	dscCtx := newDSCCtx(operatorv1.Managed)
	// RegistriesNamespace left empty

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, &modules.ModuleCRConfig{
		ApplicationsNamespace: "fallback-ns",
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(spec["instancesNamespace"]).Should(Equal("fallback-ns"))
}

func TestBuildModuleCR_NilCfg(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()

	dscCtx := newDSCCtx(operatorv1.Managed)
	dscCtx.DSC.Spec.Components.ModelRegistry.RegistriesNamespace = "my-registries"

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(spec["applicationNamespace"]).Should(Equal(""))
	g.Expect(spec["instancesNamespace"]).Should(Equal("my-registries"))
}

func TestBuildModuleCR_EmptyManagementStateNormalized(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()

	dscCtx := newDSCCtx("") // empty management state

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(u.GetAnnotations()[annotations.ManagementStateAnnotation]).Should(Equal(string(operatorv1.Removed)))
}

func TestWriteDSCComponentStatus_Enabled(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()

	dsc := &dscv2.DataScienceCluster{}
	releases := []common.ComponentRelease{
		{Name: "platform", Version: "1.0.0"},
	}

	h.WriteDSCComponentStatus(dsc, true, releases)

	g.Expect(dsc.Status.Components.ModelRegistry.ManagementState).Should(Equal(operatorv1.Managed))
	g.Expect(dsc.Status.Components.ModelRegistry.ModelRegistryCommonStatus).ShouldNot(BeNil())
	g.Expect(dsc.Status.Components.ModelRegistry.ModelRegistryCommonStatus.Releases).Should(HaveLen(1))
	g.Expect(dsc.Status.Components.ModelRegistry.ModelRegistryCommonStatus.Releases[0].Version).Should(Equal("1.0.0"))
}

func TestWriteDSCComponentStatus_Disabled(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()

	dsc := &dscv2.DataScienceCluster{}

	h.WriteDSCComponentStatus(dsc, false, nil)

	g.Expect(dsc.Status.Components.ModelRegistry.ManagementState).Should(Equal(operatorv1.Removed))
	g.Expect(dsc.Status.Components.ModelRegistry.ModelRegistryCommonStatus).Should(BeNil())
}

func TestWriteDSCComponentStatus_NilDSC(t *testing.T) {
	h := modelregistry.NewHandler()
	// Should not panic.
	h.WriteDSCComponentStatus(nil, true, nil)
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := modelregistry.NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.ModelRegistryComponentName))
}
