package aigateway_test

import (
	"fmt"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha2 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

func newPlatformModules(mgmtState operatorv1.ManagementState) *configv1alpha2.PlatformModules {
	return &configv1alpha2.PlatformModules{
		AIGateway: common.ManagementSpec{
			ManagementState: mgmtState,
		},
	}
}

func newDSC(mgmtState operatorv1.ManagementState) *dscv3.DataScienceCluster {
	return &dscv3.DataScienceCluster{
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{
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
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_Empty(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(""))).Should(BeFalse())
}

func TestIsEnabled_EmptyModules(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	g.Expect(h.IsEnabled(&configv1alpha2.PlatformModules{})).Should(BeFalse())
}

func TestIsEnabled_NilModules(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestPopulatePlatformModule_ExplicitManaged(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	var pm configv1alpha2.PlatformModules
	h.PopulatePlatformModule(&pm, &modules.DSCContext{DSC: newDSC(operatorv1.Managed)})
	g.Expect(pm.AIGateway.ManagementState).Should(Equal(operatorv1.Managed))
}

func TestPopulatePlatformModule_ExplicitRemoved(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	var pm configv1alpha2.PlatformModules
	h.PopulatePlatformModule(&pm, &modules.DSCContext{DSC: newDSC(operatorv1.Removed)})
	g.Expect(pm.AIGateway.ManagementState).Should(Equal(operatorv1.Removed))
}

func TestPopulatePlatformModule_NilDSC(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	var pm configv1alpha2.PlatformModules
	h.PopulatePlatformModule(&pm, nil)
	g.Expect(pm.AIGateway.ManagementState).Should(BeEmpty())
}

func TestCanonicalModelsAsAService(t *testing.T) {
	cases := []struct {
		name    string
		maas    componentApi.DSCModelsAsServiceSpec
		spec    map[string]any
		enabled bool
	}{
		{name: "empty", spec: map[string]any{}},
		{
			name:    "managed",
			maas:    componentApi.DSCModelsAsServiceSpec{ManagementState: operatorv1.Managed},
			spec:    map[string]any{"managementState": "Managed"},
			enabled: true,
		},
		{
			name: "removed",
			maas: componentApi.DSCModelsAsServiceSpec{ManagementState: operatorv1.Removed},
			spec: map[string]any{"managementState": "Removed"},
		},
	}
	states := []struct {
		name      string
		state     operatorv1.ManagementState
		effective operatorv1.ManagementState
	}{
		{name: "empty", effective: operatorv1.Removed},
		{name: "managed", state: operatorv1.Managed, effective: operatorv1.Managed},
		{name: "removed", state: operatorv1.Removed, effective: operatorv1.Removed},
	}

	for _, tc := range cases {
		for _, gateway := range states {
			for _, kserve := range states {
				name := fmt.Sprintf("%s/aigateway=%s/kserve=%s", tc.name, gateway.name, kserve.name)

				t.Run(name, func(t *testing.T) {
					g := NewWithT(t)
					h := aigateway.NewHandler()
					dsc := newDSC(gateway.state)
					dsc.Spec.Components.Kserve.ManagementState = kserve.state
					dsc.Spec.Components.AIGateway.ModelsAsAService = tc.maas
					dsc.Spec.Components.AIGateway.BatchGateway.ManagementState = operatorv1.Managed
					before := dsc.DeepCopy()
					dscCtx := &modules.DSCContext{DSC: dsc}

					var pm configv1alpha2.PlatformModules
					h.PopulatePlatformModule(&pm, dscCtx)
					g.Expect(pm.AIGateway.ManagementState).Should(Equal(gateway.effective))

					subs := h.GetSubmoduleConditions()
					g.Expect(subs).Should(HaveLen(2))
					g.Expect(subs[0].SourceConditionType).Should(Equal("ModelsAsAServiceReady"))
					g.Expect(subs[0].DSCConditionType).Should(Equal("ModelsAsAServiceReady"))
					g.Expect(subs[0].StatusFieldName).Should(Equal("ModelsAsAService"))
					g.Expect(subs[0].IsEnabled(dscCtx)).Should(Equal(tc.enabled))
					g.Expect(subs[1].IsEnabled(dscCtx)).Should(BeTrue())

					u, err := h.BuildModuleCR(t.Context(), nil, dscCtx, nil)
					g.Expect(err).ShouldNot(HaveOccurred())
					g.Expect(u.Object["spec"]).Should(Equal(map[string]any{
						"modelsAsAService": tc.spec,
						"batchGateway":     map[string]any{"managementState": "Managed"},
					}))
					g.Expect(dsc).Should(Equal(before), "projection must not mutate the DSC")
				})
			}
		}
	}
}

func TestSubmoduleIsEnabled_NilDSC(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	for _, sub := range h.GetSubmoduleConditions() {
		g.Expect(sub.IsEnabled(nil)).Should(BeFalse())
		g.Expect(sub.IsEnabled(&modules.DSCContext{})).Should(BeFalse())
	}
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := aigateway.NewHandler()
	dsc := newDSC(operatorv1.Managed)

	u, err := h.BuildModuleCR(t.Context(), nil, &modules.DSCContext{DSC: dsc}, nil)
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
	_, err := h.BuildModuleCR(t.Context(), nil, nil, nil)
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
		"RELATED_IMAGE_ODH_PYTHON_312_IMAGE",
		"RELATED_IMAGE_ODH_AI_GATEWAY_CONTROLLER_IMAGE",
		"RELATED_IMAGE_ODH_PRAXIS_EXTPROC_IMAGE",
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
		{"managed-rhoai-not-supported", cluster.ManagedRhoai, "/base/aigateway/manifests/ai-gateway-operator/overlays/rhoai"},
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
