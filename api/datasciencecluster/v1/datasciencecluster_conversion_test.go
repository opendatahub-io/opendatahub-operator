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
