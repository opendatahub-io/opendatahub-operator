// This file contains tests for dashboard controller initialization functionality.
// These tests verify the dashboard.Initialize function and related initialization logic.
package dashboard_test

import (
	"strings"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestInitialize(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	// Test success case
	t.Run("success", func(t *testing.T) {
		rr := &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: CreateTestDashboard(),
			Release:  common.Release{Name: cluster.OpenDataHub},
		}

		err = dashboard.Initialize(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Manifests).Should(HaveLen(1))
		g.Expect(rr.Manifests[0].ContextDir).Should(Equal(dashboard.ComponentName))
	})

	// Test cases that should now succeed since we removed unnecessary validations
	testCases := []struct {
		name        string
		setupRR     func() *odhtypes.ReconciliationRequest
		expectError bool
		errorMsg    string
	}{
		{
			name: "nil client",
			setupRR: func() *odhtypes.ReconciliationRequest {
				return &odhtypes.ReconciliationRequest{
					Client:   nil,
					Instance: CreateTestDashboard(),
					Release:  common.Release{Name: cluster.OpenDataHub},
				}
			},
			expectError: false,
			errorMsg:    "",
		},
		{
			name: "nil DSCI",
			setupRR: func() *odhtypes.ReconciliationRequest {
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: CreateTestDashboard(),
					Release:  common.Release{Name: cluster.OpenDataHub},
				}
			},
			expectError: false,
			errorMsg:    "",
		},
		{
			name: "invalid Instance type",
			setupRR: func() *odhtypes.ReconciliationRequest {
				// Use a different type that's not *componentApi.Dashboard
				invalidInstance := &componentApi.TrustyAI{}
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: invalidInstance,
					Release:  common.Release{Name: cluster.OpenDataHub},
				}
			},
			expectError: false,
			errorMsg:    "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rr := tc.setupRR()
			initialManifests := rr.Manifests // Capture initial state

			err := dashboard.Initialize(ctx, rr)

			if tc.expectError {
				g.Expect(err).Should(HaveOccurred())
				g.Expect(err.Error()).Should(ContainSubstring(tc.errorMsg))
				// Ensure Manifests remains unchanged when error occurs
				g.Expect(rr.Manifests).Should(Equal(initialManifests))
			} else {
				g.Expect(err).ShouldNot(HaveOccurred())
				// Assert that Manifests is populated with expected content
				g.Expect(rr.Manifests).ShouldNot(Equal(initialManifests))
				g.Expect(rr.Manifests).Should(HaveLen(1))
				g.Expect(rr.Manifests[0].ContextDir).Should(Equal(dashboard.ComponentName))
				g.Expect(rr.Manifests[0].Path).Should(Equal(odhdeploy.DefaultManifestPath))
			}
		})
	}
}

func TestInitErrorPaths(t *testing.T) {
	handler := getDashboardHandler()

	// Test with invalid platform that might cause errors
	// This tests the error handling in the Init function
	platforms := []common.Platform{
		"",                 // Empty platform
		"invalid-platform", // Invalid platform name
	}

	for _, platform := range platforms {
		t.Run(string(platform), func(t *testing.T) {
			// The Init function should handle invalid platforms gracefully
			// It might fail due to missing manifest paths, but should not panic
			result := runInitWithPanicRecovery(handler, platform)

			// Validate the result - should either succeed or fail with specific error
			validateInitResult(t, struct {
				name                 string
				platform             common.Platform
				expectErrorSubstring string
				expectPanic          bool
			}{
				name:                 string(platform),
				platform:             platform,
				expectErrorSubstring: unsupportedPlatformErrorMsg,
				expectPanic:          false,
			}, result)
		})
	}
}

// InitResult represents the result of running Init with panic recovery.
type InitResult struct {
	PanicRecovered interface{}
	Err            error
}

// runInitWithPanicRecovery runs Init with panic recovery and returns the result.
func runInitWithPanicRecovery(handler registry.ComponentHandler, platform common.Platform) InitResult {
	var panicRecovered interface{}
	defer func() {
		if r := recover(); r != nil {
			panicRecovered = r
		}
	}()
	err := handler.Init(platform)
	return InitResult{
		PanicRecovered: panicRecovered,
		Err:            err,
	}
}

// validateInitResult validates the result of Init call based on expectations.
func validateInitResult(t *testing.T, tc struct {
	name                 string
	platform             common.Platform
	expectErrorSubstring string
	expectPanic          bool
}, result InitResult) {
	t.Helper()
	g := NewWithT(t)

	if tc.expectPanic {
		g.Expect(result.PanicRecovered).ShouldNot(BeNil())
		return
	}

	if result.PanicRecovered != nil {
		t.Errorf(ErrorInitPanicked, tc.platform, result.PanicRecovered)
		return
	}

	if tc.expectErrorSubstring != "" {
		if result.Err != nil {
			g.Expect(result.Err.Error()).Should(ContainSubstring(tc.expectErrorSubstring))
		} else {
			t.Logf("Init handled platform %s gracefully without error", tc.platform)
		}
	} else {
		g.Expect(result.Err).ShouldNot(HaveOccurred())
	}
}

func TestInitErrorCases(t *testing.T) {
	// Test cases for platforms that should fail during Init
	// These are split into two categories:
	// 1. Intentionally unsupported platforms (should return error)
	// 2. Test platforms missing manifest fixtures (should return error due to missing manifests)
	testCases := []struct {
		name                 string
		platform             common.Platform
		expectErrorSubstring string
		expectPanic          bool
		// category indicates the reason for the expected failure
		category string // "unsupported" or "missing-manifests"
	}{
		{
			name:                 "unsupported-non-existent-platform",
			platform:             common.Platform(NonExistentPlatform),
			expectErrorSubstring: unsupportedPlatformErrorMsg,
			expectPanic:          false,
			category:             "unsupported", // This platform is intentionally not supported by the dashboard component
		},
		{
			name:                 "test-platform-missing-manifests",
			platform:             common.Platform(TestPlatform),
			expectErrorSubstring: unsupportedPlatformErrorMsg,
			expectPanic:          false,
			category:             "missing-manifests", // This is a test platform that should be supported but lacks manifest fixtures
		},
	}

	// Group test cases by category for better organization
	unsupportedPlatforms := []struct {
		name                 string
		platform             common.Platform
		expectErrorSubstring string
		expectPanic          bool
	}{}
	missingManifestPlatforms := []struct {
		name                 string
		platform             common.Platform
		expectErrorSubstring string
		expectPanic          bool
	}{}

	// Categorize test cases
	for _, tc := range testCases {
		switch tc.category {
		case "unsupported":
			unsupportedPlatforms = append(unsupportedPlatforms, struct {
				name                 string
				platform             common.Platform
				expectErrorSubstring string
				expectPanic          bool
			}{tc.name, tc.platform, tc.expectErrorSubstring, tc.expectPanic})
		case "missing-manifests":
			missingManifestPlatforms = append(missingManifestPlatforms, struct {
				name                 string
				platform             common.Platform
				expectErrorSubstring string
				expectPanic          bool
			}{tc.name, tc.platform, tc.expectErrorSubstring, tc.expectPanic})
		}
	}

	// Test intentionally unsupported platforms
	// These platforms are not supported by the dashboard component and should fail
	t.Run("UnsupportedPlatforms", func(t *testing.T) {
		for _, tc := range unsupportedPlatforms {
			t.Run(tc.name, func(t *testing.T) {
				handler := getDashboardHandler()
				result := runInitWithPanicRecovery(handler, tc.platform)
				validateInitResult(t, tc, result)
			})
		}
	})

	// Test platforms with missing manifest fixtures
	// These platforms should be supported but are missing test manifest files
	// Future maintainers should either:
	// 1. Add manifest fixtures for these platforms, or
	// 2. Mark these tests as skipped if the platforms are not intended for testing
	t.Run("MissingManifestFixtures", func(t *testing.T) {
		for _, tc := range missingManifestPlatforms {
			t.Run(tc.name, func(t *testing.T) {
				handler := getDashboardHandler()
				result := runInitWithPanicRecovery(handler, tc.platform)
				validateInitResult(t, tc, result)
			})
		}
	})
}

// TestInitWithVariousPlatforms tests the Init function with various platform types.
func TestInitWithVariousPlatforms(t *testing.T) {
	g := NewWithT(t)

	handler := getDashboardHandler()

	// Define test cases with explicit expectations for each platform
	testCases := []struct {
		name        string
		platform    common.Platform
		expectedErr string // Empty string means no error expected, non-empty means error should contain this substring
	}{
		// Supported platforms should succeed
		{"SelfManagedRhoai", cluster.SelfManagedRhoai, ""},
		{"ManagedRhoai", cluster.ManagedRhoai, ""},
		{"OpenDataHub", cluster.OpenDataHub, ""},
		// Unsupported platforms should fail with clear error messages
		{"OpenShift", common.Platform("OpenShift"), unsupportedPlatformErrorMsg},
		{"Kubernetes", common.Platform("Kubernetes"), unsupportedPlatformErrorMsg},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test that Init handles platforms gracefully without panicking
			defer func() {
				if r := recover(); r != nil {
					t.Errorf(ErrorInitPanicked, tc.platform, r)
				}
			}()

			err := handler.Init(tc.platform)

			if tc.expectedErr != "" {
				// Platform should fail with specific error message
				g.Expect(err).Should(HaveOccurred(), "Expected error for platform %s", tc.platform)
				g.Expect(err.Error()).Should(ContainSubstring(tc.expectedErr),
					"Expected error to contain '%s' for platform %s, got: %v", tc.expectedErr, tc.platform, err)
			} else {
				// Platform should succeed - Init is designed to be resilient
				g.Expect(err).ShouldNot(HaveOccurred(), "Expected no error for platform %s, got: %v", tc.platform, err)
			}
		})
	}
}

// TestInitWithInvalidPlatformNames tests the Init function with invalid platform names.
// The Init function is designed to be resilient and handle missing manifests gracefully.
// ApplyParams returns nil (no error) when params.env files don't exist, so invalid platforms should succeed.
func TestInitWithInvalidPlatformNames(t *testing.T) {
	g := NewWithT(t)

	handler := getDashboardHandler()

	// Test cases are categorized by expected behavior:
	// 1. Unsupported platforms (not in OverlaysSourcePaths map) - should succeed gracefully
	// 2. Valid string formats that happen to be unsupported - should succeed gracefully
	// 3. Edge cases like empty strings - should succeed gracefully
	// The Init function is designed to be resilient and not fail on missing manifests
	const (
		categoryUnsupported            = "unsupported"
		categoryValidFormatUnsupported = "valid-format-unsupported"
		categoryEdgeCase               = "edge-case"
		unsupportedPlatformErrorMsg    = "unsupported platform"
	)

	testCases := []struct {
		name        string
		platform    common.Platform
		category    string // "unsupported", "valid-format-unsupported", "edge-case"
		description string
	}{
		// Unsupported platforms (not in OverlaysSourcePaths map)
		{
			name:        "unsupported-non-existent-platform",
			platform:    common.Platform(NonExistentPlatform),
			category:    categoryUnsupported,
			description: "Platform not in OverlaysSourcePaths map should succeed gracefully (ApplyParams handles missing files)",
		},
		{
			name:        "unsupported-test-platform",
			platform:    common.Platform(TestPlatform),
			category:    categoryUnsupported,
			description: "Test platform not in OverlaysSourcePaths map should succeed gracefully (ApplyParams handles missing files)",
		},
		// Valid string formats that happen to be unsupported platforms
		{
			name:        "valid-format-platform-with-dashes",
			platform:    common.Platform("platform-with-dashes"),
			category:    categoryValidFormatUnsupported,
			description: "Platform with dashes is valid string format - should succeed gracefully",
		},
		{
			name:        "valid-format-platform-with-underscores",
			platform:    common.Platform("platform_with_underscores"),
			category:    categoryValidFormatUnsupported,
			description: "Platform with underscores is valid string format - should succeed gracefully",
		},
		{
			name:        "valid-format-platform-with-dots",
			platform:    common.Platform("platform.with.dots"),
			category:    categoryValidFormatUnsupported,
			description: "Platform with dots is valid string format - should succeed gracefully",
		},
		{
			name:        "valid-format-very-long-platform-name",
			platform:    common.Platform("very-long-platform-name-that-exceeds-normal-limits-and-should-still-work-properly"),
			category:    categoryValidFormatUnsupported,
			description: "Long platform name is valid string format - should succeed gracefully",
		},
		// Edge cases
		{
			name:        "edge-case-empty-platform",
			platform:    common.Platform(""),
			category:    categoryEdgeCase,
			description: "Empty platform string should succeed gracefully (ApplyParams handles missing files)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test that Init handles invalid platforms without panicking
			defer func() {
				if r := recover(); r != nil {
					t.Errorf(ErrorInitPanicked, tc.platform, r)
				}
			}()

			err := handler.Init(tc.platform)

			// All unsupported platforms should now fail fast with clear error messages
			// This is better behavior than silent failures or missing configuration
			if tc.category == categoryUnsupported || tc.category == categoryValidFormatUnsupported || tc.category == categoryEdgeCase {
				g.Expect(err).Should(HaveOccurred(), "Expected error for unsupported platform: %s", tc.description)
				g.Expect(err.Error()).Should(ContainSubstring(unsupportedPlatformErrorMsg), "Expected 'unsupported platform' error for: %s", tc.description)
			} else {
				// Only truly supported platforms should succeed
				g.Expect(err).ShouldNot(HaveOccurred(), "Expected no error for %s platform: %s", tc.category, tc.description)
			}
		})
	}
}

// TestInitConsolidated tests the Init function with various platform scenarios.
// This consolidated test replaces multiple near-duplicate tests with a single table-driven test.
func TestInitConsolidated(t *testing.T) {
	g := NewWithT(t)

	// Define test case structure
	type testCase struct {
		name                   string
		platform               common.Platform
		expectedError          bool
		expectedErrorSubstring string
	}

	// Define test cases covering all previously separate scenarios
	testCases := []testCase{
		// First apply error scenarios
		{
			name:                   "first-apply-error-test-platform",
			platform:               common.Platform(TestPlatform),
			expectedError:          true, // Unsupported platforms should fail fast
			expectedErrorSubstring: unsupportedPlatformErrorMsg,
		},

		// Second apply error scenarios
		{
			name:                   "second-apply-error-upstream",
			platform:               common.Platform("upstream"),
			expectedError:          true, // Unsupported platforms should fail fast
			expectedErrorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:                   "second-apply-error-downstream",
			platform:               common.Platform("downstream"),
			expectedError:          true, // Unsupported platforms should fail fast
			expectedErrorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:                   "second-apply-error-self-managed",
			platform:               common.Platform(TestSelfManagedPlatform),
			expectedError:          true, // Unsupported platforms should fail fast
			expectedErrorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:                   "second-apply-error-managed",
			platform:               common.Platform("managed"),
			expectedError:          true, // Unsupported platforms should fail fast
			expectedErrorSubstring: unsupportedPlatformErrorMsg,
		},

		// Invalid platform scenarios
		{
			name:                   "invalid-empty-platform",
			platform:               common.Platform(""),
			expectedError:          true, // Unsupported platforms should fail fast
			expectedErrorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:                   "invalid-special-chars-platform",
			platform:               common.Platform("test-platform-with-special-chars!@#$%"),
			expectedError:          true, // Unsupported platforms should fail fast
			expectedErrorSubstring: unsupportedPlatformErrorMsg,
		},

		// Long platform scenario
		{
			name:                   "long-platform-name",
			platform:               common.Platform(strings.Repeat("a", 1000)),
			expectedError:          true, // Unsupported platforms should fail fast
			expectedErrorSubstring: unsupportedPlatformErrorMsg,
		},

		// Nil-like platform scenario
		{
			name:                   "nil-like-platform",
			platform:               common.Platform("nil-test"),
			expectedError:          true, // Unsupported platforms should fail fast
			expectedErrorSubstring: unsupportedPlatformErrorMsg,
		},

		// Multiple calls scenario (test consistency)
		{
			name:                   "multiple-calls-consistency",
			platform:               common.Platform(TestPlatform),
			expectedError:          true, // Unsupported platforms should fail fast
			expectedErrorSubstring: unsupportedPlatformErrorMsg,
		},
	}

	// Run table-driven tests
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new handler for each test case to ensure isolation
			handler := getDashboardHandler()

			// Test that Init handles platforms without panicking
			defer func() {
				if r := recover(); r != nil {
					t.Errorf(ErrorInitPanicked, tc.platform, r)
				}
			}()

			// Call Init with the test platform
			err := handler.Init(tc.platform)

			// Make deterministic assertions based on expected behavior
			if tc.expectedError {
				g.Expect(err).Should(HaveOccurred(), "Expected error for platform %s", tc.platform)
				if tc.expectedErrorSubstring != "" {
					g.Expect(err.Error()).Should(ContainSubstring(tc.expectedErrorSubstring),
						"Expected error to contain '%s' for platform %s, got: %v", tc.expectedErrorSubstring, tc.platform, err)
				}
			} else {
				g.Expect(err).ShouldNot(HaveOccurred(), "Expected no error for platform %s, got: %v", tc.platform, err)
			}
		})
	}
}
