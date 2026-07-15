package v1

import (
	"testing"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	operatorv1 "github.com/openshift/api/operator/v1"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
)

// TestGetV1ComponentName verifies that component name mapping works correctly.
func TestGetV1ComponentName(t *testing.T) {
	g := NewWithT(t)

	// Test components that have different names between v1 and v2
	g.Expect(getV1ComponentName(componentApi.DataSciencePipelinesComponentName)).To(Equal(LegacyDataScienceComponentName))
	g.Expect(getV1ComponentName(componentApi.ModelRegistryComponentName)).To(Equal(LegacyModelRegistryComponentName))

	// Test components that have the same names between v1 and v2
	g.Expect(getV1ComponentName(componentApi.DashboardComponentName)).To(Equal(componentApi.DashboardComponentName))
	g.Expect(getV1ComponentName(componentApi.KserveComponentName)).To(Equal(componentApi.KserveComponentName))
	g.Expect(getV1ComponentName(componentApi.KueueComponentName)).To(Equal(componentApi.KueueComponentName))
	g.Expect(getV1ComponentName(componentApi.RayComponentName)).To(Equal(componentApi.RayComponentName))
	g.Expect(getV1ComponentName(componentApi.TrustyAIComponentName)).To(Equal(componentApi.TrustyAIComponentName))
	g.Expect(getV1ComponentName(componentApi.TrainingOperatorComponentName)).To(Equal(componentApi.TrainingOperatorComponentName))
	g.Expect(getV1ComponentName(componentApi.FeastOperatorComponentName)).To(Equal(componentApi.FeastOperatorComponentName))
	g.Expect(getV1ComponentName(componentApi.LlamaStackOperatorComponentName)).To(Equal(componentApi.LlamaStackOperatorComponentName))
}

// TestConstructInstalledComponentsFromV2Status verifies that the construction function
// properly creates InstalledComponents map from v2 component status.
func TestConstructInstalledComponentsFromV2Status(t *testing.T) {
	g := NewWithT(t)

	v2Status := dscv2.DataScienceClusterStatus{
		Components: dscv2.ComponentsStatus{
			Dashboard: componentApi.DSCDashboardStatus{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Managed,
				},
			},
			AIPipelines: componentApi.DSCDataSciencePipelinesStatus{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Managed,
				},
			},
			Kserve: componentApi.DSCKserveStatus{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Removed,
				},
			},
			Kueue: componentApi.DSCKueueStatus{
				KueueManagementSpec: componentApi.KueueManagementSpec{
					ManagementState: operatorv1.Unmanaged,
				},
			},
			Ray: componentApi.DSCRayStatus{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Managed,
				},
			},
		},
	}

	result := constructInstalledComponentsFromV2Status(v2Status)

	// Test components that are managed (should be true)
	g.Expect(result).To(HaveKeyWithValue(componentApi.DashboardComponentName, true))
	g.Expect(result).To(HaveKeyWithValue(LegacyDataScienceComponentName, true))
	g.Expect(result).To(HaveKeyWithValue(componentApi.RayComponentName, true))

	// Test components that are not managed (should be false)
	g.Expect(result).To(HaveKeyWithValue(componentApi.KserveComponentName, false))
	g.Expect(result).To(HaveKeyWithValue(componentApi.KueueComponentName, false))

	// Test that all expected components are present
	expectedComponents := []string{
		componentApi.DashboardComponentName,
		LegacyDataScienceComponentName, // Special case - uses legacy name
		componentApi.KserveComponentName,
		componentApi.KueueComponentName,
		componentApi.RayComponentName,
		componentApi.TrustyAIComponentName,
		LegacyModelRegistryComponentName, // Special case - uses legacy name
		componentApi.TrainingOperatorComponentName,
		componentApi.FeastOperatorComponentName,
		componentApi.LlamaStackOperatorComponentName,
	}
	for _, component := range expectedComponents {
		g.Expect(result).To(HaveKey(component))
	}
}

// TestConstructInstalledComponentsFromV2Status_EmptyValue verifies behavior with completely empty status
func TestConstructInstalledComponentsFromV2Status_EmptyValue(t *testing.T) {
	g := NewWithT(t)

	// Test with completely empty status (no components configured)
	emptyStatus := dscv2.DataScienceClusterStatus{}
	result := constructInstalledComponentsFromV2Status(emptyStatus)

	// Define expected components list
	expectedComponents := []string{
		componentApi.DashboardComponentName,
		LegacyDataScienceComponentName,
		componentApi.KserveComponentName,
		componentApi.KueueComponentName,
		componentApi.RayComponentName,
		componentApi.TrustyAIComponentName,
		LegacyModelRegistryComponentName,
		componentApi.TrainingOperatorComponentName,
		componentApi.FeastOperatorComponentName,
		componentApi.LlamaStackOperatorComponentName,
		componentApi.WorkbenchesComponentName,
	}

	// Should return map with all components false (since ManagementState is empty/unset)
	g.Expect(result).NotTo(BeNil())
	g.Expect(len(result)).To(Equal(len(expectedComponents))) // All 11 components should be present

	// All should be false since empty ManagementState != Managed
	for _, value := range result {
		g.Expect(value).To(BeFalse())
	}
}

// TestConvertFrom_ConstructsInstalledComponents verifies that ConvertFrom properly
// constructs the v1 InstalledComponents field from v2 component management states.
func TestConvertFrom_ConstructsInstalledComponents(t *testing.T) {
	g := NewWithT(t)

	v2DSC := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsc",
		},
		Status: dscv2.DataScienceClusterStatus{
			Components: dscv2.ComponentsStatus{
				Dashboard: componentApi.DSCDashboardStatus{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
				},
				AIPipelines: componentApi.DSCDataSciencePipelinesStatus{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
				},
				Kserve: componentApi.DSCKserveStatus{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Workbenches: componentApi.DSCWorkbenchesStatus{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Unmanaged,
					},
				},
			},
		},
	}

	v1DSC := &DataScienceCluster{}
	err := v1DSC.ConvertFrom(v2DSC)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(v1DSC.Status.InstalledComponents).To(HaveKeyWithValue(componentApi.DashboardComponentName, true))
	g.Expect(v1DSC.Status.InstalledComponents).To(HaveKeyWithValue(LegacyDataScienceComponentName, true))
	g.Expect(v1DSC.Status.InstalledComponents).To(HaveKeyWithValue(componentApi.KserveComponentName, false))
	g.Expect(v1DSC.Status.InstalledComponents).To(HaveKeyWithValue(componentApi.WorkbenchesComponentName, false))
}

// TestConvertConditions_V1ToV2 verifies that condition types containing v1 component names
// are properly converted to v2 names.
func TestConvertConditions_V1ToV2(t *testing.T) {
	g := NewWithT(t)

	input := []common.Condition{
		{Type: "DataSciencePipelinesReady", Status: metav1.ConditionTrue},
		{Type: "DashboardReady", Status: metav1.ConditionFalse},
	}

	result := convertConditions(input, true)

	g.Expect(result).To(HaveLen(2))
	g.Expect(result[0].Type).To(Equal("AIPipelinesReady"))
	g.Expect(result[0].Status).To(Equal(metav1.ConditionTrue))
	g.Expect(result[1].Type).To(Equal("DashboardReady"))
	g.Expect(result[1].Status).To(Equal(metav1.ConditionFalse))
}

// TestConvertConditions_V2ToV1 verifies that condition types containing v2 component names
// are properly converted to v1 names.
func TestConvertConditions_V2ToV1(t *testing.T) {
	g := NewWithT(t)

	input := []common.Condition{
		{Type: "AIPipelinesReady", Status: metav1.ConditionTrue},
		{Type: "WorkbenchesReady", Status: metav1.ConditionTrue},
	}

	result := convertConditions(input, false)

	g.Expect(result).To(HaveLen(2))
	g.Expect(result[0].Type).To(Equal("DataSciencePipelinesReady"))
	g.Expect(result[0].Status).To(Equal(metav1.ConditionTrue))
	g.Expect(result[1].Type).To(Equal("WorkbenchesReady"))
	g.Expect(result[1].Status).To(Equal(metav1.ConditionTrue))
}

// TestConvertConditions_Nil ensures nil input returns nil without panicking.
func TestConvertConditions_Nil(t *testing.T) {
	g := NewWithT(t)
	result := convertConditions(nil, true)
	g.Expect(result).To(BeNil())
}

// TestConvertFrom_FallsBackToKserveModelsAsServiceWhenAIGatewayNotSet verifies the
// ConvertFrom fallback: when aigateway.modelsAsAService is not set (3.4→3.5 backward-compat
// upgrade path), the stored kserve.modelsAsService is preserved in the v1 output so that
// the CEL transition rule's oldSelf reflects the actual value and v1 API writes succeed.
func TestConvertFrom_FallsBackToKserveModelsAsServiceWhenAIGatewayNotSet(t *testing.T) {
	g := NewWithT(t)

	// Simulate a 3.4 DSC stored in v2: kserve.modelsAsService=Managed, aigateway not set.
	v2DSC := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dsc"},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					KserveCommonSpec: componentApi.KserveCommonSpec{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{ //nolint:staticcheck
							ManagementState: operatorv1.Managed,
						},
					},
				},
				// AIGateway not set — aigateway.modelsAsAService.ManagementState == ""
			},
		},
	}

	v1DSC := &DataScienceCluster{}
	g.Expect(v1DSC.ConvertFrom(v2DSC)).To(Succeed())

	// v1 should reflect the stored kserve.modelsAsService (Managed), not the empty aigateway value.
	// This ensures the CEL transition rule sees oldSelf.managementState == 'Managed' and allows
	// subsequent v1 API writes with modelsAsService=Managed (no "no such key" error).
	g.Expect(v1DSC.Spec.Components.Kserve.ModelsAsService.ManagementState). //nolint:staticcheck
										To(Equal(operatorv1.Managed))
}

// TestConvertFrom_UsesAIGatewayModelsAsServiceWhenSet verifies that when
// aigateway.modelsAsAService is explicitly set, it takes precedence over the
// stored kserve.modelsAsService in the v1 output (post-migration state).
func TestConvertFrom_UsesAIGatewayModelsAsServiceWhenSet(t *testing.T) {
	g := NewWithT(t)

	// Post-migration DSC: kserve.modelsAsService=Removed (cleaned up), aigateway.modelsAsAService=Managed.
	v2DSC := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dsc"},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					KserveCommonSpec: componentApi.KserveCommonSpec{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{ //nolint:staticcheck
							ManagementState: operatorv1.Removed,
						},
					},
				},
				AIGateway: componentApi.DSCAIGateway{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}

	v1DSC := &DataScienceCluster{}
	g.Expect(v1DSC.ConvertFrom(v2DSC)).To(Succeed())

	// v1 should mirror aigateway.modelsAsAService (Managed), not the stored kserve value (Removed).
	g.Expect(v1DSC.Spec.Components.Kserve.ModelsAsService.ManagementState). //nolint:staticcheck
										To(Equal(operatorv1.Managed))
}

// TestConvertTo_MigratesMaaSFromKserveToAIGateway verifies that when converting
// from v1 to v2, kserve.modelsAsService is automatically migrated to aigateway.modelsasservice.
func TestConvertTo_MigratesMaaSFromKserveToAIGateway(t *testing.T) {
	g := NewWithT(t)

	// Create v1 DSC with kserve.modelsAsService enabled
	v1DSC := &DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsc",
		},
		Spec: DataScienceClusterSpec{
			Components: Components{
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

	// Convert v1 to v2
	v2DSC := &dscv2.DataScienceCluster{}
	err := v1DSC.ConvertTo(v2DSC)
	g.Expect(err).ToNot(HaveOccurred())

	// Verify modelsAsService was migrated to aigateway.modelsAsAService
	g.Expect(v2DSC.Spec.Components.AIGateway.ModelsAsAService.ManagementState).To(Equal(operatorv1.Managed))

	// Verify AIGateway was enabled
	g.Expect(v2DSC.Spec.Components.AIGateway.ManagementState).To(Equal(operatorv1.Managed))

	// kserve.modelsAsService is intentionally preserved (not cleared) in v2 for
	// fidelity with what the v1 client wrote. CEL allows Managed→Removed cleanup
	// after migrating to aigateway; the field is pruned from the CRD in 3.7.
	g.Expect(v2DSC.Spec.Components.Kserve.ModelsAsService.ManagementState).To(Equal(operatorv1.Managed))
}

// TestConvertTo_MaaSNotEnabledInV1 verifies that when kserve.modelsAsService is
// not enabled in v1, AIGateway is set to Removed in v2.
func TestConvertTo_MaaSNotEnabledInV1(t *testing.T) {
	g := NewWithT(t)

	v1DSC := &DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsc",
		},
		Spec: DataScienceClusterSpec{
			Components: Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Removed,
						},
					},
				},
			},
		},
	}

	v2DSC := &dscv2.DataScienceCluster{}
	err := v1DSC.ConvertTo(v2DSC)
	g.Expect(err).ToNot(HaveOccurred())

	// AIGateway should be Removed when MaaS is not enabled
	g.Expect(v2DSC.Spec.Components.AIGateway.ManagementState).To(Equal(operatorv1.Removed))

	// modelsAsAService should still be copied to aigateway
	g.Expect(v2DSC.Spec.Components.AIGateway.ModelsAsAService.ManagementState).To(Equal(operatorv1.Removed))
}

// TestConvertTo_MaaSNotConfiguredInV1 verifies that when kserve.modelsAsService is
// never configured (zero-value ManagementState ""), AIGateway is set to Removed in v2.
func TestConvertTo_MaaSNotConfiguredInV1(t *testing.T) {
	g := NewWithT(t)

	v1DSC := &DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsc",
		},
		Spec: DataScienceClusterSpec{
			Components: Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					// ModelsAsService left at zero value — never configured
				},
			},
		},
	}

	v2DSC := &dscv2.DataScienceCluster{}
	err := v1DSC.ConvertTo(v2DSC)
	g.Expect(err).ToNot(HaveOccurred())

	// AIGateway should be Removed when MaaS was never configured (zero-value "")
	g.Expect(v2DSC.Spec.Components.AIGateway.ManagementState).To(Equal(operatorv1.Removed))
	g.Expect(v2DSC.Spec.Components.AIGateway.ModelsAsAService.ManagementState).To(Equal(operatorv1.ManagementState("")))
}

// TestConvertRoundTrip_BatchGatewayPreserved verifies that a v2→v1→v2 round-trip
// preserves BatchGateway state even when modelsAsService is Removed (i.e. the only
// AIGateway signal in v1 would reconstruct AIGateway as Removed).
func TestConvertRoundTrip_BatchGatewayPreserved(t *testing.T) {
	g := NewWithT(t)

	// v2 DSC: AIGateway Managed, BatchGateway Managed, MaaS Removed
	v2Original := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dsc"},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				AIGateway: componentApi.DSCAIGateway{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						BatchGateway: componentApi.AIGatewayBatchGatewaySpec{
							ManagementState: operatorv1.Managed,
						},
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Removed,
						},
					},
				},
			},
		},
	}

	// Step 1: v2 → v1 (ConvertFrom stashes AIGateway state)
	v1DSC := &DataScienceCluster{}
	err := v1DSC.ConvertFrom(v2Original)
	g.Expect(err).ToNot(HaveOccurred())

	// v1 should have kserve.modelsAsService mirrored as Removed
	g.Expect(v1DSC.Spec.Components.Kserve.ModelsAsService.ManagementState).To(Equal(operatorv1.Removed))
	// v1 should carry the stash annotation
	g.Expect(v1DSC.GetAnnotations()).To(HaveKey("conversion.opendatahub.io/aigateway-state"))

	// Step 2: v1 → v2 (ConvertTo restores from stash)
	v2Restored := &dscv2.DataScienceCluster{}
	err = v1DSC.ConvertTo(v2Restored)
	g.Expect(err).ToNot(HaveOccurred())

	// BatchGateway must be preserved — not lost due to round-trip
	g.Expect(v2Restored.Spec.Components.AIGateway.ManagementState).To(Equal(operatorv1.Managed))
	g.Expect(v2Restored.Spec.Components.AIGateway.BatchGateway.ManagementState).To(Equal(operatorv1.Managed))
	g.Expect(v2Restored.Spec.Components.AIGateway.ModelsAsAService.ManagementState).To(Equal(operatorv1.Removed))

	// Internal annotation must be removed from the restored v2 object
	g.Expect(v2Restored.GetAnnotations()).NotTo(HaveKey("conversion.opendatahub.io/aigateway-state"))
}

// TestConvertTo_CorruptStashAnnotationDoesNotLeak verifies that a corrupt stash
// annotation is always removed from the v2 object even when unmarshal fails.
func TestConvertTo_CorruptStashAnnotationDoesNotLeak(t *testing.T) {
	g := NewWithT(t)

	v1DSC := &DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsc",
			Annotations: map[string]string{
				"conversion.opendatahub.io/aigateway-state": "not-valid-json",
			},
		},
	}

	v2DSC := &dscv2.DataScienceCluster{}
	err := v1DSC.ConvertTo(v2DSC)
	g.Expect(err).ToNot(HaveOccurred())

	// Annotation must not leak onto the v2 object even though unmarshal failed
	g.Expect(v2DSC.GetAnnotations()).NotTo(HaveKey("conversion.opendatahub.io/aigateway-state"))
}
