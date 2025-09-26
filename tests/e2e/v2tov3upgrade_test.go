package e2e_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhAnnotations "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	defaultCodeFlareComponentName = "default-codeflare"

	// HardwareProfile migration test constants
	testAcceleratorProfileName = "test-accelerator-profile"
	testAcceleratorDisplayName = "Test GPU Accelerator"
	testAcceleratorDescription = "Test accelerator for e2e testing"
	testAcceleratorIdentifier  = "nvidia.com/gpu"
	odhDashboardConfigName     = "odh-dashboard-config"

	// Default container size values (matching dashboard defaults)
	defaultNotebookSizeSmallCPURequest    = "1"
	defaultNotebookSizeSmallCPULimit      = "2"
	defaultNotebookSizeSmallMemoryRequest = "8Gi"
	defaultNotebookSizeSmallMemoryLimit   = "8Gi"

	defaultNotebookSizeMediumCPURequest    = "3"
	defaultNotebookSizeMediumCPULimit      = "6"
	defaultNotebookSizeMediumMemoryRequest = "24Gi"
	defaultNotebookSizeMediumMemoryLimit   = "24Gi"

	defaultNotebookSizeLargeCPURequest    = "7"
	defaultNotebookSizeLargeCPULimit      = "14"
	defaultNotebookSizeLargeMemoryRequest = "56Gi"
	defaultNotebookSizeLargeMemoryLimit   = "56Gi"

	defaultNotebookSizeXLargeCPURequest    = "15"
	defaultNotebookSizeXLargeCPULimit      = "30"
	defaultNotebookSizeXLargeMemoryRequest = "120Gi"
	defaultNotebookSizeXLargeMemoryLimit   = "120Gi"

	// Model server default sizes
	defaultModelServerSizeSmallCPURequest    = "2"
	defaultModelServerSizeSmallCPULimit      = "8"
	defaultModelServerSizeSmallMemoryRequest = "8Gi"
	defaultModelServerSizeSmallMemoryLimit   = "8Gi"

	defaultModelServerSizeMediumCPURequest    = "8"
	defaultModelServerSizeMediumCPULimit      = "10"
	defaultModelServerSizeMediumMemoryRequest = "10Gi"
	defaultModelServerSizeMediumMemoryLimit   = "10Gi"

	defaultModelServerSizeLargeCPURequest    = "10"
	defaultModelServerSizeLargeCPULimit      = "20"
	defaultModelServerSizeLargeMemoryRequest = "20Gi"
	defaultModelServerSizeLargeMemoryLimit   = "20Gi"
)

type V2Tov3UpgradeTestCtx struct {
	*TestContext
}

func v2Tov3UpgradeTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	// Create an instance of test context.
	v2Tov3UpgradeTestCtx := V2Tov3UpgradeTestCtx{
		TestContext: tc,
	}

	// Define test cases.
	testCases := []TestCase{
		{"codeflare resources preserved after support removal", v2Tov3UpgradeTestCtx.ValidateCodeFlareResourcePreservation},
		{"hardware profile migration from accelerator profiles", v2Tov3UpgradeTestCtx.ValidateAcceleratorProfileToHardwareProfileMigration},
		{"hardware profile migration from container sizes", v2Tov3UpgradeTestCtx.ValidateContainerSizeToHardwareProfileMigration},
		{"special hardware profile creation for inference services", v2Tov3UpgradeTestCtx.ValidateSpecialHardwareProfileCreation},
		{"hardware profile migration with custom dashboard config", v2Tov3UpgradeTestCtx.ValidateHardwareProfileMigrationWithCustomDashboardConfig},
		{"hardware profile tolerations and scheduling", v2Tov3UpgradeTestCtx.ValidateHardwareProfileTolerationsAndScheduling},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

func (tc *V2Tov3UpgradeTestCtx) ValidateCodeFlareResourcePreservation(t *testing.T) {
	t.Helper()

	tc.ValidateComponentResourcePreservation(t, gvk.CodeFlare, defaultCodeFlareComponentName)
}

func (tc *V2Tov3UpgradeTestCtx) ValidateComponentResourcePreservation(t *testing.T, componentGVK schema.GroupVersionKind, componentName string) {
	t.Helper()

	nn := types.NamespacedName{
		Name: componentName,
	}

	dsc := tc.FetchDataScienceCluster()

	tc.createOperatorManagedComponent(componentGVK, componentName, dsc)

	tc.triggerDSCReconciliation(t)

	// Verify component still exists after reconciliation (was not removed)
	tc.EnsureResourceExistsConsistently(WithMinimalObject(gvk.CodeFlare, nn),
		WithCustomErrorMsg("CodeFlare component resource '%s' was expected to exist but was not found", defaultCodeFlareComponentName),
	)

	// Cleanup
	tc.DeleteResource(
		WithMinimalObject(componentGVK, nn),
		WithWaitForDeletion(true),
	)
}

func (tc *V2Tov3UpgradeTestCtx) triggerDSCReconciliation(t *testing.T) {
	t.Helper()

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.dashboard = {}`)),
		WithCondition(jq.Match(`.metadata.generation == .status.observedGeneration`)),
		WithCustomErrorMsg("Failed to trigger DSC reconciliation"),
	)
}

func (tc *V2Tov3UpgradeTestCtx) createOperatorManagedComponent(componentGVK schema.GroupVersionKind, componentName string, dsc *dscv1.DataScienceCluster) client.Object {
	existingComponent := resources.GvkToUnstructured(componentGVK)
	existingComponent.SetName(componentName)

	resources.SetLabels(existingComponent, map[string]string{
		labels.PlatformPartOf: strings.ToLower(gvk.DataScienceCluster.Kind),
	})

	resources.SetAnnotations(existingComponent, map[string]string{
		odhAnnotations.ManagedByODHOperator: "true",
		odhAnnotations.PlatformVersion:      dsc.Status.Release.Version.String(),
		odhAnnotations.PlatformType:         string(dsc.Status.Release.Name),
		odhAnnotations.InstanceGeneration:   strconv.Itoa(int(dsc.GetGeneration())),
		odhAnnotations.InstanceUID:          string(dsc.GetUID()),
	})

	err := controllerutil.SetOwnerReference(dsc, existingComponent, tc.Scheme())
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Failed to set owner reference from DataScienceCluster '%s' to %s component '%s'",
		dsc.GetName(), componentGVK.Kind, componentName)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(existingComponent),
		WithCustomErrorMsg("Failed to create existing %s component for preservation test", componentGVK.Kind),
	)

	return existingComponent
}

// =============================================================================
// HARDWARE PROFILE MIGRATION TESTS
// =============================================================================

// ValidateAcceleratorProfileToHardwareProfileMigration tests TC1 and TC2:
// - Validates the creation of two HardwareProfiles for each AcceleratorProfile
// - Tests CPU/memory calculation with both default and custom OdhDashboardConfig
func (tc *V2Tov3UpgradeTestCtx) ValidateAcceleratorProfileToHardwareProfileMigration(t *testing.T) {
	t.Helper()

	// Create test AcceleratorProfile
	acceleratorProfile := tc.createTestAcceleratorProfile()

	// Create default OdhDashboardConfig if it doesn't exist
	tc.ensureDefaultOdhDashboardConfig()

	// Trigger migration by updating DSC (simulating upgrade)
	tc.triggerHardwareProfileMigration(t)

	// Validate notebook HardwareProfile creation
	tc.validateNotebookHardwareProfileFromAcceleratorProfile(acceleratorProfile)

	// Validate serving HardwareProfile creation
	tc.validateServingHardwareProfileFromAcceleratorProfile(acceleratorProfile)

	// Cleanup
	tc.cleanupTestAcceleratorProfile(acceleratorProfile)
}

// ValidateContainerSizeToHardwareProfileMigration tests TC3:
// - Validates creation of HardwareProfiles for each container size
func (tc *V2Tov3UpgradeTestCtx) ValidateContainerSizeToHardwareProfileMigration(t *testing.T) {
	t.Helper()

	// Ensure default OdhDashboardConfig exists
	tc.ensureDefaultOdhDashboardConfig()

	// Trigger migration
	tc.triggerHardwareProfileMigration(t)

	// Validate HardwareProfiles for notebook sizes
	tc.validateNotebookContainerSizeHardwareProfiles()

	// Validate HardwareProfiles for model server sizes
	tc.validateModelServerContainerSizeHardwareProfiles()
}

// ValidateSpecialHardwareProfileCreation tests TC4:
// - Validates creation of the special custom-serving HardwareProfile
func (tc *V2Tov3UpgradeTestCtx) ValidateSpecialHardwareProfileCreation(t *testing.T) {
	t.Helper()

	// Ensure default OdhDashboardConfig exists
	tc.ensureDefaultOdhDashboardConfig()

	// Trigger migration
	tc.triggerHardwareProfileMigration(t)

	// Validate special custom-serving HardwareProfile
	tc.validateSpecialCustomServingHardwareProfile()
}

// ValidateHardwareProfileMigrationWithCustomDashboardConfig tests TC2:
// - Tests CPU/memory calculation with custom OdhDashboardConfig values
func (tc *V2Tov3UpgradeTestCtx) ValidateHardwareProfileMigrationWithCustomDashboardConfig(t *testing.T) {
	t.Helper()

	// Create custom OdhDashboardConfig with different container sizes
	customConfig := tc.createCustomOdhDashboardConfig()

	// Create test AcceleratorProfile
	acceleratorProfile := tc.createTestAcceleratorProfile()

	// Trigger migration
	tc.triggerHardwareProfileMigration(t)

	// Validate that HardwareProfiles use the custom container sizes for calculation
	tc.validateCustomContainerSizeCalculations(acceleratorProfile, customConfig)

	// Cleanup
	tc.cleanupTestAcceleratorProfile(acceleratorProfile)
	tc.cleanupCustomOdhDashboardConfig(customConfig)
}

// ValidateHardwareProfileTolerationsAndScheduling tests toleration handling:
// - Tests notebook HWP includes both AP tolerations and notebook-only tolerations
// - Tests serving HWP includes only AP tolerations
func (tc *V2Tov3UpgradeTestCtx) ValidateHardwareProfileTolerationsAndScheduling(t *testing.T) {
	t.Helper()

	// Create AcceleratorProfile with tolerations
	acceleratorProfile := tc.createTestAcceleratorProfileWithTolerations()

	// Create OdhDashboardConfig with notebook tolerations enabled
	dashboardConfig := tc.createOdhDashboardConfigWithNotebookTolerations()

	// Trigger migration
	tc.triggerHardwareProfileMigration(t)

	// Validate tolerations in notebook HardwareProfile
	tc.validateNotebookHardwareProfileTolerations(acceleratorProfile)

	// Validate tolerations in serving HardwareProfile
	tc.validateServingHardwareProfileTolerations(acceleratorProfile)

	// Cleanup
	tc.cleanupTestAcceleratorProfile(acceleratorProfile)
	tc.cleanupCustomOdhDashboardConfig(dashboardConfig)
}

// =============================================================================
// HELPER METHODS FOR HARDWARE PROFILE MIGRATION TESTS
// =============================================================================

func (tc *V2Tov3UpgradeTestCtx) createTestAcceleratorProfile() *unstructured.Unstructured {
	acceleratorProfile := &unstructured.Unstructured{}
	acceleratorProfile.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "dashboard.opendatahub.io",
		Version: "v1",
		Kind:    "AcceleratorProfile",
	})
	acceleratorProfile.SetName(testAcceleratorProfileName)
	acceleratorProfile.SetNamespace(tc.AppsNamespace)

	// Set spec fields according to AcceleratorProfile schema
	spec := map[string]interface{}{
		"displayName": testAcceleratorDisplayName,
		"description": testAcceleratorDescription,
		"identifier":  testAcceleratorIdentifier,
		"enabled":     true,
	}
	acceleratorProfile.Object["spec"] = spec

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(acceleratorProfile),
		WithEventuallyTimeout(tc.TestTimeouts.shortEventuallyTimeout),
		WithCustomErrorMsg("Failed to create test AcceleratorProfile"),
	)

	return acceleratorProfile
}

func (tc *V2Tov3UpgradeTestCtx) createTestAcceleratorProfileWithTolerations() *unstructured.Unstructured {
	acceleratorProfile := tc.createTestAcceleratorProfile()

	// Add tolerations to the AcceleratorProfile
	tolerations := []map[string]interface{}{
		{
			"key":      "nvidia.com/gpu",
			"operator": "Equal",
			"value":    "true",
			"effect":   "NoSchedule",
		},
		{
			"key":      "accelerator",
			"operator": "Equal",
			"value":    "gpu",
			"effect":   "NoExecute",
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithFetchedObject(acceleratorProfile.GroupVersionKind(),
			types.NamespacedName{Name: acceleratorProfile.GetName(), Namespace: acceleratorProfile.GetNamespace()}),
		WithMutateFunc(testf.Transform(`.spec.tolerations = %v`, tolerations)),
		WithCustomErrorMsg("Failed to add tolerations to AcceleratorProfile"),
	)

	return acceleratorProfile
}

func (tc *V2Tov3UpgradeTestCtx) ensureDefaultOdhDashboardConfig() *unstructured.Unstructured {
	dashboardConfig := tc.createDefaultOdhDashboardConfig()

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(dashboardConfig),
		WithCustomErrorMsg("Failed to create default OdhDashboardConfig"),
	)

	return dashboardConfig
}

func (tc *V2Tov3UpgradeTestCtx) createDefaultOdhDashboardConfig() *unstructured.Unstructured {
	dashboardConfig := &unstructured.Unstructured{}
	dashboardConfig.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "opendatahub.io",
		Version: "v1alpha",
		Kind:    "OdhDashboardConfig",
	})
	dashboardConfig.SetName(odhDashboardConfigName)
	dashboardConfig.SetNamespace(tc.AppsNamespace)

	// Create default notebook sizes
	notebookSizes := []map[string]interface{}{
		{
			"name": "Small",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"cpu":    defaultNotebookSizeSmallCPURequest,
					"memory": defaultNotebookSizeSmallMemoryRequest,
				},
				"limits": map[string]interface{}{
					"cpu":    defaultNotebookSizeSmallCPULimit,
					"memory": defaultNotebookSizeSmallMemoryLimit,
				},
			},
		},
		{
			"name": "Medium",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"cpu":    defaultNotebookSizeMediumCPURequest,
					"memory": defaultNotebookSizeMediumMemoryRequest,
				},
				"limits": map[string]interface{}{
					"cpu":    defaultNotebookSizeMediumCPULimit,
					"memory": defaultNotebookSizeMediumMemoryLimit,
				},
			},
		},
		{
			"name": "Large",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"cpu":    defaultNotebookSizeLargeCPURequest,
					"memory": defaultNotebookSizeLargeMemoryRequest,
				},
				"limits": map[string]interface{}{
					"cpu":    defaultNotebookSizeLargeCPULimit,
					"memory": defaultNotebookSizeLargeMemoryLimit,
				},
			},
		},
		{
			"name": "X Large",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"cpu":    defaultNotebookSizeXLargeCPURequest,
					"memory": defaultNotebookSizeXLargeMemoryRequest,
				},
				"limits": map[string]interface{}{
					"cpu":    defaultNotebookSizeXLargeCPULimit,
					"memory": defaultNotebookSizeXLargeMemoryLimit,
				},
			},
		},
	}

	// Create default model server sizes
	modelServerSizes := []map[string]interface{}{
		{
			"name": "Small",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"cpu":    defaultModelServerSizeSmallCPURequest,
					"memory": defaultModelServerSizeSmallMemoryRequest,
				},
				"limits": map[string]interface{}{
					"cpu":    defaultModelServerSizeSmallCPULimit,
					"memory": defaultModelServerSizeSmallMemoryLimit,
				},
			},
		},
		{
			"name": "Medium",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"cpu":    defaultModelServerSizeMediumCPURequest,
					"memory": defaultModelServerSizeMediumMemoryRequest,
				},
				"limits": map[string]interface{}{
					"cpu":    defaultModelServerSizeMediumCPULimit,
					"memory": defaultModelServerSizeMediumMemoryLimit,
				},
			},
		},
		{
			"name": "Large",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"cpu":    defaultModelServerSizeLargeCPURequest,
					"memory": defaultModelServerSizeLargeMemoryRequest,
				},
				"limits": map[string]interface{}{
					"cpu":    defaultModelServerSizeLargeCPULimit,
					"memory": defaultModelServerSizeLargeMemoryLimit,
				},
			},
		},
	}

	spec := map[string]interface{}{
		"notebookSizes":    notebookSizes,
		"modelServerSizes": modelServerSizes,
		"notebookController": map[string]interface{}{
			"enabled": true,
			"notebookTolerationSettings": map[string]interface{}{
				"enabled": false,
			},
		},
	}

	dashboardConfig.Object["spec"] = spec
	return dashboardConfig
}

func (tc *V2Tov3UpgradeTestCtx) createCustomOdhDashboardConfig() *unstructured.Unstructured {
	dashboardConfig := tc.createDefaultOdhDashboardConfig()
	dashboardConfig.SetName("custom-" + odhDashboardConfigName)

	// Customize container sizes for testing
	customNotebookSizes := []map[string]interface{}{
		{
			"name": "Custom Small",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"cpu":    "500m",
					"memory": "4Gi",
				},
				"limits": map[string]interface{}{
					"cpu":    "1",
					"memory": "4Gi",
				},
			},
		},
		{
			"name": "Custom Large",
			"resources": map[string]interface{}{
				"requests": map[string]interface{}{
					"cpu":    "4",
					"memory": "16Gi",
				},
				"limits": map[string]interface{}{
					"cpu":    "8",
					"memory": "32Gi",
				},
			},
		},
	}

	spec := dashboardConfig.Object["spec"].(map[string]interface{})
	spec["notebookSizes"] = customNotebookSizes

	return dashboardConfig
}

func (tc *V2Tov3UpgradeTestCtx) createOdhDashboardConfigWithNotebookTolerations() *unstructured.Unstructured {
	dashboardConfig := tc.createDefaultOdhDashboardConfig()
	dashboardConfig.SetName("tolerations-" + odhDashboardConfigName)

	// Enable notebook tolerations
	spec := dashboardConfig.Object["spec"].(map[string]interface{})
	notebookController := spec["notebookController"].(map[string]interface{})
	notebookController["enabled"] = true

	tolerationSettings := map[string]interface{}{
		"enabled":  true,
		"key":      "notebook-only",
		"operator": "Equal",
		"value":    "true",
		"effect":   "NoSchedule",
	}
	notebookController["notebookTolerationSettings"] = tolerationSettings

	return dashboardConfig
}

func (tc *V2Tov3UpgradeTestCtx) triggerHardwareProfileMigration(t *testing.T) {
	t.Helper()

	// Trigger DSC reconciliation to simulate upgrade and migration
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.status.release.version = "%s"`,
			"3.0.0")),
		WithCondition(jq.Match(`.metadata.generation == .status.observedGeneration`)),
		WithCustomErrorMsg("Failed to trigger HardwareProfile migration"),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
	)
}

func (tc *V2Tov3UpgradeTestCtx) validateNotebookHardwareProfileFromAcceleratorProfile(acceleratorProfile *unstructured.Unstructured) {
	expectedName := fmt.Sprintf("%s-notebooks", acceleratorProfile.GetName())

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HardwareProfile,
			types.NamespacedName{Name: expectedName, Namespace: acceleratorProfile.GetNamespace()}),
		WithCondition(And(
			// Validate basic metadata
			jq.Match(`.metadata.name == "%s"`, expectedName),
			jq.Match(`.metadata.namespace == "%s"`, acceleratorProfile.GetNamespace()),

			// Validate annotations
			jq.Match(`.metadata.annotations["opendatahub.io/dashboard-feature-visibility"] == "workbench"`),
			jq.Match(`.metadata.annotations["opendatahub.io/display-name"] == "%s"`, testAcceleratorDisplayName),
			jq.Match(`.metadata.annotations["opendatahub.io/description"] == "%s"`, testAcceleratorDescription),
			jq.Match(`.metadata.annotations["opendatahub.io/disabled"] == "false"`),
			jq.Match(`.metadata.annotations | has("opendatahub.io/modified-date")`),

			// Validate accelerator identifier
			jq.Match(`.spec.identifiers[0].identifier == "%s"`, testAcceleratorIdentifier),
			jq.Match(`.spec.identifiers[0].displayName == "%s"`, testAcceleratorIdentifier),
			jq.Match(`.spec.identifiers[0].resourceType == "Accelerator"`),
			jq.Match(`.spec.identifiers[0].minCount == 1`),
			jq.Match(`.spec.identifiers[0].defaultCount == 1`),

			// Validate CPU identifier (calculated from notebook container sizes)
			jq.Match(`.spec.identifiers[1].identifier == "cpu"`),
			jq.Match(`.spec.identifiers[1].resourceType == "CPU"`),
			jq.Match(`.spec.identifiers[1].minCount == "1"`),  // Min from default notebook sizes
			jq.Match(`.spec.identifiers[1].maxCount == "30"`), // Max from default notebook sizes
			jq.Match(`.spec.identifiers[1].defaultCount == "1"`),

			// Validate memory identifier (calculated from notebook container sizes)
			jq.Match(`.spec.identifiers[2].identifier == "memory"`),
			jq.Match(`.spec.identifiers[2].resourceType == "Memory"`),
			jq.Match(`.spec.identifiers[2].minCount == "8Gi"`),   // Min from default notebook sizes
			jq.Match(`.spec.identifiers[2].maxCount == "120Gi"`), // Max from default notebook sizes
			jq.Match(`.spec.identifiers[2].defaultCount == "8Gi"`),
		)),
		WithCustomErrorMsg("Notebook HardwareProfile validation failed for AcceleratorProfile %s", acceleratorProfile.GetName()),
	)
}

func (tc *V2Tov3UpgradeTestCtx) validateServingHardwareProfileFromAcceleratorProfile(acceleratorProfile *unstructured.Unstructured) {
	expectedName := fmt.Sprintf("%s-serving", acceleratorProfile.GetName())

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HardwareProfile,
			types.NamespacedName{Name: expectedName, Namespace: acceleratorProfile.GetNamespace()}),
		WithCondition(And(
			// Validate basic metadata
			jq.Match(`.metadata.name == "%s"`, expectedName),
			jq.Match(`.metadata.namespace == "%s"`, acceleratorProfile.GetNamespace()),

			// Validate annotations
			jq.Match(`.metadata.annotations["opendatahub.io/dashboard-feature-visibility"] == "model-serving"`),
			jq.Match(`.metadata.annotations["opendatahub.io/display-name"] == "%s"`, testAcceleratorDisplayName),
			jq.Match(`.metadata.annotations["opendatahub.io/description"] == "%s"`, testAcceleratorDescription),
			jq.Match(`.metadata.annotations["opendatahub.io/disabled"] == "false"`),

			// Validate accelerator identifier
			jq.Match(`.spec.identifiers[0].identifier == "%s"`, testAcceleratorIdentifier),
			jq.Match(`.spec.identifiers[0].resourceType == "Accelerator"`),

			// Validate CPU identifier (fixed values for serving)
			jq.Match(`.spec.identifiers[1].identifier == "cpu"`),
			jq.Match(`.spec.identifiers[1].resourceType == "CPU"`),
			jq.Match(`.spec.identifiers[1].minCount == "1"`),
			jq.Match(`.spec.identifiers[1].defaultCount == "1"`),
			jq.Match(`.spec.identifiers[1] | has("maxCount") | not`), // No maxCount for serving

			// Validate memory identifier (fixed values for serving)
			jq.Match(`.spec.identifiers[2].identifier == "memory"`),
			jq.Match(`.spec.identifiers[2].resourceType == "Memory"`),
			jq.Match(`.spec.identifiers[2].minCount == "1Gi"`),
			jq.Match(`.spec.identifiers[2].defaultCount == "1Gi"`),
			jq.Match(`.spec.identifiers[2] | has("maxCount") | not`), // No maxCount for serving
		)),
		WithCustomErrorMsg("Serving HardwareProfile validation failed for AcceleratorProfile %s", acceleratorProfile.GetName()),
	)
}

func (tc *V2Tov3UpgradeTestCtx) validateNotebookContainerSizeHardwareProfiles() {
	containerSizes := []string{"Small", "Medium", "Large", "X Large"}

	for _, size := range containerSizes {
		expectedName := fmt.Sprintf("containersize-%s-notebooks", strings.ToLower(strings.ReplaceAll(size, " ", "")))

		tc.EnsureResourceExists(
			WithMinimalObject(gvk.HardwareProfile,
				types.NamespacedName{Name: expectedName, Namespace: tc.AppsNamespace}),
			WithCondition(And(
				// Validate metadata
				jq.Match(`.metadata.name == "%s"`, expectedName),
				jq.Match(`.metadata.annotations["opendatahub.io/dashboard-feature-visibility"] == "workbench"`),
				jq.Match(`.metadata.annotations["opendatahub.io/display-name"] == "%s"`, size),
				jq.Match(`.metadata.annotations["opendatahub.io/disabled"] == "false"`),

				// Validate CPU and memory identifiers exist
				jq.Match(`.spec.identifiers[0].identifier == "cpu"`),
				jq.Match(`.spec.identifiers[0].resourceType == "CPU"`),
				jq.Match(`.spec.identifiers[1].identifier == "memory"`),
				jq.Match(`.spec.identifiers[1].resourceType == "Memory"`),

				// No accelerator identifier for container size HWPs
				jq.Match(`.spec.identifiers | map(.resourceType) | contains(["Accelerator"]) | not`),
			)),
			WithCustomErrorMsg("Notebook container size HardwareProfile validation failed for size %s", size),
		)
	}
}

func (tc *V2Tov3UpgradeTestCtx) validateModelServerContainerSizeHardwareProfiles() {
	containerSizes := []string{"Small", "Medium", "Large"}

	for _, size := range containerSizes {
		expectedName := fmt.Sprintf("containersize-%s-serving", strings.ToLower(size))

		tc.EnsureResourceExists(
			WithMinimalObject(gvk.HardwareProfile,
				types.NamespacedName{Name: expectedName, Namespace: tc.AppsNamespace}),
			WithCondition(And(
				// Validate metadata
				jq.Match(`.metadata.name == "%s"`, expectedName),
				jq.Match(`.metadata.annotations["opendatahub.io/dashboard-feature-visibility"] == "model-serving"`),
				jq.Match(`.metadata.annotations["opendatahub.io/display-name"] == "%s"`, size),
				jq.Match(`.metadata.annotations["opendatahub.io/disabled"] == "false"`),

				// Validate CPU and memory identifiers exist
				jq.Match(`.spec.identifiers[0].identifier == "cpu"`),
				jq.Match(`.spec.identifiers[0].resourceType == "CPU"`),
				jq.Match(`.spec.identifiers[1].identifier == "memory"`),
				jq.Match(`.spec.identifiers[1].resourceType == "Memory"`),

				// No accelerator identifier for container size HWPs
				jq.Match(`.spec.identifiers | map(.resourceType) | contains(["Accelerator"]) | not`),
			)),
			WithCustomErrorMsg("Model server container size HardwareProfile validation failed for size %s", size),
		)
	}
}

func (tc *V2Tov3UpgradeTestCtx) validateSpecialCustomServingHardwareProfile() {
	expectedName := "custom-serving"

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HardwareProfile,
			types.NamespacedName{Name: expectedName, Namespace: tc.AppsNamespace}),
		WithCondition(And(
			// Validate metadata
			jq.Match(`.metadata.name == "%s"`, expectedName),
			jq.Match(`.metadata.annotations["opendatahub.io/dashboard-feature-visibility"] == "model-serving"`),
			jq.Match(`.metadata.annotations["opendatahub.io/display-name"] == "%s"`, expectedName),
			jq.Match(`.metadata.annotations["opendatahub.io/disabled"] == "false"`),

			// Validate CPU identifier
			jq.Match(`.spec.identifiers[0].identifier == "cpu"`),
			jq.Match(`.spec.identifiers[0].resourceType == "CPU"`),
			jq.Match(`.spec.identifiers[0].minCount == "1"`),
			jq.Match(`.spec.identifiers[0].defaultCount == "1"`),
			jq.Match(`.spec.identifiers[0] | has("maxCount") | not`), // No maxCount

			// Validate memory identifier
			jq.Match(`.spec.identifiers[1].identifier == "memory"`),
			jq.Match(`.spec.identifiers[1].resourceType == "Memory"`),
			jq.Match(`.spec.identifiers[1].minCount == "1Gi"`),
			jq.Match(`.spec.identifiers[1].defaultCount == "1Gi"`),
			jq.Match(`.spec.identifiers[1] | has("maxCount") | not`), // No maxCount

			// No accelerator identifier (special case)
			jq.Match(`.spec.identifiers | map(.resourceType) | contains(["Accelerator"]) | not`),
		)),
		WithCustomErrorMsg("Special custom-serving HardwareProfile validation failed"),
	)
}

func (tc *V2Tov3UpgradeTestCtx) validateCustomContainerSizeCalculations(acceleratorProfile *unstructured.Unstructured, customConfig *unstructured.Unstructured) {
	expectedName := fmt.Sprintf("%s-notebooks", acceleratorProfile.GetName())

	// With custom config, CPU limits should be calculated from the custom container sizes:
	// Custom Small: 500m-1, Custom Large: 4-8, so min=500m, max=8
	// Custom Small: 4Gi-4Gi, Custom Large: 16Gi-32Gi, so min=4Gi, max=32Gi
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HardwareProfile,
			types.NamespacedName{Name: expectedName, Namespace: acceleratorProfile.GetNamespace()}),
		WithCondition(And(
			// Validate CPU limits calculated from custom config
			jq.Match(`.spec.identifiers[1].minCount == "500m"`), // Min from custom small
			jq.Match(`.spec.identifiers[1].maxCount == "8"`),    // Max from custom large
			jq.Match(`.spec.identifiers[1].defaultCount == "500m"`),

			// Validate memory limits calculated from custom config
			jq.Match(`.spec.identifiers[2].minCount == "4Gi"`),  // Min from custom small
			jq.Match(`.spec.identifiers[2].maxCount == "32Gi"`), // Max from custom large
			jq.Match(`.spec.identifiers[2].defaultCount == "4Gi"`),
		)),
		WithCustomErrorMsg("Custom container size calculation validation failed"),
	)
}

func (tc *V2Tov3UpgradeTestCtx) validateNotebookHardwareProfileTolerations(acceleratorProfile *unstructured.Unstructured) {
	expectedName := fmt.Sprintf("%s-notebooks", acceleratorProfile.GetName())

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HardwareProfile,
			types.NamespacedName{Name: expectedName, Namespace: acceleratorProfile.GetNamespace()}),
		WithCondition(And(
			// Validate scheduling section exists
			jq.Match(`.spec | has("scheduling")`),
			jq.Match(`.spec.scheduling.type == "Node"`),

			// Validate tolerations include both AP tolerations and notebook-only tolerations
			jq.Match(`.spec.scheduling.node.tolerations | length >= 3`), // AP tolerations + notebook toleration

			// Check for original AP tolerations
			jq.Match(`.spec.scheduling.node.tolerations[] | select(.key == "nvidia.com/gpu")`),
			jq.Match(`.spec.scheduling.node.tolerations[] | select(.key == "accelerator")`),

			// Check for notebook-only toleration
			jq.Match(`.spec.scheduling.node.tolerations[] | select(.key == "notebook-only")`),
		)),
		WithCustomErrorMsg("Notebook HardwareProfile tolerations validation failed"),
	)
}

func (tc *V2Tov3UpgradeTestCtx) validateServingHardwareProfileTolerations(acceleratorProfile *unstructured.Unstructured) {
	expectedName := fmt.Sprintf("%s-serving", acceleratorProfile.GetName())

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HardwareProfile,
			types.NamespacedName{Name: expectedName, Namespace: acceleratorProfile.GetNamespace()}),
		WithCondition(And(
			// Validate scheduling section exists
			jq.Match(`.spec | has("scheduling")`),
			jq.Match(`.spec.scheduling.type == "Node"`),

			// Validate tolerations include only AP tolerations (no notebook-only)
			jq.Match(`.spec.scheduling.node.tolerations | length == 2`), // Only AP tolerations

			// Check for original AP tolerations
			jq.Match(`.spec.scheduling.node.tolerations[] | select(.key == "nvidia.com/gpu")`),
			jq.Match(`.spec.scheduling.node.tolerations[] | select(.key == "accelerator")`),

			// Should NOT have notebook-only toleration
			jq.Match(`.spec.scheduling.node.tolerations[] | select(.key == "notebook-only") | not`),
		)),
		WithCustomErrorMsg("Serving HardwareProfile tolerations validation failed"),
	)
}

// =============================================================================
// CLEANUP HELPER METHODS
// =============================================================================

func (tc *V2Tov3UpgradeTestCtx) cleanupTestAcceleratorProfile(acceleratorProfile *unstructured.Unstructured) {
	tc.DeleteResource(
		WithMinimalObject(acceleratorProfile.GroupVersionKind(),
			types.NamespacedName{Name: acceleratorProfile.GetName(), Namespace: acceleratorProfile.GetNamespace()}),
		WithWaitForDeletion(true),
		WithIgnoreNotFound(true),
	)

	// Also cleanup the generated HardwareProfiles
	notebookHWPName := fmt.Sprintf("%s-notebooks", acceleratorProfile.GetName())
	servingHWPName := fmt.Sprintf("%s-serving", acceleratorProfile.GetName())

	tc.DeleteResource(
		WithMinimalObject(gvk.HardwareProfile,
			types.NamespacedName{Name: notebookHWPName, Namespace: acceleratorProfile.GetNamespace()}),
		WithWaitForDeletion(true),
		WithIgnoreNotFound(true),
	)

	tc.DeleteResource(
		WithMinimalObject(gvk.HardwareProfile,
			types.NamespacedName{Name: servingHWPName, Namespace: acceleratorProfile.GetNamespace()}),
		WithWaitForDeletion(true),
		WithIgnoreNotFound(true),
	)
}

func (tc *V2Tov3UpgradeTestCtx) cleanupCustomOdhDashboardConfig(dashboardConfig *unstructured.Unstructured) {
	tc.DeleteResource(
		WithMinimalObject(dashboardConfig.GroupVersionKind(),
			types.NamespacedName{Name: dashboardConfig.GetName(), Namespace: dashboardConfig.GetNamespace()}),
		WithWaitForDeletion(true),
		WithIgnoreNotFound(true),
	)
}
