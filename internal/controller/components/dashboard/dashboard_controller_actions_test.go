package dashboard_test

import (
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"

	. "github.com/onsi/gomega"
)

// TestDashboardHardwareProfileTypes tests the exported types for hardware profiles.
func TestDashboardHardwareProfileTypes(t *testing.T) {
	g := NewWithT(t)

	// Test DashboardHardwareProfile struct
	profile := dashboard.CreateTestDashboardHardwareProfile()

	g.Expect(profile.Name).Should(Equal(dashboard.TestProfile))
	g.Expect(profile.Namespace).Should(Equal(dashboard.TestNamespace))
	g.Expect(profile.Spec.DisplayName).Should(Equal(dashboard.TestDisplayName))
	g.Expect(profile.Spec.Enabled).Should(BeTrue())
	g.Expect(profile.Spec.Description).Should(Equal(dashboard.TestDescription))

	// Test DashboardHardwareProfileList
	list := &dashboard.DashboardHardwareProfileList{
		Items: []dashboard.DashboardHardwareProfile{*profile},
	}

	g.Expect(list.Items).Should(HaveLen(1))
	g.Expect(list.Items[0].Name).Should(Equal(dashboard.TestProfile))
}
