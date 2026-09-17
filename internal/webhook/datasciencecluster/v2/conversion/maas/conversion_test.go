package maas_test

import (
	"fmt"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1alpha2 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

type maasConversionCase struct {
	canonicalMaaS   operatorv1.ManagementState
	kserveParent    operatorv1.ManagementState
	legacyMaaS      operatorv1.ManagementState
	aigatewayParent operatorv1.ManagementState
}

func maasConversionCases() []maasConversionCase {
	// Cover omitted, Managed, and Removed canonical values across all parent
	// combinations. This keeps the parent gate and omitted-field behavior
	// visible in one readable matrix.
	maasStates := []operatorv1.ManagementState{"", operatorv1.Managed, operatorv1.Removed}
	parentStates := []operatorv1.ManagementState{"", operatorv1.Managed, operatorv1.Removed}
	dimensions := [][]operatorv1.ManagementState{maasStates, parentStates, maasStates, parentStates}
	caseCount := 1
	for _, states := range dimensions {
		caseCount *= len(states)
	}

	cases := make([]maasConversionCase, 0, caseCount)

	for caseIndex := range caseCount {
		// Each digit selects one field's state, enumerating every combination.
		var combination [4]operatorv1.ManagementState
		remaining := caseIndex

		for field, states := range dimensions {
			combination[field] = states[remaining%len(states)]
			remaining /= len(states)
		}

		cases = append(cases, maasConversionCase{
			canonicalMaaS:   combination[0],
			kserveParent:    combination[1],
			legacyMaaS:      combination[2],
			aigatewayParent: combination[3],
		})
	}

	return cases
}

func TestMaaSConversionMatrix(t *testing.T) {
	for _, tc := range maasConversionCases() {
		name := fmt.Sprintf("canonical-maas=%s/kserve-parent=%s/legacy-maas=%s/aigateway-parent=%s",
			tc.canonicalMaaS, tc.kserveParent, tc.legacyMaaS, tc.aigatewayParent)

		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)

			source := newMaaSConversionSource(tc)
			original := source.DeepCopy()

			projected := projectedMaaSState(tc)
			parent := projectedAIGatewayParent(tc)
			effectiveParent := parentStateOrRemoved(parent)

			hub := &dscv3.DataScienceCluster{}
			g.Expect(source.ConvertTo(hub)).To(Succeed())
			g.Expect(source).To(Equal(original))
			g.Expect(hub.Spec.Components.AIGateway.ModelsAsAService.ManagementState).To(Equal(projected))
			g.Expect(hub.Spec.Components.AIGateway.ManagementState).To(Equal(parent))
			g.Expect(hub.ReadMaaSV2State()).To(Equal(
				tc.canonicalMaaS != operatorv1.Managed && tc.legacyMaaS == operatorv1.Managed))

			expectMaaSHandlerBehavior(t, hub, projected, effectiveParent)

			// The v2 read restores legacy MaaS authority while retaining the migrated parent.
			back := &dscv2.DataScienceCluster{}
			g.Expect(back.ConvertFrom(hub)).To(Succeed())

			expected := original.DeepCopy()
			setRetiredOperatorsRemoved(expected)
			expected.Spec.Components.AIGateway.ModelsAsAService.ManagementState = projected
			expected.Spec.Components.AIGateway.ManagementState = parent
			expected.Spec.Components.Kserve.ModelsAsService.ManagementState = operatorv1.Removed
			if tc.canonicalMaaS != operatorv1.Managed && tc.legacyMaaS == operatorv1.Managed {
				expected.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
				expected.Spec.Components.Kserve.ModelsAsService.ManagementState = operatorv1.Managed
			}

			g.Expect(back).To(Equal(expected))

			again := &dscv3.DataScienceCluster{}
			g.Expect(back.ConvertTo(again)).To(Succeed())
			g.Expect(again).To(Equal(hub))

			// Further conversions must be stable.
			g.Expect(back.ConvertFrom(again)).To(Succeed())
			stable := &dscv3.DataScienceCluster{}
			g.Expect(back.ConvertTo(stable)).To(Succeed())
			g.Expect(stable).To(Equal(again))
		})
	}
}

func newMaaSConversionSource(tc maasConversionCase) *dscv2.DataScienceCluster {
	source := &dscv2.DataScienceCluster{}
	source.Spec.Components.Kserve.ManagementState = tc.kserveParent
	source.Spec.Components.Kserve.ModelsAsService.ManagementState = tc.legacyMaaS
	source.Spec.Components.AIGateway.ManagementState = tc.aigatewayParent
	source.Spec.Components.AIGateway.ModelsAsAService.ManagementState = tc.canonicalMaaS

	return source
}

func setRetiredOperatorsRemoved(dsc *dscv2.DataScienceCluster) {
	dsc.Spec.Components.TrainingOperator.ManagementState = operatorv1.Removed
	dsc.Spec.Components.LlamaStackOperator.ManagementState = operatorv1.Removed
	dsc.Status.Components.TrainingOperator.ManagementState = operatorv1.Removed
	dsc.Status.Components.LlamaStackOperator.ManagementState = operatorv1.Removed
}

func projectedMaaSState(tc maasConversionCase) operatorv1.ManagementState {
	switch {
	case tc.canonicalMaaS == operatorv1.Managed:
		return operatorv1.Managed
	case tc.kserveParent == operatorv1.Managed && tc.legacyMaaS == operatorv1.Managed:
		return operatorv1.Managed
	default:
		return operatorv1.Removed
	}
}

func projectedAIGatewayParent(tc maasConversionCase) operatorv1.ManagementState {
	if tc.aigatewayParent != "" {
		return tc.aigatewayParent
	}

	if tc.kserveParent == operatorv1.Managed && tc.legacyMaaS == operatorv1.Managed {
		return operatorv1.Managed
	}

	return ""
}

func parentStateOrRemoved(state operatorv1.ManagementState) operatorv1.ManagementState {
	if state == "" {
		return operatorv1.Removed
	}

	return state
}

func expectMaaSHandlerBehavior(
	t *testing.T,
	dsc *dscv3.DataScienceCluster,
	expectedMaaS operatorv1.ManagementState,
	expectedParent operatorv1.ManagementState,
) {
	t.Helper()
	g := NewWithT(t)

	handler := aigateway.NewHandler()
	dscContext := &modules.DSCContext{DSC: dsc}

	// The module CR receives the selected canonical MaaS state.
	moduleCR, err := handler.BuildModuleCR(t.Context(), nil, dscContext, nil)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(moduleCR.Object).To(jq.Match(
		`(.spec.modelsAsAService.managementState // "") == "%s"`, expectedMaaS))

	// Readiness tracking follows MaaS, independently of its parent's lifecycle.
	maasCondition := handler.Config.SubmoduleConditions[0]
	g.Expect(maasCondition.StatusFieldName).To(Equal("ModelsAsAService"))
	g.Expect(maasCondition.IsEnabled(dscContext)).To(Equal(expectedMaaS == operatorv1.Managed))

	// Platform module lifecycle follows the separately selected parent state.
	platform := &configv1alpha2.PlatformModules{}
	handler.PopulatePlatformModule(platform, dscContext)

	g.Expect(platform.AIGateway.ManagementState).To(Equal(expectedParent))
}

func TestMaaSConversionIgnoresIncomingProvenance(t *testing.T) {
	g := NewWithT(t)

	source := &dscv2.DataScienceCluster{}
	source.Annotations = map[string]string{dscv3.MaaSV2StateAnnotation: "legacy-managed", "example": "kept"}
	source.Spec.Components.Kserve.ModelsAsService.ManagementState = operatorv1.Removed

	hub := &dscv3.DataScienceCluster{}
	g.Expect(source.ConvertTo(hub)).To(Succeed())
	g.Expect(hub.Annotations).To(Equal(map[string]string{"example": "kept"}))

	back := &dscv2.DataScienceCluster{}
	g.Expect(back.ConvertFrom(hub)).To(Succeed())
	g.Expect(back.Spec.Components.Kserve.ModelsAsService.ManagementState).To(Equal(operatorv1.Removed))

	hub.Annotations[dscv3.MaaSV2StateAnnotation] = "unsupported"
	g.Expect(back.ConvertFrom(hub)).To(MatchError(ContainSubstring("provenance marker")))
}

func TestMaaSConversionDefaultsEmptyMaaSStates(t *testing.T) {
	g := NewWithT(t)

	source := &dscv2.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{APIVersion: dscv2.GroupVersion.String(), Kind: "DataScienceCluster"},
	}
	hub := &dscv3.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{APIVersion: dscv3.GroupVersion.String(), Kind: "DataScienceCluster"},
	}
	g.Expect(source.ConvertTo(hub)).To(Succeed())
	g.Expect(hub.Spec.Components.AIGateway.ModelsAsAService.ManagementState).To(Equal(operatorv1.Removed))
	g.Expect(hub.ReadMaaSV2State()).To(BeFalse())

	back := &dscv2.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{APIVersion: dscv2.GroupVersion.String(), Kind: "DataScienceCluster"},
	}
	g.Expect(back.ConvertFrom(hub)).To(Succeed())

	expected := source.DeepCopy()
	setRetiredOperatorsRemoved(expected)
	expected.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed
	expected.Spec.Components.Kserve.ModelsAsService.ManagementState = operatorv1.Removed
	g.Expect(back).To(Equal(expected))
}
