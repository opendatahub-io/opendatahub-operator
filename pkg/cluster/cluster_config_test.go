package cluster_test

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// erroringClient is a wrapper around a client.Client that allows us to inject errors.
type erroringClient struct {
	client.Client
	err error
}

func (c *erroringClient) Get(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
	if key.Name == "cluster-config-v1" {
		return c.err
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func TestIsFipsEnabled(t *testing.T) {
	var genericError = errors.New("generic client error")

	// Define test cases
	testCases := []struct {
		name           string
		configMap      *corev1.ConfigMap
		clientErr      error
		expectedResult bool
		expectedError  error
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
			expectedError:  nil,
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
			expectedError:  nil,
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
			expectedResult: false, // Should return false when fips key is missing
			expectedError:  nil,
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
			expectedResult: true, // Should return true because the string "fips: true" is present
			expectedError:  nil,
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
			expectedResult: false, // Should return false because the string "fips: false" is present
			expectedError:  nil,
		},
		{
			name: "Empty install-config",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-config-v1",
					Namespace: "kube-system",
				},
				Data: map[string]string{
					"install-config": "",
				},
			},
			expectedResult: false,
			expectedError:  nil,
		},
		{
			name:           "ConfigMap not found",
			clientErr:      k8serr.NewNotFound(schema.GroupResource{Group: "", Resource: "configmaps"}, "cluster-config-v1"),
			expectedResult: false,
			expectedError:  k8serr.NewNotFound(schema.GroupResource{Group: "", Resource: "configmaps"}, "cluster-config-v1"), // Expect the same error
		},
		{
			name:           "Other client error",
			clientErr:      genericError,
			expectedResult: false,
			expectedError:  errors.New("generic client error"),
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

				fakeClient = fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
				if tc.clientErr != nil {
					fakeClient = &erroringClient{
						Client: fakeClient,
						err:    tc.clientErr,
					}
				}
			} else {
				fakeClient = fake.NewClientBuilder().Build()
			}

			// Call the function under test
			ctx := t.Context()
			result, err := cluster.IsFipsEnabled(ctx, fakeClient)

			// Check the result
			if result != tc.expectedResult {
				t.Errorf("IsFIPSEnabled() = %v, want %v", result, tc.expectedResult)
			}

			// Check the error.  We need to handle nil vs. non-nil errors carefully.
			if tc.expectedError != nil {
				switch {
				case err == nil:
					t.Errorf("IsFIPSEnabled() error = nil, want %v", tc.expectedError)
				case k8serr.IsNotFound(tc.expectedError):
					if !k8serr.IsNotFound(err) {
						t.Errorf("IsFipsEnabled() error = %v, want NotFound error", err)
					}
				default:
					if err.Error() != tc.expectedError.Error() {
						t.Errorf("IsFIPSEnabled() error = %v, want %v", err, tc.expectedError)
					}
				}
			} else if err != nil {
				t.Errorf("IsFIPSEnabled() error = %v, want nil", err)
			}
		})
	}
}
