package cluster

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

func TestGetApplicationNamespace(t *testing.T) {
	testCases := []struct {
		name              string
		platform          common.Platform
		appNamespace      string
		expectedNamespace string
	}{
		{
			name:              "Returns user defined namespace for OpenDataHub",
			platform:          OpenDataHub,
			appNamespace:      "custom-odh-ns",
			expectedNamespace: "custom-odh-ns",
		},
		{
			name:              "Returns user defined namespace for SelfManagedRhoai",
			platform:          SelfManagedRhoai,
			appNamespace:      "custom-rhoai-ns",
			expectedNamespace: "custom-rhoai-ns",
		},
		{
			name:              "Fallback to default for OpenDataHub",
			platform:          OpenDataHub,
			appNamespace:      "",
			expectedNamespace: "opendatahub",
		},
		{
			name:              "Fallback to default for SelfManagedRhoai",
			platform:          SelfManagedRhoai,
			appNamespace:      "",
			expectedNamespace: "redhat-ods-applications",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Directly set the internal clusterConfig
			clusterConfig.Release.Name = tc.platform
			clusterConfig.ApplicationNamespace = tc.appNamespace
			defer func() {
				// Reset after test
				clusterConfig.ApplicationNamespace = ""
				clusterConfig.Release.Name = ""
			}()

			result := GetApplicationNamespace()
			if result != tc.expectedNamespace {
				t.Errorf("GetApplicationNamespace() = %q, want %q", result, tc.expectedNamespace)
			}
		})
	}
}

func TestSetApplicationNamespace(t *testing.T) {
	testCases := []struct {
		name               string
		platform           common.Platform
		existingNamespaces []corev1.Namespace
		expectedNamespace  string
		expectError        bool
		errorMsg           string
	}{
		{
			name:     "ManagedRhoai always uses redhat-ods-applications",
			platform: ManagedRhoai,
			existingNamespaces: []corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "custom-labeled-ns",
						Labels: map[string]string{
							"opendatahub.io/application-namespace": "true",
						},
					},
				},
			},
			expectedNamespace: "redhat-ods-applications",
			expectError:       false,
		},
		{
			name:     "OpenDataHub with labeled namespace",
			platform: OpenDataHub,
			existingNamespaces: []corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "custom-odh-ns",
						Labels: map[string]string{
							"opendatahub.io/application-namespace": "true",
						},
					},
				},
			},
			expectedNamespace: "custom-odh-ns",
			expectError:       false,
		},
		{
			name:               "OpenDataHub without labeled namespace uses default",
			platform:           OpenDataHub,
			existingNamespaces: []corev1.Namespace{},
			expectedNamespace:  "opendatahub",
			expectError:        false,
		},
		{
			name:     "SelfManagedRhoai with labeled namespace",
			platform: SelfManagedRhoai,
			existingNamespaces: []corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "custom-rhoai-ns",
						Labels: map[string]string{
							"opendatahub.io/application-namespace": "true",
						},
					},
				},
			},
			expectedNamespace: "custom-rhoai-ns",
			expectError:       false,
		},
		{
			name:               "SelfManagedRhoai without labeled namespace uses default",
			platform:           SelfManagedRhoai,
			existingNamespaces: []corev1.Namespace{},
			expectedNamespace:  "redhat-ods-applications",
			expectError:        false,
		},
		{
			name:     "Error when multiple labeled namespaces exist",
			platform: OpenDataHub,
			existingNamespaces: []corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "namespace-1",
						Labels: map[string]string{
							"opendatahub.io/application-namespace": "true",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "namespace-2",
						Labels: map[string]string{
							"opendatahub.io/application-namespace": "true",
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "only one namespace with label opendatahub.io/application-namespace: true is supported",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup: Create fake client with namespaces
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)

			objs := make([]client.Object, len(tc.existingNamespaces))
			for i := range tc.existingNamespaces {
				objs[i] = &tc.existingNamespaces[i]
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				Build()

			// Setup cluster config with platform
			clusterConfig.Release.Name = tc.platform
			defer func() {
				// Reset after test
				clusterConfig.ApplicationNamespace = ""
				clusterConfig.Release.Name = ""
			}()

			// Execute - call internal function directly
			ctx := context.Background()
			err := setApplicationNamespace(ctx, fakeClient)

			// Assert error
			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				} else if err.Error() != tc.errorMsg {
					t.Errorf("Expected error %q, got %q", tc.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Assert namespace
			result := GetApplicationNamespace()
			if result != tc.expectedNamespace {
				t.Errorf("Application namespace = %q, want %q", result, tc.expectedNamespace)
			}
		})
	}
}
