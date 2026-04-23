package cluster

import (
	"context"
	"testing"

	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
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

func TestSetApplicationNamespace_RHAIOverride(t *testing.T) {
	testCases := []struct {
		name              string
		rhaiNamespace     string
		platform          common.Platform
		labeledNamespace  string
		expectedNamespace string
	}{
		{
			name:              "SetApplicationNamespace overrides ManagedRhoai default",
			rhaiNamespace:     "my-custom-namespace",
			platform:          ManagedRhoai,
			expectedNamespace: "my-custom-namespace",
		},
		{
			name:              "SetApplicationNamespace overrides SelfManagedRhoai default",
			rhaiNamespace:     "my-custom-namespace",
			platform:          SelfManagedRhoai,
			expectedNamespace: "my-custom-namespace",
		},
		{
			name:              "SetApplicationNamespace overrides labeled namespace",
			rhaiNamespace:     "my-custom-namespace",
			platform:          OpenDataHub,
			labeledNamespace:  "labeled-ns",
			expectedNamespace: "my-custom-namespace",
		},
		{
			name:              "Empty SetApplicationNamespace keeps platform default",
			rhaiNamespace:     "",
			platform:          OpenDataHub,
			expectedNamespace: "opendatahub",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)

			var objs []client.Object
			if tc.labeledNamespace != "" {
				objs = append(objs, &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: tc.labeledNamespace,
						Labels: map[string]string{
							"opendatahub.io/application-namespace": "true",
						},
					},
				})
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				Build()

			clusterConfig.Release.Name = tc.platform
			defer func() {
				clusterConfig.ApplicationNamespace = ""
				clusterConfig.Release.Name = ""
			}()

			// Simulate cluster.Init() detecting the platform namespace
			ctx := context.Background()
			if err := setApplicationNamespace(ctx, fakeClient); err != nil {
				t.Fatalf("setApplicationNamespace failed: %v", err)
			}

			// Simulate main.go calling SetRHAIApplicationNamespace with the Viper-loaded value
			SetRHAIApplicationNamespace(tc.rhaiNamespace)

			result := GetApplicationNamespace()
			if result != tc.expectedNamespace {
				t.Errorf("GetApplicationNamespace() = %q, want %q", result, tc.expectedNamespace)
			}
		})
	}
}

func TestGetClusterInfo_XKSPlatformType(t *testing.T) {
	t.Run("ODH_PLATFORM_TYPE=XKS sets cluster type to Kubernetes without API detection", func(t *testing.T) {
		// getClusterInfo reads clusterConfig.Release.Name which is set by getRelease()
		// during Init().  Simulate that here as the other internal tests do.
		clusterConfig.Release.Name = XKS
		defer func() { clusterConfig.Release.Name = "" }()

		// Minimal fake client — getClusterInfo skips all OCP API calls for XKS.
		fakeClient := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()

		info, err := getClusterInfo(context.Background(), fakeClient)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if info.Type != ClusterTypeKubernetes {
			t.Errorf("ClusterInfo.Type = %q, want %q", info.Type, ClusterTypeKubernetes)
		}
	})
}

func TestApplicationNamespaceFallback(t *testing.T) {
	testCases := []struct {
		name              string
		rhaiNamespace     string
		dsciNamespace     string
		expectedNamespace string
		expectError       bool
	}{
		{
			name:              "returns RHAI namespace when explicitly set, regardless of DSCI",
			rhaiNamespace:     "my-rhai-ns",
			dsciNamespace:     "",
			expectedNamespace: "my-rhai-ns",
		},
		{
			name:              "returns RHAI namespace even when DSCI also exists",
			rhaiNamespace:     "my-rhai-ns",
			dsciNamespace:     "dsci-app-ns",
			expectedNamespace: "my-rhai-ns",
		},
		{
			name:              "returns DSCI namespace when RHAI not set and DSCI exists",
			rhaiNamespace:     "",
			dsciNamespace:     "dsci-app-ns",
			expectedNamespace: "dsci-app-ns",
		},
		{
			name:          "propagates error when RHAI not set and DSCI missing",
			rhaiNamespace: "",
			dsciNamespace: "",
			expectError:   true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			_ = dsciv2.AddToScheme(scheme)

			var objs []client.Object
			if tc.dsciNamespace != "" {
				objs = append(objs, &dsciv2.DSCInitialization{
					ObjectMeta: metav1.ObjectMeta{Name: "default"},
					Spec:       dsciv2.DSCInitializationSpec{ApplicationsNamespace: tc.dsciNamespace},
				})
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				Build()

			viper.Set("rhai-applications-namespace", tc.rhaiNamespace)
			defer func() {
				viper.Set("rhai-applications-namespace", "")
			}()

			ctx := context.Background()
			result, err := ApplicationNamespace(ctx, fakeClient)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error but got nil (result=%q)", result)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if result != tc.expectedNamespace {
				t.Errorf("ApplicationNamespace() = %q, want %q", result, tc.expectedNamespace)
			}
		})
	}
}
