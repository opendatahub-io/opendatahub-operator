package dashboard_test

import (
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"

	. "github.com/onsi/gomega"
)

// TestDashboardHardwareProfileJSONSerialization tests JSON serialization/deserialization of hardware profiles.
func TestDashboardHardwareProfileJSONSerialization(t *testing.T) {
	g := NewWithT(t)

	// Create a profile with known values
	profile := &dashboard.DashboardHardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-profile",
			Namespace: "test-namespace",
		},
		Spec: dashboard.DashboardHardwareProfileSpec{
			DisplayName: "Test Display Name",
			Enabled:     true,
			Description: "Test Description",
		},
	}

	// Test JSON serialization
	jsonData, err := json.Marshal(profile)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(jsonData).ShouldNot(BeEmpty())

	// Test JSON deserialization
	var deserializedProfile dashboard.DashboardHardwareProfile
	err = json.Unmarshal(jsonData, &deserializedProfile)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify all fields are preserved
	g.Expect(deserializedProfile.Name).Should(Equal(profile.Name))
	g.Expect(deserializedProfile.Namespace).Should(Equal(profile.Namespace))
	g.Expect(deserializedProfile.Spec.DisplayName).Should(Equal(profile.Spec.DisplayName))
	g.Expect(deserializedProfile.Spec.Enabled).Should(Equal(profile.Spec.Enabled))
	g.Expect(deserializedProfile.Spec.Description).Should(Equal(profile.Spec.Description))

	// Test list serialization
	list := &dashboard.DashboardHardwareProfileList{
		Items: []dashboard.DashboardHardwareProfile{*profile},
	}

	listJSON, err := json.Marshal(list)
	g.Expect(err).ShouldNot(HaveOccurred())

	var deserializedList dashboard.DashboardHardwareProfileList
	err = json.Unmarshal(listJSON, &deserializedList)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(deserializedList.Items).Should(HaveLen(1))
	g.Expect(deserializedList.Items[0].Name).Should(Equal(profile.Name))
}
