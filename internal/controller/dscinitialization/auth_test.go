package dscinitialization_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/dscinitialization"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

// expectedAdminGroupForPlatform returns the expected admin group for a given platform.
func expectedAdminGroupForPlatform(platform common.Platform) string {
	switch platform {
	case cluster.SelfManagedRhoai:
		return "rhods-admins"
	case cluster.ManagedRhoai:
		return "dedicated-admins"
	default:
		return "odh-admins" // fallback for OpenDataHub and unknown platforms
	}
}

func TestBuildDefaultAuth(t *testing.T) {
	tests := []struct {
		name               string
		platform           common.Platform
		expectedAdminGroup string
	}{
		{
			name:               "OpenDataHub platform",
			platform:           cluster.OpenDataHub,
			expectedAdminGroup: expectedAdminGroupForPlatform(cluster.OpenDataHub),
		},
		{
			name:               "SelfManagedRhoai platform",
			platform:           cluster.SelfManagedRhoai,
			expectedAdminGroup: expectedAdminGroupForPlatform(cluster.SelfManagedRhoai),
		},
		{
			name:               "ManagedRhoai platform",
			platform:           cluster.ManagedRhoai,
			expectedAdminGroup: expectedAdminGroupForPlatform(cluster.ManagedRhoai),
		},
		{
			name:               "Empty platform should fallback to OpenDataHub",
			platform:           "",
			expectedAdminGroup: expectedAdminGroupForPlatform(""),
		},
		{
			name:               "Unknown platform should fallback to OpenDataHub",
			platform:           "unknown-platform",
			expectedAdminGroup: expectedAdminGroupForPlatform("unknown-platform"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			// Create fake client (will be Oauth mode by default)
			cli, err := fakeclient.New()
			g.Expect(err).ShouldNot(HaveOccurred())

			authObj := dscinitialization.BuildDefaultAuth(ctx, cli, tt.platform)
			g.Expect(authObj).ShouldNot(BeNil(), "BuildDefaultAuth should not return nil")

			auth, ok := authObj.(*serviceApi.Auth)
			g.Expect(ok).Should(BeTrue(), "BuildDefaultAuth should return *serviceApi.Auth")
			g.Expect(auth).ShouldNot(BeNil(), "Auth object should not be nil after type assertion")

			// Verify basic properties
			g.Expect(auth.Name).Should(Equal(serviceApi.AuthInstanceName))
			g.Expect(auth.Kind).Should(Equal(serviceApi.AuthKind))

			// Verify AdminGroups
			g.Expect(auth.Spec.AdminGroups).Should(HaveLen(1))
			g.Expect(auth.Spec.AdminGroups[0]).Should(Equal(tt.expectedAdminGroup))
			g.Expect(auth.Spec.AdminGroups[0]).ShouldNot(BeEmpty(), "AdminGroups should not contain empty strings")

			// Verify AllowedGroups
			g.Expect(auth.Spec.AllowedGroups).Should(HaveLen(1))
			g.Expect(auth.Spec.AllowedGroups[0]).Should(Equal("system:authenticated"))
		})
	}
	t.Run("OIDC mode uses placeholder group", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()
		cli, err := fakeclient.New(fakeclient.WithClusterAuthType(cluster.AuthModeOIDC))
		g.Expect(err).ShouldNot(HaveOccurred())
		obj := dscinitialization.BuildDefaultAuth(ctx, cli, cluster.OpenDataHub)
		auth, ok := obj.(*serviceApi.Auth)
		g.Expect(ok).To(BeTrue())
		g.Expect(auth.Spec.AdminGroups).To(HaveLen(1))
		g.Expect(auth.Spec.AdminGroups[0]).To(Equal("REPLACE-WITH-OIDC-ADMIN-GROUP"))
		// AllowedGroups should remain unchanged
		g.Expect(auth.Spec.AllowedGroups).To(Equal([]string{"system:authenticated"}))
	})
}

func TestCreateAuth(t *testing.T) {
	tests := []struct {
		name         string
		platform     common.Platform
		existingAuth *serviceApi.Auth
		expectError  bool
	}{
		{
			name:         "Creates Auth when none exists - OpenDataHub",
			platform:     cluster.OpenDataHub,
			existingAuth: nil,
			expectError:  false,
		},
		{
			name:         "Creates Auth when none exists - SelfManagedRhoai",
			platform:     cluster.SelfManagedRhoai,
			existingAuth: nil,
			expectError:  false,
		},
		{
			name:         "Creates Auth when none exists - ManagedRhoai",
			platform:     cluster.ManagedRhoai,
			existingAuth: nil,
			expectError:  false,
		},
		{
			name:     "Does nothing when Auth already exists",
			platform: cluster.OpenDataHub,
			existingAuth: &serviceApi.Auth{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.AuthInstanceName},
				Spec: serviceApi.AuthSpec{
					AdminGroups:   []string{"existing-admin"},
					AllowedGroups: []string{"existing-allowed"},
				},
			},
			expectError: false,
		},
		{
			name:         "Handles empty platform gracefully",
			platform:     "",
			existingAuth: nil,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			// Setup fake client
			var objects []client.Object
			if tt.existingAuth != nil {
				objects = append(objects, tt.existingAuth)
			}

			cli, err := fakeclient.New(fakeclient.WithObjects(objects...))
			g.Expect(err).ShouldNot(HaveOccurred())

			// Create reconciler
			reconciler := &dscinitialization.DSCInitializationReconciler{
				Client: cli,
			}

			// Call CreateAuth
			err = reconciler.CreateAuth(ctx, tt.platform)

			// Verify error expectation
			if tt.expectError {
				g.Expect(err).Should(HaveOccurred())
				return
			}
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify Auth resource exists
			auth := &serviceApi.Auth{}
			err = cli.Get(ctx, client.ObjectKey{Name: serviceApi.AuthInstanceName}, auth)
			g.Expect(err).ShouldNot(HaveOccurred())

			if tt.existingAuth != nil {
				// Should preserve existing Auth unchanged
				g.Expect(auth.Spec.AdminGroups).Should(Equal(tt.existingAuth.Spec.AdminGroups))
				g.Expect(auth.Spec.AllowedGroups).Should(Equal(tt.existingAuth.Spec.AllowedGroups))
			} else {
				// Should create new Auth with correct admin group
				expectedAdminGroup := expectedAdminGroupForPlatform(tt.platform)

				g.Expect(auth.Spec.AdminGroups).Should(HaveLen(1))
				g.Expect(auth.Spec.AdminGroups[0]).Should(Equal(expectedAdminGroup))
				g.Expect(auth.Spec.AllowedGroups).Should(Equal([]string{"system:authenticated"}))
			}
		})
	}
}

func TestCreateAuth_ErrorHandling(t *testing.T) {
	t.Run("Succeeds with clean fakeclient", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		// Create a clean fakeclient for testing successful CreateAuth flow
		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		// Note: For actual error testing, a mock client would be needed
		// as fakeclient doesn't simulate Get/Create failures easily

		reconciler := &dscinitialization.DSCInitializationReconciler{
			Client: cli,
		}

		// This should succeed with a clean fakeclient
		err = reconciler.CreateAuth(ctx, cluster.OpenDataHub)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("Maintains idempotency on multiple calls", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		reconciler := &dscinitialization.DSCInitializationReconciler{
			Client: cli,
		}

		// First call should create the Auth
		err = reconciler.CreateAuth(ctx, cluster.OpenDataHub)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Second call should be idempotent and not cause errors
		err = reconciler.CreateAuth(ctx, cluster.OpenDataHub)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify only one Auth exists
		auth := &serviceApi.Auth{}
		err = cli.Get(ctx, client.ObjectKey{Name: serviceApi.AuthInstanceName}, auth)
		g.Expect(err).ShouldNot(HaveOccurred())
	})
}
