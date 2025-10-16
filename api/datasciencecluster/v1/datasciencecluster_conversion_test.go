package v1

import (
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
)

// TestConvertInstalledComponents_V1ToV2 verifies that v1 component names are properly
// converted to v2 names while preserving other components unchanged.
func TestConvertInstalledComponents_V1ToV2(t *testing.T) {
	g := NewWithT(t)

	input := map[string]bool{
		"data-science-pipelines-operator": true,
		"dashboard":                       true,
		"workbenches":                     false,
	}

	result := convertInstalledComponents(input, true)

	g.Expect(result).To(HaveKeyWithValue("aipipelines", true))
	g.Expect(result).To(HaveKeyWithValue("dashboard", true))
	g.Expect(result).To(HaveKeyWithValue("workbenches", false))
	g.Expect(result).NotTo(HaveKey("data-science-pipelines-operator"))
}

// TestConvertInstalledComponents_V2ToV1 verifies that v2 component names are properly
// converted to v1 names while preserving other components unchanged.
func TestConvertInstalledComponents_V2ToV1(t *testing.T) {
	g := NewWithT(t)

	input := map[string]bool{
		"aipipelines": true,
		"dashboard":   true,
		"kserve":      false,
	}

	result := convertInstalledComponents(input, false)

	g.Expect(result).To(HaveKeyWithValue("data-science-pipelines-operator", true))
	g.Expect(result).To(HaveKeyWithValue("dashboard", true))
	g.Expect(result).To(HaveKeyWithValue("kserve", false))
	g.Expect(result).NotTo(HaveKey("aipipelines"))
}

// TestConvertInstalledComponents_Nil ensures nil input returns nil without panicking.
func TestConvertInstalledComponents_Nil(t *testing.T) {
	g := NewWithT(t)
	result := convertInstalledComponents(nil, true)
	g.Expect(result).To(BeNil())
}

// TestConvertTo_InstalledComponents verifies that the full ConvertTo method properly
// converts InstalledComponents from v1 to v2 format.
func TestConvertTo_InstalledComponents(t *testing.T) {
	g := NewWithT(t)

	v1DSC := &DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsc",
		},
		Status: DataScienceClusterStatus{
			InstalledComponents: map[string]bool{
				"data-science-pipelines-operator": true,
				"dashboard":                       false,
			},
		},
	}

	v2DSC := &dscv2.DataScienceCluster{}
	err := v1DSC.ConvertTo(v2DSC)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(v2DSC.Status.InstalledComponents).To(HaveKeyWithValue("aipipelines", true))
	g.Expect(v2DSC.Status.InstalledComponents).To(HaveKeyWithValue("dashboard", false))
	g.Expect(v2DSC.Status.InstalledComponents).NotTo(HaveKey("data-science-pipelines-operator"))
}

// TestConvertFrom_InstalledComponents verifies that the full ConvertFrom method properly
// converts InstalledComponents from v2 to v1 format.
func TestConvertFrom_InstalledComponents(t *testing.T) {
	g := NewWithT(t)

	v2DSC := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsc",
		},
		Status: dscv2.DataScienceClusterStatus{
			InstalledComponents: map[string]bool{
				"aipipelines": true,
				"workbenches": false,
			},
		},
	}

	v1DSC := &DataScienceCluster{}
	err := v1DSC.ConvertFrom(v2DSC)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(v1DSC.Status.InstalledComponents).To(HaveKeyWithValue("data-science-pipelines-operator", true))
	g.Expect(v1DSC.Status.InstalledComponents).To(HaveKeyWithValue("workbenches", false))
	g.Expect(v1DSC.Status.InstalledComponents).NotTo(HaveKey("aipipelines"))
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
