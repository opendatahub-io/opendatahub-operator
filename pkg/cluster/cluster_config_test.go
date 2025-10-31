package cluster_test

import (
	"context"
	"errors"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
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
	t.Parallel()
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
			t.Parallel()
			g := NewWithT(t)

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
			ctx := context.Background()
			result, err := cluster.IsFipsEnabled(ctx, fakeClient)

			// Check the result
			g.Expect(result).To(Equal(tc.expectedResult))

			// Check the error
			if tc.expectedError != nil {
				g.Expect(err).To(HaveOccurred())
				if k8serr.IsNotFound(tc.expectedError) {
					g.Expect(k8serr.IsNotFound(err)).To(BeTrue())
				} else {
					g.Expect(err.Error()).To(Equal(tc.expectedError.Error()))
				}
			} else {
				g.Expect(err).ShouldNot(HaveOccurred())
			}
		})
	}
}

// TestIsIntegratedOAuth validates the OpenShift authentication method detection logic.
// This function determines whether the operator should create default admin groups
// based on the cluster's authentication configuration. The test ensures:
//
// 1. IntegratedOAuth is correctly identified as integrated (should create groups)
// 2. Empty auth type is correctly identified as integrated (should create groups)
// 3. Custom auth types are correctly identified as non-integrated (should not create groups)
// 4. Missing authentication objects are handled with proper errors
//
// Authentication Types and Behavior:
// - IntegratedOAuth (default): Create default admin groups
// - "" (empty, default): Create default admin groups
// - "None": Do not create default admin groups
// - "OIDC": Do not create default admin groups
// - Custom types: Do not create default admin groups
// - Missing object: Return error (cluster configuration issue)
//
// This is critical for security because it determines whether the operator will
// automatically create admin groups that could grant elevated access.
func TestIsIntegratedOAuth(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		authObject     *configv1.Authentication
		expectError    bool
		expectedResult bool
		description    string
	}{
		{
			name: "should return true for IntegratedOAuth",
			authObject: &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster.ClusterAuthenticationObj,
				},
				Spec: configv1.AuthenticationSpec{
					Type: configv1.AuthenticationTypeIntegratedOAuth,
				},
			},
			expectError:    false,
			expectedResult: true,
			description:    "IntegratedOAuth should be considered integrated auth method",
		},
		{
			name: "should return true for empty type",
			authObject: &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster.ClusterAuthenticationObj,
				},
				Spec: configv1.AuthenticationSpec{
					Type: "",
				},
			},
			expectError:    false,
			expectedResult: true,
			description:    "Empty type should be considered integrated auth method (default)",
		},
		{
			name: "should return false for OIDC",
			authObject: &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster.ClusterAuthenticationObj,
				},
				Spec: configv1.AuthenticationSpec{
					Type: "OIDC",
				},
			},
			expectError:    false,
			expectedResult: false,
			description:    "OIDC should not be considered integrated auth method",
		},
		{
			name: "should return false for None",
			authObject: &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster.ClusterAuthenticationObj,
				},
				Spec: configv1.AuthenticationSpec{
					Type: configv1.AuthenticationTypeNone,
				},
			},
			expectError:    false,
			expectedResult: false,
			description:    "None should not be considered integrated auth method",
		},
		{
			name: "should return false for other auth types",
			authObject: &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster.ClusterAuthenticationObj,
				},
				Spec: configv1.AuthenticationSpec{
					Type: "CustomAuth",
				},
			},
			expectError:    false,
			expectedResult: false,
			description:    "Other auth types should not be considered integrated auth method",
		},
		{
			name:           "should handle missing authentication object",
			authObject:     nil,
			expectError:    true,
			expectedResult: false,
			description:    "Should return error when authentication object doesn't exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			var fakeClient client.Client

			// Build scheme with OpenShift API types
			scheme := runtime.NewScheme()
			err := configv1.AddToScheme(scheme)
			g.Expect(err).ShouldNot(HaveOccurred())

			if tt.authObject != nil {
				fakeClient = fake.NewClientBuilder().
					WithScheme(scheme).
					WithRuntimeObjects(tt.authObject).
					Build()
			} else {
				fakeClient = fake.NewClientBuilder().
					WithScheme(scheme).
					Build()
			}

			result, err := cluster.IsIntegratedOAuth(ctx, fakeClient)

			if tt.expectError {
				g.Expect(err).Should(HaveOccurred(), tt.description)
			} else {
				g.Expect(err).ShouldNot(HaveOccurred(), tt.description)
				g.Expect(result).Should(Equal(tt.expectedResult), tt.description)
			}
		})
	}
}
