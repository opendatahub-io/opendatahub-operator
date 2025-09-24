package dashboard_test

import (
	"os"
	"testing"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const (
	dashboardURLKey = "dashboard-url"
	sectionTitleKey = "section-title"
	testNamespace   = "test-namespace"
	testDashboard   = "test-dashboard"
	testPath        = "/test/path"

	// Test condition types - these should match the ones in dashboard_support.go.
	testConditionTypes = status.ConditionDeploymentsAvailable
)

func TestDefaultManifestInfo(t *testing.T) {
	tests := []struct {
		name     string
		platform common.Platform
		expected string
	}{
		{
			name:     "OpenDataHub platform",
			platform: cluster.OpenDataHub,
			expected: "/odh",
		},
		{
			name:     "SelfManagedRhoai platform",
			platform: cluster.SelfManagedRhoai,
			expected: "/rhoai/onprem",
		},
		{
			name:     "ManagedRhoai platform",
			platform: cluster.ManagedRhoai,
			expected: "/rhoai/addon",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			manifestInfo := dashboard.DefaultManifestInfo(tt.platform)
			g.Expect(manifestInfo.ContextDir).Should(Equal(dashboard.ComponentName))
			g.Expect(manifestInfo.SourcePath).Should(Equal(tt.expected))
		})
	}
}

func TestBffManifestsPath(t *testing.T) {
	g := NewWithT(t)
	manifestInfo := dashboard.BffManifestsPath()
	g.Expect(manifestInfo.ContextDir).Should(Equal(dashboard.ComponentName))
	g.Expect(manifestInfo.SourcePath).Should(Equal("modular-architecture"))
}

func TestComputeKustomizeVariable(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create a mock client with a console route
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "console",
			Namespace: "openshift-console",
		},
		Spec: routev1.RouteSpec{
			Host: "console-openshift-console.apps.example.com",
		},
	}

	// Create the OpenShift ingress resource that cluster.GetDomain() needs
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")
	ingress.SetNamespace("")

	// Set the domain in the spec
	err := unstructured.SetNestedField(ingress.Object, "apps.example.com", "spec", "domain")
	g.Expect(err).ShouldNot(HaveOccurred())

	cli, err := fakeclient.New(fakeclient.WithObjects(route, ingress))
	g.Expect(err).ShouldNot(HaveOccurred())

	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: testNamespace,
		},
	}

	tests := []struct {
		name     string
		platform common.Platform
		expected map[string]string
	}{
		{
			name:     "OpenDataHub platform",
			platform: cluster.OpenDataHub,
			expected: map[string]string{
				dashboardURLKey: "https://odh-dashboard-test-namespace.apps.example.com",
				sectionTitleKey: "OpenShift Open Data Hub",
			},
		},
		{
			name:     "SelfManagedRhoai platform",
			platform: cluster.SelfManagedRhoai,
			expected: map[string]string{
				dashboardURLKey: "https://rhods-dashboard-test-namespace.apps.example.com",
				sectionTitleKey: "OpenShift Self Managed Services",
			},
		},
		{
			name:     "ManagedRhoai platform",
			platform: cluster.ManagedRhoai,
			expected: map[string]string{
				dashboardURLKey: "https://rhods-dashboard-test-namespace.apps.example.com",
				sectionTitleKey: "OpenShift Managed Services",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			variables, err := dashboard.ComputeKustomizeVariable(ctx, cli, tt.platform, &dsci.Spec)
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(variables).Should(Equal(tt.expected))
		})
	}
}

func TestComputeKustomizeVariableNoConsoleRoute(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: testNamespace,
		},
	}

	_, err = dashboard.ComputeKustomizeVariable(ctx, cli, cluster.OpenDataHub, &dsci.Spec)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("error getting console route URL"))
}

// TestComputeComponentNameBasic tests basic functionality and determinism with table-driven approach.
func TestComputeComponentNameBasic(t *testing.T) {
	tests := []struct {
		name      string
		callCount int
	}{
		{
			name:      "SingleCall",
			callCount: 1,
		},
		{
			name:      "MultipleCalls",
			callCount: 5,
		},
		{
			name:      "Stability",
			callCount: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Test that the function returns valid component names
			names := make([]string, tt.callCount)
			for i := range tt.callCount {
				names[i] = dashboard.ComputeComponentName()
			}

			// All calls should return non-empty names
			for i, name := range names {
				g.Expect(name).ShouldNot(BeEmpty(), "Call %d should return non-empty name", i)
				g.Expect(name).Should(Or(
					Equal(dashboard.LegacyComponentNameUpstream),
					Equal(dashboard.LegacyComponentNameDownstream),
				), "Call %d should return valid component name", i)
				g.Expect(len(name)).Should(BeNumerically("<", 50), "Call %d should return reasonable length name", i)
			}

			// All calls should return identical results (determinism)
			if tt.callCount > 1 {
				for i := 1; i < len(names); i++ {
					g.Expect(names[i]).Should(Equal(names[0]), "All calls should return identical results")
				}
			}
		})
	}
}

func TestInit(t *testing.T) {
	g := NewWithT(t)

	handler := &dashboard.ComponentHandler{}

	// Test successful initialization for different platforms
	platforms := []common.Platform{
		cluster.OpenDataHub,
		cluster.SelfManagedRhoai,
		cluster.ManagedRhoai,
	}

	for _, platform := range platforms {
		t.Run(string(platform), func(t *testing.T) {
			err := handler.Init(platform)
			g.Expect(err).ShouldNot(HaveOccurred())
		})
	}
}

func TestSectionTitleMapping(t *testing.T) {
	g := NewWithT(t)

	expectedMappings := map[common.Platform]string{
		cluster.SelfManagedRhoai: "OpenShift Self Managed Services",
		cluster.ManagedRhoai:     "OpenShift Managed Services",
		cluster.OpenDataHub:      "OpenShift Open Data Hub",
	}

	for platform, expectedTitle := range expectedMappings {
		g.Expect(dashboard.SectionTitle[platform]).Should(Equal(expectedTitle))
	}
}

func TestBaseConsoleURLMapping(t *testing.T) {
	g := NewWithT(t)

	expectedMappings := map[common.Platform]string{
		cluster.SelfManagedRhoai: "https://rhods-dashboard-",
		cluster.ManagedRhoai:     "https://rhods-dashboard-",
		cluster.OpenDataHub:      "https://odh-dashboard-",
	}

	for platform, expectedURL := range expectedMappings {
		g.Expect(dashboard.BaseConsoleURL[platform]).Should(Equal(expectedURL))
	}
}

func TestOverlaysSourcePathsMapping(t *testing.T) {
	g := NewWithT(t)

	expectedMappings := map[common.Platform]string{
		cluster.SelfManagedRhoai: "/rhoai/onprem",
		cluster.ManagedRhoai:     "/rhoai/addon",
		cluster.OpenDataHub:      "/odh",
	}

	for platform, expectedPath := range expectedMappings {
		g.Expect(dashboard.OverlaysSourcePaths[platform]).Should(Equal(expectedPath))
	}
}

func TestImagesMap(t *testing.T) {
	g := NewWithT(t)

	expectedImages := map[string]string{
		"odh-dashboard-image":     "RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
		"model-registry-ui-image": "RELATED_IMAGE_ODH_MOD_ARCH_MODEL_REGISTRY_IMAGE",
	}

	for key, expectedValue := range expectedImages {
		g.Expect(dashboard.ImagesMap[key]).Should(Equal(expectedValue))
	}
}

func TestConditionTypes(t *testing.T) {
	g := NewWithT(t)

	expectedConditions := []string{
		status.ConditionDeploymentsAvailable,
	}

	g.Expect(testConditionTypes).Should(Equal(expectedConditions[0]))
}

func TestComponentNameConstant(t *testing.T) {
	g := NewWithT(t)
	g.Expect(dashboard.ComponentName).Should(Equal(componentApi.DashboardComponentName))
}

func TestReadyConditionType(t *testing.T) {
	g := NewWithT(t)
	expectedConditionType := componentApi.DashboardKind + status.ReadySuffix
	g.Expect(dashboard.ReadyConditionType).Should(Equal(expectedConditionType))
}

func TestLegacyComponentNames(t *testing.T) {
	g := NewWithT(t)
	g.Expect(dashboard.LegacyComponentNameUpstream).Should(Equal("dashboard"))
	g.Expect(dashboard.LegacyComponentNameDownstream).Should(Equal("rhods-dashboard"))
}

// Test helper functions for creating test objects.
func TestCreateTestObjects(t *testing.T) {
	g := NewWithT(t)

	// Test creating a dashboard instance
	dashboard := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDashboard,
			Namespace: testNamespace,
		},
		Spec: componentApi.DashboardSpec{
			DashboardCommonSpec: componentApi.DashboardCommonSpec{
				DevFlagsSpec: common.DevFlagsSpec{
					DevFlags: &common.DevFlags{
						Manifests: []common.ManifestsConfig{
							{
								SourcePath: "/custom/path",
							},
						},
					},
				},
			},
		},
	}

	g.Expect(dashboard.Name).Should(Equal(testDashboard))
	g.Expect(dashboard.Namespace).Should(Equal(testNamespace))
	g.Expect(dashboard.Spec.DevFlags).ShouldNot(BeNil())
	g.Expect(dashboard.Spec.DevFlags.Manifests).Should(HaveLen(1))
	g.Expect(dashboard.Spec.DevFlags.Manifests[0].SourcePath).Should(Equal("/custom/path"))
}

func TestManifestInfoStructure(t *testing.T) {
	g := NewWithT(t)

	manifestInfo := odhtypes.ManifestInfo{
		Path:       dashboard.TestPath,
		ContextDir: dashboard.ComponentName,
		SourcePath: "/odh",
	}

	g.Expect(manifestInfo.Path).Should(Equal(dashboard.TestPath))
	g.Expect(manifestInfo.ContextDir).Should(Equal(dashboard.ComponentName))
	g.Expect(manifestInfo.SourcePath).Should(Equal("/odh"))
}

func TestPlatformConstants(t *testing.T) {
	g := NewWithT(t)

	// Test that platform constants are defined correctly
	g.Expect(cluster.OpenDataHub).Should(Equal(common.Platform("Open Data Hub")))
	g.Expect(cluster.SelfManagedRhoai).Should(Equal(common.Platform("OpenShift AI Self-Managed")))
	g.Expect(cluster.ManagedRhoai).Should(Equal(common.Platform("OpenShift AI Cloud Service")))
}

func TestComponentAPIConstants(t *testing.T) {
	g := NewWithT(t)

	// Test that component API constants are defined correctly
	g.Expect(componentApi.DashboardComponentName).Should(Equal("dashboard"))
	g.Expect(componentApi.DashboardKind).Should(Equal("Dashboard"))
	g.Expect(componentApi.DashboardInstanceName).Should(Equal("default-dashboard"))
}

func TestStatusConstants(t *testing.T) {
	g := NewWithT(t)

	// Test that status constants are defined correctly
	g.Expect(status.ConditionDeploymentsAvailable).Should(Equal("DeploymentsAvailable"))
	g.Expect(status.ReadySuffix).Should(Equal("Ready"))
}

func TestManifestInfoEquality(t *testing.T) {
	g := NewWithT(t)

	manifest1 := odhtypes.ManifestInfo{
		Path:       dashboard.TestPath,
		ContextDir: dashboard.ComponentName,
		SourcePath: "/odh",
	}

	manifest2 := odhtypes.ManifestInfo{
		Path:       dashboard.TestPath,
		ContextDir: dashboard.ComponentName,
		SourcePath: "/odh",
	}

	manifest3 := odhtypes.ManifestInfo{
		Path:       "/different/path",
		ContextDir: dashboard.ComponentName,
		SourcePath: "/odh",
	}

	g.Expect(manifest1).Should(Equal(manifest2))
	g.Expect(manifest1).ShouldNot(Equal(manifest3))
}

func TestPlatformMappingConsistency(t *testing.T) {
	g := NewWithT(t)

	// Test that all platform mappings have consistent keys
	platforms := []common.Platform{
		cluster.OpenDataHub,
		cluster.SelfManagedRhoai,
		cluster.ManagedRhoai,
	}

	for _, platform := range platforms {
		g.Expect(dashboard.SectionTitle).Should(HaveKey(platform))
		g.Expect(dashboard.BaseConsoleURL).Should(HaveKey(platform))
		g.Expect(dashboard.OverlaysSourcePaths).Should(HaveKey(platform))
	}
}

func TestImageMappingCompleteness(t *testing.T) {
	g := NewWithT(t)

	// Test that all expected image keys are present
	expectedKeys := []string{
		"odh-dashboard-image",
		"model-registry-ui-image",
	}

	for _, key := range expectedKeys {
		g.Expect(dashboard.ImagesMap).Should(HaveKey(key))
		g.Expect(dashboard.ImagesMap[key]).ShouldNot(BeEmpty())
	}
}

func TestConditionTypesCompleteness(t *testing.T) {
	g := NewWithT(t)

	// Test that all expected condition types are present
	expectedConditions := []string{
		status.ConditionDeploymentsAvailable,
	}

	// Test that the condition type is defined
	g.Expect(testConditionTypes).ShouldNot(BeEmpty())
	for _, condition := range expectedConditions {
		g.Expect(testConditionTypes).Should(Equal(condition))
	}
}

// TestComputeComponentNameEdgeCases tests edge cases with comprehensive table-driven subtests.
func TestComputeComponentNameEdgeCases(t *testing.T) {
	tests := getComponentNameTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runComponentNameTestCase(t, tt)
		})
	}
}

// TestComputeComponentNameConcurrency tests concurrent access to the function.
func TestComputeComponentNameConcurrency(t *testing.T) {
	t.Run("concurrent_access", func(t *testing.T) {
		g := NewWithT(t)

		// Test concurrent access to the function
		const numGoroutines = 10
		const callsPerGoroutine = 100

		results := make(chan string, numGoroutines*callsPerGoroutine)

		// Start multiple goroutines
		for range numGoroutines {
			go func() {
				for range callsPerGoroutine {
					results <- dashboard.ComputeComponentName()
				}
			}()
		}

		// Collect all results
		allResults := make([]string, 0, numGoroutines*callsPerGoroutine)
		for range numGoroutines * callsPerGoroutine {
			allResults = append(allResults, <-results)
		}

		// All results should be identical
		firstResult := allResults[0]
		for _, result := range allResults {
			g.Expect(result).Should(Equal(firstResult))
		}
	})
}

// TestComputeComponentNamePerformance tests performance with many calls.
func TestComputeComponentNamePerformance(t *testing.T) {
	t.Run("performance_benchmark", func(t *testing.T) {
		g := NewWithT(t)

		// Test performance with many calls
		const performanceTestCalls = 1000
		results := make([]string, performanceTestCalls)

		// Measure performance while testing stability
		start := time.Now()
		for i := range performanceTestCalls {
			results[i] = dashboard.ComputeComponentName()
		}
		duration := time.Since(start)

		// Assert all results are identical (stability)
		firstResult := results[0]
		for i := 1; i < len(results); i++ {
			g.Expect(results[i]).Should(Equal(firstResult), "Result %d should equal first result", i)
		}

		// Assert performance is acceptable (1000 calls complete under 1s)
		// Only enforce strict performance requirements on fast runners (not in constrained CI)
		if os.Getenv("CI") == "" || os.Getenv("FAST_CI") == "true" {
			g.Expect(duration).Should(BeNumerically("<", time.Second), "1000 calls should complete under 1 second on fast runners")
		} else {
			// More lenient threshold for constrained CI environments
			g.Expect(duration).Should(BeNumerically("<", 3*time.Second), "1000 calls should complete under 3 seconds on constrained CI")
		}
	})
}

// getComponentNameTestCases returns test cases for component name edge cases.
// These tests focus on robustness and error handling since the cluster configuration
// is initialized once and cached, so environment variable changes don't affect behavior.
func getComponentNameTestCases() []componentNameTestCase {
	// Get the current component name to use as expected value
	currentName := dashboard.ComputeComponentName()

	return []componentNameTestCase{
		{
			name:         "Cached configuration behavior",
			platformType: "TestPlatform",
			ciEnv:        "true",
			expectedName: currentName,
			description:  "Should return cached component name regardless of environment variables",
		},
	}
}

// componentNameTestCase represents a test case for component name testing.
type componentNameTestCase struct {
	name         string
	platformType string
	ciEnv        string
	expectedName string
	description  string
}

// runComponentNameTestCase executes a single component name test case.
func runComponentNameTestCase(t *testing.T, tt componentNameTestCase) {
	t.Helper()
	g := NewWithT(t)

	// Store and restore original environment values
	originalPlatformType := os.Getenv("ODH_PLATFORM_TYPE")
	originalCI := os.Getenv("CI")
	defer restoreComponentNameEnvironment(originalPlatformType, originalCI)

	// Set up test environment
	setupTestEnvironment(tt.platformType, tt.ciEnv)

	// Test that the function doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("computeComponentName panicked with %s: %v", tt.description, r)
		}
	}()

	// Call the function and verify result
	name := dashboard.ComputeComponentName()
	assertComponentName(g, name, tt.expectedName, tt.description)
}

// setupTestEnvironment sets up the test environment variables.
func setupTestEnvironment(platformType, ciEnv string) {
	if platformType != "" {
		os.Setenv("ODH_PLATFORM_TYPE", platformType)
	} else {
		os.Unsetenv("ODH_PLATFORM_TYPE")
	}

	if ciEnv != "" {
		os.Setenv("CI", ciEnv)
	} else {
		os.Unsetenv("CI")
	}
}

// restoreComponentNameEnvironment restores the original environment variables.
func restoreComponentNameEnvironment(originalPlatformType, originalCI string) {
	if originalPlatformType != "" {
		os.Setenv("ODH_PLATFORM_TYPE", originalPlatformType)
	} else {
		os.Unsetenv("ODH_PLATFORM_TYPE")
	}
	if originalCI != "" {
		os.Setenv("CI", originalCI)
	} else {
		os.Unsetenv("CI")
	}
}

// assertComponentName performs assertions on the component name.
func assertComponentName(g *WithT, name, expectedName, description string) {
	// Assert the component name is not empty
	g.Expect(name).ShouldNot(BeEmpty(), "Component name should not be empty for %s", description)

	// Assert the expected component name
	g.Expect(name).Should(Equal(expectedName), "Expected %s but got %s for %s", expectedName, name, description)

	// Verify the name is one of the valid legacy component names
	g.Expect(name).Should(BeElementOf(dashboard.LegacyComponentNameUpstream, dashboard.LegacyComponentNameDownstream),
		"Component name should be one of the valid legacy names for %s", description)
}

// TestComputeComponentNameInitializationPath tests the initialization path behavior.
// These tests verify that environment variables would affect behavior if the cache was cleared.
// Note: These tests document the expected behavior but cannot actually test it due to caching.
func TestComputeComponentNameInitializationPath(t *testing.T) {
	t.Run("environment_variable_effects_documented", func(t *testing.T) {
		g := NewWithT(t)

		// Store original environment
		originalPlatformType := os.Getenv("ODH_PLATFORM_TYPE")
		originalCI := os.Getenv("CI")
		defer restoreComponentNameEnvironment(originalPlatformType, originalCI)

		// Test cases that would affect behavior if cache was cleared
		testCases := getInitializationPathTestCases()

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				setupTestEnvironment(tc.platformType, tc.ciEnv)
				runInitializationPathTest(t, g, tc)
			})
		}
	})
}

// getInitializationPathTestCases returns test cases for initialization path testing.
func getInitializationPathTestCases() []initializationPathTestCase {
	return []initializationPathTestCase{
		{
			name:         "OpenDataHub platform type",
			platformType: "OpenDataHub",
			ciEnv:        "",
			description:  "Should return upstream component name for OpenDataHub platform",
		},
		{
			name:         "SelfManagedRHOAI platform type",
			platformType: "SelfManagedRHOAI",
			ciEnv:        "",
			description:  "Should return downstream component name for SelfManagedRHOAI platform",
		},
		{
			name:         "ManagedRHOAI platform type",
			platformType: "ManagedRHOAI",
			ciEnv:        "",
			description:  "Should return downstream component name for ManagedRHOAI platform",
		},
		{
			name:         "CI environment set",
			platformType: "",
			ciEnv:        "true",
			description:  "Should handle CI environment variable during initialization",
		},
	}
}

// initializationPathTestCase represents a test case for initialization path testing.
type initializationPathTestCase struct {
	name         string
	platformType string
	ciEnv        string
	description  string
}

// runInitializationPathTest executes a single initialization path test case.
func runInitializationPathTest(t *testing.T, g *WithT, tc initializationPathTestCase) {
	t.Helper()

	// Call the function (will use cached config regardless of env vars)
	name := dashboard.ComputeComponentName()

	// Verify it returns a valid component name
	g.Expect(name).ShouldNot(BeEmpty(), "Component name should not be empty for %s", tc.description)
	g.Expect(name).Should(BeElementOf(dashboard.LegacyComponentNameUpstream, dashboard.LegacyComponentNameDownstream),
		"Component name should be valid for %s", tc.description)

	// Note: Due to caching, all calls will return the same result regardless of environment variables
	// This test documents the expected behavior if the cache was cleared
}
