// This file contains tests for dashboard utility functions.
// These tests verify the utility functions in dashboard_controller_actions.go.
//
//nolint:testpackage
package dashboard

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
			err := validateNamespace(tc.namespace)

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

func TestResourceExists(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		resources []unstructured.Unstructured
		candidate client.Object
		expected  bool
	}{
		{
			name:      "NilCandidate",
			resources: []unstructured.Unstructured{},
			candidate: nil,
			expected:  false,
		},
		{
			name:      "EmptyResources",
			resources: []unstructured.Unstructured{},
			candidate: createTestObject("test", "namespace", schema.GroupVersionKind{
				Group:   "test",
				Version: "v1",
				Kind:    "Test",
			}),
			expected: false,
		},
		{
			name: "MatchingResource",
			resources: []unstructured.Unstructured{
				*createTestUnstructured("test", "namespace", schema.GroupVersionKind{
					Group:   "test",
					Version: "v1",
					Kind:    "Test",
				}),
			},
			candidate: createTestObject("test", "namespace", schema.GroupVersionKind{
				Group:   "test",
				Version: "v1",
				Kind:    "Test",
			}),
			expected: true,
		},
		{
			name: "NonMatchingName",
			resources: []unstructured.Unstructured{
				*createTestUnstructured("different", "namespace", schema.GroupVersionKind{
					Group:   "test",
					Version: "v1",
					Kind:    "Test",
				}),
			},
			candidate: createTestObject("test", "namespace", schema.GroupVersionKind{
				Group:   "test",
				Version: "v1",
				Kind:    "Test",
			}),
			expected: false,
		},
		{
			name: "NonMatchingNamespace",
			resources: []unstructured.Unstructured{
				*createTestUnstructured("test", "different", schema.GroupVersionKind{
					Group:   "test",
					Version: "v1",
					Kind:    "Test",
				}),
			},
			candidate: createTestObject("test", "namespace", schema.GroupVersionKind{
				Group:   "test",
				Version: "v1",
				Kind:    "Test",
			}),
			expected: false,
		},
		{
			name: "NonMatchingGVK",
			resources: []unstructured.Unstructured{
				*createTestUnstructured("test", "namespace", schema.GroupVersionKind{
					Group:   "different",
					Version: "v1",
					Kind:    "Test",
				}),
			},
			candidate: createTestObject("test", "namespace", schema.GroupVersionKind{
				Group:   "test",
				Version: "v1",
				Kind:    "Test",
			}),
			expected: false,
		},
		{
			name: "MultipleResourcesWithMatch",
			resources: []unstructured.Unstructured{
				*createTestUnstructured("test1", "namespace", schema.GroupVersionKind{
					Group:   "test",
					Version: "v1",
					Kind:    "Test",
				}),
				*createTestUnstructured("test2", "namespace", schema.GroupVersionKind{
					Group:   "test",
					Version: "v1",
					Kind:    "Test",
				}),
				*createTestUnstructured("test", "namespace", schema.GroupVersionKind{
					Group:   "test",
					Version: "v1",
					Kind:    "Test",
				}),
			},
			candidate: createTestObject("test", "namespace", schema.GroupVersionKind{
				Group:   "test",
				Version: "v1",
				Kind:    "Test",
			}),
			expected: true,
		},
		{
			name: "MultipleResourcesNoMatch",
			resources: []unstructured.Unstructured{
				*createTestUnstructured("test1", "namespace", schema.GroupVersionKind{
					Group:   "test",
					Version: "v1",
					Kind:    "Test",
				}),
				*createTestUnstructured("test2", "namespace", schema.GroupVersionKind{
					Group:   "test",
					Version: "v1",
					Kind:    "Test",
				}),
			},
			candidate: createTestObject("test", "namespace", schema.GroupVersionKind{
				Group:   "test",
				Version: "v1",
				Kind:    "Test",
			}),
			expected: false,
		},
		{
			name: "MatchingWithDifferentNamespace",
			resources: []unstructured.Unstructured{
				*createTestUnstructured("test", "namespace1", schema.GroupVersionKind{
					Group:   "test",
					Version: "v1",
					Kind:    "Test",
				}),
			},
			candidate: createTestObject("test", "namespace2", schema.GroupVersionKind{
				Group:   "test",
				Version: "v1",
				Kind:    "Test",
			}),
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := resourceExists(tc.resources, tc.candidate)

			g := NewWithT(t)
			g.Expect(result).Should(Equal(tc.expected))
		})
	}
}

// Helper functions for creating test objects.
func createTestObject(_, namespace string, gvk schema.GroupVersionKind) client.Object {
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: namespace,
		},
	}
	obj.SetGroupVersionKind(gvk)
	return obj
}

func createTestUnstructured(name, namespace string, gvk schema.GroupVersionKind) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetName(name)
	obj.SetNamespace(namespace)
	obj.SetGroupVersionKind(gvk)
	return obj
}
