package kserve_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

// crdPath is the module CRD's location within its local downloaded chart
// assets (see make get-manifests).
const crdPath = "crd/components.platform.opendatahub.io_kserves.yaml"

// kserveSuite drives the KServe projection cases through the shared runner;
// each case is one method registered as a t.Run, building its DSC context
// explicitly from newDSC and amending only the fields under test. The
// module CRD's own spec.managementState (default Managed) is a module field,
// not the DSC management state: the projection cannot emit it because
// KserveCommonSpec has no such field, and its defaulted admitted value is not
// asserted.
type kserveSuite struct {
	*projection.Runner
}

func (s kserveSuite) minimalDefaults(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.ManagementState = ""

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		// The handler always projects the AI Hub selection, defaulting an
		// empty state to Removed; no model cache is emitted. An omitted
		// rawDeploymentServiceConfig is defaulted by the CRD (Headless) and
		// not asserted.
		jq.Match(`.spec.modelRegistry.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.modelCache == null`),
		// managementState and the DSC-level modelsAsService stanza are not
		// part of the projection; the CRD accepts the fields, so admission
		// cannot catch a leak.
		jq.Match(`.spec.managementState == null`),
		jq.Match(`.spec.modelsAsService == null`),
		jq.Match(`.spec.modelsAsAService == null`),
	)))
}

func (s kserveSuite) headedRawServiceConfig(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.ManagementState = ""
	dsc.Spec.Components.Kserve.KserveCommonSpec = componentApi.KserveCommonSpec{
		RawDeploymentServiceConfig: componentApi.KserveRawHeaded,
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.rawDeploymentServiceConfig == "Headed"`),
		jq.Match(`.spec.modelRegistry.managementState == %q`, operatorv1.Removed),
	)))
}

func (s kserveSuite) populatedCommonSpec(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.ManagementState = ""
	// The DSC-level AIGateway MaaS stanza is unknown to the module schema
	// and must not leak into the projection (strict admission rejects any
	// leak).
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.KserveCommonSpec = componentApi.KserveCommonSpec{
		RawDeploymentServiceConfig: componentApi.KserveRawHeaded,
		NIM: componentApi.NimSpec{
			ManagementState: operatorv1.Managed,
			AirGapped:       true,
		},
		OAuthProxy: &componentApi.OAuthProxyConfig{
			Resources: &componentApi.OAuthProxyResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
			},
		},
		EnableLLMInferenceServiceTLS:               new(false),
		EnableLLMInferenceServiceConsoleDashboards: new(true),
		ModelCache: &componentApi.ModelCacheSpec{
			ManagementState: operatorv1.Managed,
			CacheSize:       new(resource.MustParse("100Gi")),
			NodeNames:       []string{"worker-0"},
		},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.rawDeploymentServiceConfig == "Headed"`),
		jq.Match(`.spec.nim.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.nim.airGapped == true`),
		jq.Match(`.spec.oauthProxy == {resources: {requests: {cpu: "100m"}}}`),
		jq.Match(`.spec.enableLLMInferenceServiceTLS == false`),
		jq.Match(`.spec.enableLLMInferenceServiceConsoleDashboards == true`),
		jq.Match(`.spec.modelCache.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.modelCache.cacheSize == "100Gi"`),
		jq.Match(`.spec.modelCache.nodeNames == ["worker-0"]`),
		jq.Match(`.spec.modelCache.nodeSelector == null`),
		jq.Match(`.spec.modelRegistry.managementState == %q`, operatorv1.Removed),
		// The WVA submodule is not part of the projection.
		jq.Match(`.spec.wva == null`),
	)))
}

func (s kserveSuite) complementaryOptionalToggles(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.ManagementState = ""
	dsc.Spec.Components.Kserve.KserveCommonSpec = componentApi.KserveCommonSpec{
		EnableLLMInferenceServiceTLS:               new(true),
		EnableLLMInferenceServiceConsoleDashboards: new(false),
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.enableLLMInferenceServiceTLS == true`),
		jq.Match(`.spec.enableLLMInferenceServiceConsoleDashboards == false`),
	)))
}

func (s kserveSuite) modelCacheWithNodeSelector(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.ManagementState = ""
	dsc.Spec.Components.Kserve.KserveCommonSpec = componentApi.KserveCommonSpec{
		ModelCache: &componentApi.ModelCacheSpec{
			ManagementState: operatorv1.Managed,
			CacheSize:       new(resource.MustParse("50Gi")),
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"tier": "cache"},
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      "kubernetes.io/arch",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"amd64"},
				}},
			},
		},
	}

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelCache.managementState == %q`, operatorv1.Managed),
		jq.Match(`.spec.modelCache.cacheSize == "50Gi"`),
		// nodeNames and nodeSelector are mutually exclusive per the CRD
		// CEL rules; this branch must select via nodeSelector only.
		jq.Match(`.spec.modelCache.nodeNames == null`),
		jq.Match(`.spec.modelCache.nodeSelector.matchLabels.tier == "cache"`),
		jq.Match(`.spec.modelCache.nodeSelector.matchExpressions | length == 1`),
		jq.Match(`.spec.modelCache.nodeSelector.matchExpressions[0].key == "kubernetes.io/arch"`),
		jq.Match(`.spec.modelCache.nodeSelector.matchExpressions[0].operator == "In"`),
		jq.Match(`.spec.modelCache.nodeSelector.matchExpressions[0].values == ["amd64"]`),
	)))
}

func (s kserveSuite) aihubManaged(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.ManagementState = operatorv1.Managed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelRegistry.managementState == %q`, operatorv1.Managed),
	)))
}

func (s kserveSuite) aihubRemoved(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.ManagementState = operatorv1.Removed

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelRegistry.managementState == %q`, operatorv1.Removed),
	)))
}

func (s kserveSuite) aihubEmptyDefaultsToRemoved(t *testing.T) {
	dsc := newDSC(operatorv1.Managed)
	dsc.Spec.Components.AIHub.ManagementState = ""

	s.Validate(t, projection.ModuleCRSpec{
		DSCCtx: modules.DSCContext{
			DSC: dsc,
		},
	}, projection.WithPreAdmissionValidation(And(
		jq.Match(`.spec.modelRegistry.managementState == %q`, operatorv1.Removed),
		jq.Match(`.spec.modelCache == null`),
	)))
}

// TestBuildModuleCRProjection validates KServe projections against the real
// module CRD from the local downloaded assets. It migrates the successful
// BuildModuleCR cases from handler_test.go (handler_aihub_test.go stays
// untouched per its task) and adds the AI Hub selection translation.
func TestBuildModuleCRProjection(t *testing.T) {
	h := kserve.NewHandler()
	s := kserveSuite{
		Runner: projection.NewRunner(t, h, crdPath),
	}

	t.Run("minimal defaults", s.minimalDefaults)
	t.Run("headed raw service config", s.headedRawServiceConfig)
	t.Run("populated common spec", s.populatedCommonSpec)
	t.Run("complementary optional toggles", s.complementaryOptionalToggles)
	t.Run("model cache with node selector", s.modelCacheWithNodeSelector)
	t.Run("aihub managed", s.aihubManaged)
	t.Run("aihub removed", s.aihubRemoved)
	t.Run("aihub empty defaults to removed", s.aihubEmptyDefaultsToRemoved)
}
