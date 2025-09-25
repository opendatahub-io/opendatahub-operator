//nolint:testpackage // allow testing unexported internals because these tests exercise package-private reconciliation logic
package dashboard

import (
	"strings"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

func TestNewComponentReconcilerUnit(t *testing.T) {
	t.Parallel()

	t.Run("WithNilManager", func(t *testing.T) {
		t.Parallel()
		testNewComponentReconcilerWithNilManager(t)
	})
	t.Run("ComponentNameComputation", func(t *testing.T) {
		t.Parallel()
		testComponentNameComputation(t)
	})
}

func testNewComponentReconcilerWithNilManager(t *testing.T) {
	t.Helper()
	handler := &ComponentHandler{}
	ctx := t.Context()

	t.Run("ReturnsErrorWithNilManager", func(t *testing.T) {
		t.Helper()
		err := handler.NewComponentReconciler(ctx, nil)
		if err == nil {
			t.Error("Expected function to return error with nil manager, but got nil error")
		}
		if err != nil && !strings.Contains(err.Error(), "could not create the dashboard controller") {
			t.Errorf("Expected error to contain 'could not create the dashboard controller', but got: %v", err)
		}
	})
}

// testComponentNameComputation tests that the component name computation logic works correctly.
func testComponentNameComputation(t *testing.T) {
	t.Helper()

	// Save the current global release state to avoid race conditions with parallel tests
	originalRelease := cluster.GetRelease()
	
	// Set the release info to a known deterministic value for testing
	// Use OpenDataHub as the deterministic test value
	cluster.SetReleaseForTesting(common.Release{Name: cluster.OpenDataHub})
	
	// Restore the original global state in cleanup to avoid affecting parallel tests
	t.Cleanup(func() {
		cluster.SetReleaseForTesting(originalRelease)
	})

	// Test that ComputeComponentName returns a valid component name
	componentName := ComputeComponentName()

	// Verify the component name is not empty
	if componentName == "" {
		t.Error("Expected ComputeComponentName to return non-empty string")
	}

	// Verify the component name is one of the expected values
	validNames := []string{LegacyComponentNameUpstream, LegacyComponentNameDownstream}
	valid := false
	for _, validName := range validNames {
		if componentName == validName {
			valid = true
			break
		}
	}

	if !valid {
		t.Errorf("Expected ComputeComponentName to return one of %v, but got: %s", validNames, componentName)
	}

	// Test that multiple calls return the same result (deterministic)
	componentName2 := ComputeComponentName()
	if componentName != componentName2 {
		t.Error("Expected ComputeComponentName to be deterministic, but got different results")
	}
}

// TestNewComponentReconcilerIntegration tests the NewComponentReconciler with proper error handling.
func TestNewComponentReconcilerIntegration(t *testing.T) {
	t.Parallel()

	t.Run("ErrorHandling", func(t *testing.T) {
		t.Parallel()
		testNewComponentReconcilerErrorHandling(t)
	})
}

func assertNilManagerError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Error("Expected NewComponentReconciler to return error with nil manager")
	}
	if err != nil && !strings.Contains(err.Error(), "could not create the dashboard controller") {
		t.Errorf("Expected error to contain 'could not create the dashboard controller', but got: %v", err)
	}
}

func testNewComponentReconcilerErrorHandling(t *testing.T) {
	t.Helper()

	// Test that the function handles various error conditions gracefully
	handler := &ComponentHandler{}
	ctx := t.Context()

	err := handler.NewComponentReconciler(ctx, nil)
	assertNilManagerError(t, err)

	// Test that the function doesn't panic with nil manager
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NewComponentReconciler panicked with nil manager: %v", r)
		}
	}()

	// This should not panic
	_ = handler.NewComponentReconciler(ctx, nil)
}
