package aigateway_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

func newPlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		Platform: &configv1alpha1.Platform{
			Spec: configv1alpha1.PlatformSpec{
				Modules: configv1alpha1.PlatformModules{
					AIGateway: common.ManagementSpec{
						ManagementState: mgmtState,
					},
				},
			},
		},
	}
}

func newDSC(mgmtState operatorv1.ManagementState) *dscv2.DataScienceCluster {
	return &dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				AIGateway: componentApi.DSCAIGateway{
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

func TestIsEnabled_NilPlatform(t *testing.T) {
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

// Backward compat: BuildModuleCR must populate modelsAsAService from
// kserve.modelsAsService when modelsAsAService is not explicitly set.
func TestBuildModuleCR_LegacyKserveModelsAsService_PopulatesModelsAsAService(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	dsc := &dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}

	u, err := h.BuildModuleCR(context.Background(), nil, dsc, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())

	maas, ok := spec["modelsAsAService"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "modelsAsAService should be present in AIGateway CR spec")
	g.Expect(maas["managementState"]).Should(Equal("Managed"),
		"modelsAsAService.managementState should be Managed (populated from kserve.modelsAsService)")
}

// Backward compat: explicit aigateway.modelsAsAService takes priority over
// kserve.modelsAsService when both are set.
func TestBuildModuleCR_ExplicitModelsAsAServiceWinsOverLegacy(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	dsc := &dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				AIGateway: componentApi.DSCAIGateway{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Removed, // explicit
						},
					},
				},
				Kserve: componentApi.DSCKserve{
					KserveCommonSpec: componentApi.KserveCommonSpec{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed, // legacy
						},
					},
				},
			},
		},
	}

	u, err := h.BuildModuleCR(context.Background(), nil, dsc, nil)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue())

	maas, ok := spec["modelsAsAService"].(map[string]any)
	g.Expect(ok).Should(BeTrue())
	g.Expect(maas["managementState"]).Should(Equal("Removed"),
		"explicit modelsAsAService=Removed must win over legacy kserve.modelsAsService=Managed")
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	dsc := newDSC(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, dsc, nil)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.AIGatewayInstanceName))
	g.Expect(u.GetKind()).Should(Equal(componentApi.AIGatewayKind))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec).ShouldNot(HaveKey("managementState"),
		"managementState is a DSC-level field and must not be projected into the component CR")
}

func TestBuildModuleCR_NilDSCReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.AIGatewayComponentName))
}

// TestGetDeploymentName ensures the handler declares the rendered Deployment
// name (which differs from the module name), so the platform injects the batch
// RELATED_IMAGE_* env vars into the correct Deployment.
func TestGetDeploymentName(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	g.Expect(h.GetDeploymentName()).Should(Equal("ai-gateway-operator"))
	g.Expect(h.GetDeploymentName()).ShouldNot(Equal(h.GetName()),
		"deployment name must differ from module name, which is the whole point of the override")
}

// TestImageHandling ensures the operator image is pinned via ControllerImage
// (inject override, which the action applies to the manager container and any
// initContainer sharing that image) while the batch-gateway operand images are
// injected as RelatedImages env vars.
func TestImageHandling(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()

	g.Expect(h.GetControllerImage()).Should(Equal("RELATED_IMAGE_ODH_AI_GATEWAY_OPERATOR_IMAGE"))

	g.Expect(h.GetRelatedImages()).Should(ConsistOf(
		"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_OPERATOR_IMAGE",
		"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_APISERVER_IMAGE",
		"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_PROCESSOR_IMAGE",
		"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_GC_IMAGE",
		"RELATED_IMAGE_ODH_LLM_D_ASYNC_IMAGE",
		"RELATED_IMAGE_ODH_MAAS_CONTROLLER_IMAGE",
		"RELATED_IMAGE_ODH_MAAS_API_IMAGE",
		"RELATED_IMAGE_ODH_AI_GATEWAY_PAYLOAD_PROCESSING_IMAGE",
		"RELATED_IMAGE_UBI_MINIMAL_IMAGE",
	))

	// The operator image is handled by ControllerImage (image override), not
	// env injection, so it must NOT also appear in RelatedImages.
	g.Expect(h.GetRelatedImages()).ShouldNot(ContainElement("RELATED_IMAGE_ODH_AI_GATEWAY_OPERATOR_IMAGE"))
}

// TestGetOperatorManifests_PlatformOverlay verifies the handler selects the
// platform-specific Kustomize overlay and resolves it under ManifestsBasePath.
func TestGetOperatorManifests_PlatformOverlay(t *testing.T) {
	h := aigateway.NewHandler()

	cases := []struct {
		name     string
		platform common.Platform
		want     string
	}{
		{"odh", cluster.OpenDataHub, "/base/aigateway/manifests/ai-gateway-operator/overlays/odh"},
		{"self-managed-rhoai", cluster.SelfManagedRhoai, "/base/aigateway/manifests/ai-gateway-operator/overlays/rhoai"},
		{"managed-rhoai-not-supported", cluster.ManagedRhoai, "/base/aigateway/manifests/ai-gateway-operator"},
		{"xks-uses-rhoai-overlay", cluster.XKS, "/base/aigateway/manifests/ai-gateway-operator/overlays/rhoai"},
	}

	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := &modules.PlatformContext{
				ApplicationsNamespace: "opendatahub",
				ManifestsBasePath:     "/base",
				Release:               common.Release{Name: tcase.platform},
			}

			m := h.GetOperatorManifests(ctx)
			g.Expect(m.HelmCharts).Should(BeEmpty())
			g.Expect(m.Manifests).Should(HaveLen(1))
			g.Expect(m.Manifests[0].String()).Should(Equal(tcase.want))
		})
	}
}
