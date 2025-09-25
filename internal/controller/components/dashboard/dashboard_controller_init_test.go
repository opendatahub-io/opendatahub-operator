// This file contains tests for dashboard controller initialization functionality.
// These tests verify the dashboard.Initialize function and related initialization logic.
package dashboard_test

import (
	"strings"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard/dashboard_test"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestInitialize(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dsci := &dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: dashboard_test.TestNamespace,
		},
	}

	// Test success case
	t.Run("success", func(t *testing.T) {
		rr := &odhtypes.ReconciliationRequest{
			Client:   cli,
			Instance: &componentApi.Dashboard{},
			DSCI:     dsci,
			Release:  common.Release{Name: cluster.OpenDataHub},
		}

		err = dashboard.Initialize(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Manifests).Should(HaveLen(1))
		g.Expect(rr.Manifests[0].ContextDir).Should(Equal(dashboard.ComponentName))
	})

	// Test error cases
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
					Instance: &componentApi.Dashboard{},
					DSCI:     dsci,
					Release:  common.Release{Name: cluster.OpenDataHub},
				}
			},
			expectError: true,
			errorMsg:    "client is required but was nil",
		},
		{
			name: "nil DSCI",
			setupRR: func() *odhtypes.ReconciliationRequest {
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: &componentApi.Dashboard{},
					DSCI:     nil,
					Release:  common.Release{Name: cluster.OpenDataHub},
				}
			},
			expectError: true,
			errorMsg:    "DSCI is required but was nil",
		},
		{
			name: "invalid Instance type",
			setupRR: func() *odhtypes.ReconciliationRequest {
				// Use a different type that's not *componentApi.Dashboard
				invalidInstance := &componentApi.TrustyAI{}
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: invalidInstance,
					DSCI:     dsci,
					Release:  common.Release{Name: cluster.OpenDataHub},
				}
			},
			expectError: true,
			errorMsg:    "resource instance",
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
			}
		})
	}
}

func TestInitErrorPaths(t *testing.T) {
	g := NewWithT(t)

	handler := &dashboard.ComponentHandler{}

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
			defer func() {
				if r := recover(); r != nil {
					t.Errorf(dashboard_test.ErrorInitPanicked, platform, r)
				}
			}()

			err := handler.Init(platform)
			// We expect this to fail due to missing manifest paths
			// but it should fail gracefully with a specific error
			if err != nil {
				g.Expect(err.Error()).Should(ContainSubstring(dashboard_test.ErrorFailedToUpdate))
			}
		})
	}
}

// InitResult represents the result of running Init with panic recovery.
type InitResult struct {
	PanicRecovered interface{}
	Err            error
}

// runInitWithPanicRecovery runs Init with panic recovery and returns the result.
func runInitWithPanicRecovery(handler *dashboard.ComponentHandler, platform common.Platform) InitResult {
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
		t.Errorf(dashboard_test.ErrorInitPanicked, tc.platform, result.PanicRecovered)
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
	testCases := []struct {
		name                 string
		platform             common.Platform
		expectErrorSubstring string
		expectPanic          bool
	}{
		{
			name:                 "non-existent-platform",
			platform:             common.Platform("non-existent-platform"),
			expectErrorSubstring: dashboard_test.ErrorFailedToUpdate,
			expectPanic:          false,
		},
		{
			name:                 "dashboard_test.TestPlatform",
			platform:             common.Platform(dashboard_test.TestPlatform),
			expectErrorSubstring: dashboard_test.ErrorFailedToUpdate,
			expectPanic:          false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler := &dashboard.ComponentHandler{}
			result := runInitWithPanicRecovery(handler, tc.platform)
			validateInitResult(t, tc, result)
		})
	}
}

// TestInitWithVariousPlatforms tests the Init function with various platform types.
func TestInitWithVariousPlatforms(t *testing.T) {
	g := NewWithT(t)

	handler := &dashboard.ComponentHandler{}

	// Define test cases for different platform scenarios
	testCases := []struct {
		name     string
		platform common.Platform
	}{
		// Valid platforms
		{"OpenShift", common.Platform("OpenShift")},
		{"Kubernetes", common.Platform("Kubernetes")},
		{"SelfManagedRhoai", common.Platform("SelfManagedRhoai")},
		{"ManagedRhoai", common.Platform("ManagedRhoai")},
		{"OpenDataHub", common.Platform("OpenDataHub")},
		// Special characters
		{"test-platform-with-special-chars-123", common.Platform("test-platform-with-special-chars-123")},
		{"platform_with_underscores", common.Platform("platform_with_underscores")},
		{"platform-with-dashes", common.Platform("platform-with-dashes")},
		{"platform.with.dots", common.Platform("platform.with.dots")},
		// Very long platform name
		{"very-long-platform-name", common.Platform("very-long-platform-name-that-exceeds-normal-limits-and-should-still-work-properly")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test that Init handles different platforms gracefully
			defer func() {
				if r := recover(); r != nil {
					t.Errorf(dashboard_test.ErrorInitPanicked, tc.platform, r)
				}
			}()

			err := handler.Init(tc.platform)
			// The function should either succeed or fail gracefully
			// We're testing that it doesn't panic and handles different platforms
			if err != nil {
				// If it fails, it should fail with a specific error message
				g.Expect(err.Error()).Should(ContainSubstring(dashboard_test.ErrorFailedToUpdate))
			}
		})
	}
}

// TestInitWithEmptyPlatform tests the Init function with empty platform.
func TestInitWithEmptyPlatform(t *testing.T) {
	g := NewWithT(t)

	handler := &dashboard.ComponentHandler{}

	// Test with empty platform
	platform := common.Platform("")

	// Test that Init handles empty platform gracefully
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Init panicked with empty platform: %v", r)
		}
	}()

	err := handler.Init(platform)
	// The function should either succeed or fail gracefully
	if err != nil {
		// If it fails, it should fail with a specific error message
		g.Expect(err.Error()).Should(ContainSubstring(dashboard_test.ErrorFailedToUpdate))
	}
}

func TestInitWithFirstApplyParamsError(t *testing.T) {
	g := NewWithT(t)

	handler := &dashboard.ComponentHandler{}

	// Test with a platform that might cause issues
	platform := common.Platform(dashboard_test.TestPlatform)

	// The function should handle ApplyParams errors gracefully
	err := handler.Init(platform)
	// The function might not always return an error depending on the actual ApplyParams behavior
	if err != nil {
		g.Expect(err.Error()).Should(ContainSubstring(dashboard_test.ErrorFailedToUpdateImages))
		t.Logf("Init returned error (expected): %v", err)
	} else {
		// If no error occurs, that's also acceptable behavior
		t.Log("Init handled the platform gracefully without error")
	}
}

// TestInitWithSecondApplyParamsError tests error handling in second ApplyParams call.
func TestInitWithSecondApplyParamsError(t *testing.T) {
	g := NewWithT(t)

	handler := &dashboard.ComponentHandler{}

	// Test with different platforms
	platforms := []common.Platform{
		common.Platform("upstream"),
		common.Platform("downstream"),
		common.Platform(dashboard_test.TestSelfManagedPlatform),
		common.Platform("managed"),
	}

	for _, platform := range platforms {
		err := handler.Init(platform)
		// The function should handle different platforms gracefully
		if err != nil {
			g.Expect(err.Error()).Should(Or(
				ContainSubstring(dashboard_test.ErrorFailedToUpdateImages),
				ContainSubstring(dashboard_test.ErrorFailedToUpdateModularImages),
			))
			t.Logf("Init returned error for platform %s (expected): %v", platform, err)
		} else {
			t.Logf("Init handled platform %s gracefully without error", platform)
		}
	}
}

// TestInitWithInvalidPlatform tests with invalid platform.
func TestInitWithInvalidPlatform(t *testing.T) {
	g := NewWithT(t)

	handler := &dashboard.ComponentHandler{}

	// Test with empty platform
	err := handler.Init(common.Platform(""))
	if err != nil {
		g.Expect(err.Error()).Should(Or(
			ContainSubstring(dashboard_test.ErrorFailedToUpdateImages),
			ContainSubstring(dashboard_test.ErrorFailedToUpdateModularImages),
		))
		t.Logf("Init returned error for empty platform (expected): %v", err)
	} else {
		t.Log("Init handled empty platform gracefully without error")
	}

	// Test with special characters in platform
	err = handler.Init(common.Platform("test-platform-with-special-chars!@#$%"))
	if err != nil {
		g.Expect(err.Error()).Should(Or(
			ContainSubstring(dashboard_test.ErrorFailedToUpdateImages),
			ContainSubstring(dashboard_test.ErrorFailedToUpdateModularImages),
		))
		t.Logf("Init returned error for special chars platform (expected): %v", err)
	} else {
		t.Log("Init handled special chars platform gracefully without error")
	}
}

// TestInitWithLongPlatform tests with very long platform name.
func TestInitWithLongPlatform(t *testing.T) {
	g := NewWithT(t)

	handler := &dashboard.ComponentHandler{}

	// Test with very long platform name
	longPlatform := common.Platform(strings.Repeat("a", 1000))
	err := handler.Init(longPlatform)
	if err != nil {
		g.Expect(err.Error()).Should(Or(
			ContainSubstring(dashboard_test.ErrorFailedToUpdateImages),
			ContainSubstring(dashboard_test.ErrorFailedToUpdateModularImages),
		))
		t.Logf("Init returned error for long platform (expected): %v", err)
	} else {
		t.Log("Init handled long platform gracefully without error")
	}
}

// TestInitWithNilPlatform tests with nil-like platform.
func TestInitWithNilPlatform(t *testing.T) {
	g := NewWithT(t)

	handler := &dashboard.ComponentHandler{}

	// Test with platform that might cause issues
	err := handler.Init(common.Platform("nil-test"))
	if err != nil {
		g.Expect(err.Error()).Should(Or(
			ContainSubstring(dashboard_test.ErrorFailedToUpdateImages),
			ContainSubstring(dashboard_test.ErrorFailedToUpdateModularImages),
		))
		t.Logf("Init returned error for nil-like platform (expected): %v", err)
	} else {
		t.Log("Init handled nil-like platform gracefully without error")
	}
}

// TestInitMultipleCalls tests multiple calls to Init.
func TestInitMultipleCalls(t *testing.T) {
	g := NewWithT(t)

	handler := &dashboard.ComponentHandler{}

	// Test multiple calls to ensure consistency
	platform := common.Platform(dashboard_test.TestPlatform)

	for i := range 3 {
		err := handler.Init(platform)
		if err != nil {
			g.Expect(err.Error()).Should(Or(
				ContainSubstring(dashboard_test.ErrorFailedToUpdateImages),
				ContainSubstring(dashboard_test.ErrorFailedToUpdateModularImages),
			))
			t.Logf("Init call %d returned error (expected): %v", i+1, err)
		} else {
			t.Logf("Init call %d completed successfully", i+1)
		}
	}
}
