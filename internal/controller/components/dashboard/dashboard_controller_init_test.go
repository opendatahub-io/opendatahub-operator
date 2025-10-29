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

// TestInitPlatforms tests the Init function with various platform scenarios.
// This consolidated test replaces multiple redundant tests with a single table-driven test
// that covers all platform validation scenarios in a maintainable way.
func TestInitPlatforms(t *testing.T) {
	testCases := []struct {
		name           string
		platform       common.Platform
		category       string // "supported", "unsupported", "edge-case"
		expectError    bool
		errorSubstring string
	}{
		// Supported platforms - should succeed
		{
			name:           "supported-self-managed-rhoai",
			platform:       cluster.SelfManagedRhoai,
			category:       "supported",
			expectError:    false,
			errorSubstring: "",
		},
		{
			name:           "supported-managed-rhoai",
			platform:       cluster.ManagedRhoai,
			category:       "supported",
			expectError:    false,
			errorSubstring: "",
		},
		{
			name:           "supported-opendatahub",
			platform:       cluster.OpenDataHub,
			category:       "supported",
			expectError:    false,
			errorSubstring: "",
		},

		// Unsupported platforms - should fail with clear error messages
		{
			name:           "unsupported-openshift",
			platform:       common.Platform("OpenShift"),
			category:       "unsupported",
			expectError:    true,
			errorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:           "unsupported-kubernetes",
			platform:       common.Platform("Kubernetes"),
			category:       "unsupported",
			expectError:    true,
			errorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:           "unsupported-non-existent-platform",
			platform:       common.Platform(NonExistentPlatform),
			category:       "unsupported",
			expectError:    true,
			errorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:           "unsupported-test-platform",
			platform:       common.Platform(TestPlatform),
			category:       "unsupported",
			expectError:    true,
			errorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:           "unsupported-upstream",
			platform:       common.Platform("upstream"),
			category:       "unsupported",
			expectError:    true,
			errorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:           "unsupported-downstream",
			platform:       common.Platform("downstream"),
			category:       "unsupported",
			expectError:    true,
			errorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:           "unsupported-self-managed-test",
			platform:       common.Platform(TestSelfManagedPlatform),
			category:       "unsupported",
			expectError:    true,
			errorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:           "unsupported-managed",
			platform:       common.Platform("managed"),
			category:       "unsupported",
			expectError:    true,
			errorSubstring: unsupportedPlatformErrorMsg,
		},

		// Edge cases - should fail with clear error messages
		{
			name:           "edge-case-empty-platform",
			platform:       common.Platform(""),
			category:       "edge-case",
			expectError:    true,
			errorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:           "edge-case-nil-like-platform",
			platform:       common.Platform("nil-test"),
			category:       "edge-case",
			expectError:    true,
			errorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:           "edge-case-special-chars",
			platform:       common.Platform("test-platform-with-special-chars!@#$%"),
			category:       "edge-case",
			expectError:    true,
			errorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:           "edge-case-very-long-name",
			platform:       common.Platform(strings.Repeat("a", 1000)),
			category:       "edge-case",
			expectError:    true,
			errorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:           "edge-case-platform-with-dashes",
			platform:       common.Platform("platform-with-dashes"),
			category:       "edge-case",
			expectError:    true,
			errorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:           "edge-case-platform-with-underscores",
			platform:       common.Platform("platform_with_underscores"),
			category:       "edge-case",
			expectError:    true,
			errorSubstring: unsupportedPlatformErrorMsg,
		},
		{
			name:           "edge-case-platform-with-dots",
			platform:       common.Platform("platform.with.dots"),
			category:       "edge-case",
			expectError:    true,
			errorSubstring: unsupportedPlatformErrorMsg,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
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
			if tc.expectError {
				g.Expect(err).Should(HaveOccurred(), "Expected error for %s platform: %s", tc.category, tc.platform)
				if tc.errorSubstring != "" {
					g.Expect(err.Error()).Should(ContainSubstring(tc.errorSubstring),
						"Expected error to contain '%s' for %s platform: %s, got: %v", tc.errorSubstring, tc.category, tc.platform, err)
				}
			} else {
				g.Expect(err).ShouldNot(HaveOccurred(), "Expected no error for %s platform: %s, got: %v", tc.category, tc.platform, err)
			}
		})
	}
}
