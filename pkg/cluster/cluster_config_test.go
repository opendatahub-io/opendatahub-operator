package cluster

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors" // Import k8serrors
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestIsFIPSEnabled(t *testing.T) {

    var genericError = errors.New("generic client error")

	// Define test cases
	testCases := []struct {
		name            string
		configMap     *corev1.ConfigMap
		clientErr       error
		expectedResult  bool
		expectedError   error
	}{
		{
			name: "FIPS enabled",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-config-v1",
					Namespace: "kube-system",
				},
				Data: map[string]string{
					"install-config": `apiVersion: v1
fips: true`,
				},
			},
			expectedResult: true,
			expectedError:   nil,
		},
		{
			name: "FIPS disabled",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-config-v1",
					Namespace: "kube-system",
				},
				Data: map[string]string{
					"install-config": `apiVersion: v1
fips: false`,
				},
			},
			expectedResult: false,
			expectedError:   nil,
		},
		{
			name: "FIPS key missing",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-config-v1",
					Namespace: "kube-system",
				},
				Data: map[string]string{
					"install-config": `apiVersion: v1
`,
				},
			},
			expectedResult:  false, // Should return false when fips key is missing
			expectedError:   nil,
		},
		{
			name: "Invalid YAML, but fips: true string present",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-config-v1",
					Namespace: "kube-system",
				},
				Data: map[string]string{
					"install-config": `apiVersion: v1
fips: true
invalid: yaml`,
				},
			},
			expectedResult:  true, // Should return true because the string "fips: true" is present
			expectedError:   nil,
		},
		{
			name: "Invalid YAML, but fips: false string present",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-config-v1",
					Namespace: "kube-system",
				},
				Data: map[string]string{
					"install-config": `apiVersion: v1
fips: false
invalid: yaml`,
				},
			},
			expectedResult:  false, // Should return false because the string "fips: false" is present
			expectedError:   nil,
		},
		{
			name: "ConfigMap not found",
            clientErr: k8serrors.NewNotFound(schema.GroupResource{Group: "", Resource: "configmaps"}, "cluster-config-v1"),
			expectedResult:  false,
            expectedError:   k8serrors.NewNotFound(schema.GroupResource{Group: "", Resource: "configmaps"}, "cluster-config-v1"), // Expect the same error
		},
		{
			name: "Other client error",
			clientErr: genericError,
			expectedResult:  false,
			expectedError:   errors.New("generic client error"),
		},
		{
			name: "FIPS enabled with config.yaml",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-config-v1",
					Namespace: "kube-system",
				},
				Data: map[string]string{
					"config.yaml": `fips: true`,
				},
			},
			expectedResult: true,
			expectedError:   nil,
		},
		{
			name: "FIPS disabled with config.yaml",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-config-v1",
					Namespace: "kube-system",
				},
				Data: map[string]string{
					"config.yaml": `fips: false`,
				},
			},
			expectedResult: false,
			expectedError:   nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a fake client
			var fakeClient client.Client
			if tc.configMap != nil || tc.clientErr != nil {
				objs := []runtime.Object{}
				if tc.configMap != nil {
					objs = append(objs, tc.configMap)
				}
				fakeClient = fake.NewClientBuilder().WithRuntimeObjects(objs...).WithReactors("get", "configmaps", func(action client.Action) (bool, runtime.Object, error) {
					if tc.clientErr != nil {
						return true, nil, tc.clientErr
					}
					return false, nil, nil // Fallback to the fake client's default behavior.
				}).Build()
			} else {
				fakeClient = fake.NewClientBuilder().Build()
			}

			// Call the function under test
			ctx := context.Background()
			result, err := IsFIPSEnabled(ctx, fakeClient)

			// Check the result
			if result != tc.expectedResult {
				t.Errorf("isFIPSEnabled() = %v, want %v", result, tc.expectedResult)
			}

			// Check the error.  We need to handle nil vs. non-nil errors carefully.
			if tc.expectedError != nil {
				if err == nil {
					t.Errorf("isFIPSEnabled() error = nil, want %v", tc.expectedError)
				} else if notFoundErr, ok := tc.expectedError.(*k8serrors.StatusError); ok {
					// For Kubernetes errors, use errors.Is
					if !errors.Is(err, notFoundErr) {
						t.Errorf("isFIPSEnabled() error = %v, want %v", err, tc.expectedError)
					}
				} else {
					// For generic errors, compare error strings
					if err.Error() != tc.expectedError.Error() {
						t.Errorf("isFIPSEnabled() error = %v, want %v", err, tc.expectedError)
					}
				}
			} else if err != nil {
				t.Errorf("isFIPSEnabled() error = %v, want nil", err)
			}
		})
	}
}
