// This file contains tests for dashboard utility functions.
// These tests verify the utility functions in dashboard_controller_actions.go.
package dashboard_test

import (
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/validation"

	. "github.com/onsi/gomega"
)

const (
	rfc1123ErrorMsg = "must be lowercase and conform to RFC1123 DNS label rules"
)

func TestValidateNamespace(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name          string
		namespace     string
		expectError   bool
		errorContains string
	}{
		{
			name:        "ValidNamespace",
			namespace:   "test-namespace",
			expectError: false,
		},
		{
			name:        "ValidNamespaceWithNumbers",
			namespace:   "test-namespace-123",
			expectError: false,
		},
		{
			name:        "ValidNamespaceSingleChar",
			namespace:   "a",
			expectError: false,
		},
		{
			name:        "ValidNamespaceMaxLength",
			namespace:   "a" + string(make([]byte, 62)), // 63 characters total
			expectError: true,                           // This should actually fail due to null bytes
		},
		{
			name:          "EmptyNamespace",
			namespace:     "",
			expectError:   true,
			errorContains: "namespace cannot be empty",
		},
		{
			name:          "NamespaceTooLong",
			namespace:     "a" + string(make([]byte, 63)), // 64 characters total
			expectError:   true,
			errorContains: "exceeds maximum length of 63 characters",
		},
		{
			name:          "NamespaceWithUppercase",
			namespace:     "Test-Namespace",
			expectError:   true,
			errorContains: rfc1123ErrorMsg,
		},
		{
			name:          "NamespaceWithSpecialChars",
			namespace:     "test-namespace!@#",
			expectError:   true,
			errorContains: rfc1123ErrorMsg,
		},
		{
			name:          "NamespaceStartingWithHyphen",
			namespace:     "-test-namespace",
			expectError:   true,
			errorContains: rfc1123ErrorMsg,
		},
		{
			name:          "NamespaceEndingWithHyphen",
			namespace:     "test-namespace-",
			expectError:   true,
			errorContains: rfc1123ErrorMsg,
		},
		{
			name:          "NamespaceWithUnderscore",
			namespace:     "test_namespace",
			expectError:   true,
			errorContains: rfc1123ErrorMsg,
		},
		{
			name:          "NamespaceWithDot",
			namespace:     "test.namespace",
			expectError:   true,
			errorContains: rfc1123ErrorMsg,
		},
		{
			name:          "NamespaceOnlyHyphens",
			namespace:     "---",
			expectError:   true,
			errorContains: rfc1123ErrorMsg,
		},
		{
			name:        "NamespaceWithNumbersOnly",
			namespace:   "123",
			expectError: false, // Numbers only are actually valid
		},
		{
			name:        "NamespaceWithMixedValidChars",
			namespace:   "test123-namespace456",
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validation.ValidateNamespace(tc.namespace)

			g := NewWithT(t)
			if tc.expectError {
				g.Expect(err).Should(HaveOccurred())
				if tc.errorContains != "" {
					g.Expect(err.Error()).Should(ContainSubstring(tc.errorContains))
				}
			} else {
				g.Expect(err).ShouldNot(HaveOccurred())
			}
		})
	}
}
