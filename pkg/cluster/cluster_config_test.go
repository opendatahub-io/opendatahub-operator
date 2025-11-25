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

func TestGetClusterAuthenticationMode(t *testing.T) {
	// Register the configv1 scheme
	scheme := runtime.NewScheme()
	err := configv1.AddToScheme(scheme)
	if err != nil {
		t.Fatalf("failed to add configv1 scheme: %v", err)
	}

	testCases := []struct {
		name          string
		authObject    *configv1.Authentication
		expectedMode  cluster.AuthenticationMode
		expectedError bool
		errorType     string
	}{
		{
			name: "IntegratedOAuth type",
			authObject: &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster.ClusterAuthenticationObj,
				},
				Spec: configv1.AuthenticationSpec{
					Type: configv1.AuthenticationTypeIntegratedOAuth,
				},
			},
			expectedMode:  cluster.AuthModeIntegratedOAuth,
			expectedError: false,
		},
		{
			name: "Empty type (defaults to IntegratedOAuth)",
			authObject: &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster.ClusterAuthenticationObj,
				},
				Spec: configv1.AuthenticationSpec{
					Type: "",
				},
			},
			expectedMode:  cluster.AuthModeIntegratedOAuth,
			expectedError: false,
		},
		{
			name: "OIDC type",
			authObject: &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster.ClusterAuthenticationObj,
				},
				Spec: configv1.AuthenticationSpec{
					Type: "OIDC",
				},
			},
			expectedMode:  cluster.AuthModeOIDC,
			expectedError: false,
		},
		{
			name: "None type",
			authObject: &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster.ClusterAuthenticationObj,
				},
				Spec: configv1.AuthenticationSpec{
					Type: configv1.AuthenticationTypeNone,
				},
			},
			expectedMode:  cluster.AuthModeNone,
			expectedError: false,
		},
		{
			name: "Custom type (defaults to None)",
			authObject: &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster.ClusterAuthenticationObj,
				},
				Spec: configv1.AuthenticationSpec{
					Type: "CustomAuth",
				},
			},
			expectedMode:  cluster.AuthModeNone,
			expectedError: false,
		},
		{
			name:          "Authentication object not found",
			authObject:    nil,
			expectedMode:  "",
			expectedError: true,
			errorType:     "notfound",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			var fakeClient client.Client
			objs := []runtime.Object{}
			if tc.authObject != nil {
				objs = append(objs, tc.authObject)
			}

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			result, err := cluster.GetClusterAuthenticationMode(ctx, fakeClient)

			if tc.expectedError {
				if err == nil {
					t.Errorf("GetClusterAuthenticationMode() expected error but got nil")
				} else if tc.errorType == "notfound" && !k8serr.IsNotFound(err) {
					t.Errorf("GetClusterAuthenticationMode() expected NotFound error but got %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("GetClusterAuthenticationMode() unexpected error: %v", err)
				}
				if result != tc.expectedMode {
					t.Errorf("GetClusterAuthenticationMode() = %v, want %v", result, tc.expectedMode)
				}
			}
		})
	}
}

func TestIsIntegratedOAuth(t *testing.T) {
	// Register the configv1 scheme
	scheme := runtime.NewScheme()
	err := configv1.AddToScheme(scheme)
	if err != nil {
		t.Fatalf("failed to add configv1 scheme: %v", err)
	}

	testCases := []struct {
		name           string
		authObject     *configv1.Authentication
		expectedResult bool
		expectedError  bool
	}{
		{
			name: "IntegratedOAuth type returns true",
			authObject: &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster.ClusterAuthenticationObj,
				},
				Spec: configv1.AuthenticationSpec{
					Type: configv1.AuthenticationTypeIntegratedOAuth,
				},
			},
			expectedResult: true,
			expectedError:  false,
		},
		{
			name: "Empty type returns true (defaults to IntegratedOAuth)",
			authObject: &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster.ClusterAuthenticationObj,
				},
				Spec: configv1.AuthenticationSpec{
					Type: "",
				},
			},
			expectedResult: true,
			expectedError:  false,
		},
		{
			name: "OIDC type returns false",
			authObject: &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster.ClusterAuthenticationObj,
				},
				Spec: configv1.AuthenticationSpec{
					Type: "OIDC",
				},
			},
			expectedResult: false,
			expectedError:  false,
		},
		{
			name: "None type returns false",
			authObject: &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster.ClusterAuthenticationObj,
				},
				Spec: configv1.AuthenticationSpec{
					Type: configv1.AuthenticationTypeNone,
				},
			},
			expectedResult: false,
			expectedError:  false,
		},
		{
			name: "Custom type returns false",
			authObject: &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{
					Name: cluster.ClusterAuthenticationObj,
				},
				Spec: configv1.AuthenticationSpec{
					Type: "CustomAuth",
				},
			},
			expectedResult: false,
			expectedError:  false,
		},
		{
			name:           "Authentication object not found returns error",
			authObject:     nil,
			expectedResult: false,
			expectedError:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			var fakeClient client.Client
			objs := []runtime.Object{}
			if tc.authObject != nil {
				objs = append(objs, tc.authObject)
			}

			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			result, err := cluster.IsIntegratedOAuth(ctx, fakeClient)

			if tc.expectedError {
				if err == nil {
					t.Errorf("IsIntegratedOAuth() expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("IsIntegratedOAuth() unexpected error: %v", err)
				}
				if result != tc.expectedResult {
					t.Errorf("IsIntegratedOAuth() = %v, want %v", result, tc.expectedResult)
				}
			}
		})
	}
}
