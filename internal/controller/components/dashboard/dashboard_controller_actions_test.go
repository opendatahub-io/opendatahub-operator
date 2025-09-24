package dashboard_test

import (
	"testing"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"

	. "github.com/onsi/gomega"
)

// TestDashboardConstants tests the exported constants from the dashboard package.
func TestDashboardConstants(t *testing.T) {
	g := NewWithT(t)

	// Test ComponentName constant
	g.Expect(dashboard.ComponentName).Should(Equal(componentApi.DashboardComponentName))

	// Test ReadyConditionType constant
	g.Expect(dashboard.ReadyConditionType).Should(Equal(componentApi.DashboardKind + "Ready"))

	// Test LegacyComponentNameUpstream constant
	g.Expect(dashboard.LegacyComponentNameUpstream).Should(Equal("dashboard"))

	// Test LegacyComponentNameDownstream constant
	g.Expect(dashboard.LegacyComponentNameDownstream).Should(Equal("rhods-dashboard"))
}

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

// TestDashboardHardwareProfileVariedScenarios tests various scenarios for hardware profiles.
func TestDashboardHardwareProfileVariedScenarios(t *testing.T) {
	g := NewWithT(t)

	// Test case 1: Disabled profile (Spec.Enabled == false)
	t.Run("DisabledProfile", func(t *testing.T) {
		profile := dashboard.CreateTestDashboardHardwareProfile()
		profile.Spec.Enabled = false

		g.Expect(profile.Spec.Enabled).Should(BeFalse())
		g.Expect(profile.Name).Should(Equal(dashboard.TestProfile))
		g.Expect(profile.Namespace).Should(Equal(dashboard.TestNamespace))
		g.Expect(profile.Spec.DisplayName).Should(Equal(dashboard.TestDisplayName))
		g.Expect(profile.Spec.Description).Should(Equal(dashboard.TestDescription))
	})

	// Test case 2: Profile with empty description
	t.Run("EmptyDescription", func(t *testing.T) {
		profile := dashboard.CreateTestDashboardHardwareProfile()
		profile.Spec.Description = ""

		g.Expect(profile.Spec.Description).Should(BeEmpty())
		g.Expect(profile.Spec.Enabled).Should(BeTrue())
		g.Expect(profile.Name).Should(Equal(dashboard.TestProfile))
		g.Expect(profile.Namespace).Should(Equal(dashboard.TestNamespace))
		g.Expect(profile.Spec.DisplayName).Should(Equal(dashboard.TestDisplayName))
	})

	// Test case 3: Profile with long description
	t.Run("LongDescription", func(t *testing.T) {
		longDescription := "This is a very long description that contains multiple sentences and should test the " +
			"behavior of the DashboardHardwareProfile when dealing with extensive text content. It includes " +
			"various details about the hardware profile configuration, its intended use cases, and any " +
			"specific requirements or constraints that users should be aware of when selecting this " +
			"particular profile for their data science workloads."
		profile := dashboard.CreateTestDashboardHardwareProfile()
		profile.Spec.Description = longDescription

		g.Expect(profile.Spec.Description).Should(Equal(longDescription))
		g.Expect(len(profile.Spec.Description)).Should(BeNumerically(">", 200)) // Verify it's actually long
		g.Expect(profile.Spec.Enabled).Should(BeTrue())
		g.Expect(profile.Name).Should(Equal(dashboard.TestProfile))
		g.Expect(profile.Namespace).Should(Equal(dashboard.TestNamespace))
		g.Expect(profile.Spec.DisplayName).Should(Equal(dashboard.TestDisplayName))
	})

	// Test case 4: Multi-item DashboardHardwareProfileList with different Names/Namespaces
	t.Run("MultiItemList", func(t *testing.T) {
		// Create first profile
		profile1 := dashboard.CreateTestDashboardHardwareProfile()
		profile1.Name = "profile-1"
		profile1.Namespace = "namespace-1"
		profile1.Spec.DisplayName = "Profile One"
		profile1.Spec.Description = "First hardware profile"

		// Create second profile with different properties
		profile2 := dashboard.CreateTestDashboardHardwareProfile()
		profile2.Name = "profile-2"
		profile2.Namespace = "namespace-2"
		profile2.Spec.DisplayName = "Profile Two"
		profile2.Spec.Description = "Second hardware profile"
		profile2.Spec.Enabled = false

		// Create list with both profiles
		list := &dashboard.DashboardHardwareProfileList{
			Items: []dashboard.DashboardHardwareProfile{*profile1, *profile2},
		}

		// Assert list length
		g.Expect(list.Items).Should(HaveLen(2))

		// Assert first item fields
		g.Expect(list.Items[0].Name).Should(Equal("profile-1"))
		g.Expect(list.Items[0].Namespace).Should(Equal("namespace-1"))
		g.Expect(list.Items[0].Spec.DisplayName).Should(Equal("Profile One"))
		g.Expect(list.Items[0].Spec.Description).Should(Equal("First hardware profile"))
		g.Expect(list.Items[0].Spec.Enabled).Should(BeTrue())

		// Assert second item fields
		g.Expect(list.Items[1].Name).Should(Equal("profile-2"))
		g.Expect(list.Items[1].Namespace).Should(Equal("namespace-2"))
		g.Expect(list.Items[1].Spec.DisplayName).Should(Equal("Profile Two"))
		g.Expect(list.Items[1].Spec.Description).Should(Equal("Second hardware profile"))
		g.Expect(list.Items[1].Spec.Enabled).Should(BeFalse())
	})
}
