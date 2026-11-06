package aihub_test

import (
	"context"
	"os"
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aihub"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
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

func newDSCCtx(mgmtState operatorv1.ManagementState) *modules.DSCContext {
	return &modules.DSCContext{
		DSC: &dscApi.DataScienceCluster{
			Spec: dscApi.DataScienceClusterSpec{
				Components: dscApi.Components{
					AIHub: componentApi.DSCAIHub{
						ManagementSpec: common.ManagementSpec{
							ManagementState: mgmtState,
						},
						AIHubCommonSpec: componentApi.AIHubCommonSpec{
							InstancesNamespace: componentApi.DefaultModelRegistriesNamespace,
						},
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

func TestGetExtraEnv_CatalogImageSources(t *testing.T) {
	const imageEnv = "RELATED_IMAGE_RHAI_AI_HUB_CATALOG_IMAGE"
	digest := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name  string
		image string
		unset bool
	}{
		{name: "missing CSV image", unset: true},
		{name: "empty CSV image"},
		{name: "floating tag", image: "registry.example.com/catalog:candidate"},
		{name: "tag and digest", image: "registry.example.com/catalog:stable@" + digest},
		{name: "digest only", image: "registry.example.com/catalog@" + digest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			t.Setenv(imageEnv, tt.image)
			if tt.unset {
				g.Expect(os.Unsetenv(imageEnv)).Should(Succeed())
			}
			h := aihub.NewHandler()
			var provider modules.ExtraEnvProvider = h
			env := provider.GetExtraEnv()
			if tt.image == "" {
				// No injection: keep the source defaults rendered by the overlay.
				g.Expect(env).Should(BeEmpty())
				return
			}
			g.Expect(env).Should(Equal(map[string]string{
				"CATALOG_DATA_IMAGE_STREAM_SOURCE":   tt.image,
				"BENCHMARK_DATA_IMAGE_STREAM_SOURCE": tt.image,
			}))
		})
	}
}

func TestGetExtraEnv_PreservesBaseConfiguration(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()
	h.Config.ExtraEnv = map[string]string{
		"EXISTING_SETTING":                   "keep",
		"CATALOG_DATA_IMAGE_STREAM_SOURCE":   "registry.example.com/catalog:default",
		"BENCHMARK_DATA_IMAGE_STREAM_SOURCE": "registry.example.com/benchmarks:default",
	}

	t.Setenv("RELATED_IMAGE_RHAI_AI_HUB_CATALOG_IMAGE", "")
	g.Expect(h.GetExtraEnv()).Should(Equal(h.Config.ExtraEnv))

	const image = "registry.example.com/unified:stable"
	t.Setenv("RELATED_IMAGE_RHAI_AI_HUB_CATALOG_IMAGE", image)
	env := h.GetExtraEnv()
	g.Expect(env).Should(Equal(map[string]string{
		"EXISTING_SETTING":                   "keep",
		"CATALOG_DATA_IMAGE_STREAM_SOURCE":   image,
		"BENCHMARK_DATA_IMAGE_STREAM_SOURCE": image,
	}))
	g.Expect(h.Config.ExtraEnv).Should(Equal(map[string]string{
		"EXISTING_SETTING":                   "keep",
		"CATALOG_DATA_IMAGE_STREAM_SOURCE":   "registry.example.com/catalog:default",
		"BENCHMARK_DATA_IMAGE_STREAM_SOURCE": "registry.example.com/benchmarks:default",
	}))
}

func TestPopulatePlatformModule_Managed(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()

	pm := &configApi.PlatformModules{}
	dscCtx := newDSCCtx(operatorv1.Managed)
	h.PopulatePlatformModule(pm, dscCtx)

	g.Expect(pm.AIHub.ManagementState).Should(Equal(operatorv1.Managed))
}

func TestPopulatePlatformModule_EmptyDefaultsToRemoved(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()

	pm := &configApi.PlatformModules{}
	dscCtx := newDSCCtx("")
	h.PopulatePlatformModule(pm, dscCtx)

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

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()

	dscCtx := newDSCCtx(operatorv1.Managed)
	dscCtx.DSC.Spec.Components.AIHub.InstancesNamespace = "my-registries"

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
	h := aihub.NewHandler()

	dscCtx := newDSCCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, &modules.ModuleCRConfig{
		ApplicationsNamespace: "test-apps-ns",
		GatewayConfig: &serviceApi.GatewayConfig{
			Status: serviceApi.GatewayConfigStatus{Domain: "apps.example.com"},
		},
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
	h := aihub.NewHandler()

	dscCtx := newDSCCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, &modules.ModuleCRConfig{
		ApplicationsNamespace: "test-apps-ns",
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec).ShouldNot(HaveKey("gateway"))
}

func TestBuildModuleCR_ProjectsDefaultedInstancesNamespace(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()

	dscCtx := newDSCCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, &modules.ModuleCRConfig{
		ApplicationsNamespace: "fallback-ns",
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(spec["applicationNamespace"]).Should(Equal("fallback-ns"))
	g.Expect(spec["instancesNamespace"]).Should(Equal(componentApi.DefaultModelRegistriesNamespace))
	g.Expect(dscCtx.DSC.Spec.Components.AIHub.InstancesNamespace).Should(Equal(componentApi.DefaultModelRegistriesNamespace))
}

func TestBuildModuleCR_NilCfg(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()

	dscCtx := newDSCCtx(operatorv1.Managed)
	dscCtx.DSC.Spec.Components.AIHub.InstancesNamespace = "my-registries"

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(spec["applicationNamespace"]).Should(Equal(""))
	g.Expect(spec["instancesNamespace"]).Should(Equal("my-registries"))
}

func TestBuildModuleCR_EmptyManagementStateNormalized(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()

	dscCtx := newDSCCtx("") // empty management state

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(u.GetAnnotations()[annotations.ManagementStateAnnotation]).Should(Equal(string(operatorv1.Removed)))
}

func TestWriteDSCComponentStatus_Enabled(t *testing.T) {
	g := NewWithT(t)
	h := aihub.NewHandler()

	dsc := newDSCCtx(operatorv1.Managed).DSC
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

func TestWriteDSCComponentStatus_DefaultNamespaceAllocatesStatus(t *testing.T) {
	g := NewWithT(t)
	dsc := newDSCCtx(operatorv1.Managed).DSC

	aihub.NewHandler().WriteDSCComponentStatus(dsc, true, nil)

	g.Expect(dsc.Status.Components.AIHub.AIHubCommonStatus).ShouldNot(BeNil())
	g.Expect(dsc.Status.Components.AIHub.InstancesNamespace).Should(Equal(componentApi.DefaultModelRegistriesNamespace))
}

func TestBuildModuleCR_InstancesNamespaceMayEqualApplicationsNamespace(t *testing.T) {
	g := NewWithT(t)
	dscCtx := newDSCCtx(operatorv1.Managed)
	dscCtx.DSC.Spec.Components.AIHub.InstancesNamespace = "shared-apps"

	u, err := aihub.NewHandler().BuildModuleCR(t.Context(), nil, dscCtx, &modules.ModuleCRConfig{
		ApplicationsNamespace: "shared-apps",
	})
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.Object["spec"]).Should(HaveKeyWithValue("instancesNamespace", "shared-apps"))
}
