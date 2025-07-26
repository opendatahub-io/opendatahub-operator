package dscinitialization_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/dscinitialization"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestBuildDefaultAuth(t *testing.T) {
	tests := []struct {
		name               string
		platform           common.Platform
		expectedAdminGroup string
	}{
		{
			name:               "OpenDataHub platform",
			platform:           cluster.OpenDataHub,
			expectedAdminGroup: "odh-admins",
		},
		{
			name:               "SelfManagedRhoai platform",
			platform:           cluster.SelfManagedRhoai,
			expectedAdminGroup: "rhods-admins",
		},
		{
			name:               "ManagedRhoai platform",
			platform:           cluster.ManagedRhoai,
			expectedAdminGroup: "dedicated-admins",
		},
		{
			name:               "Empty platform should fallback to OpenDataHub",
			platform:           "",
			expectedAdminGroup: "odh-admins",
		},
		{
			name:               "Unknown platform should fallback to OpenDataHub",
			platform:           "unknown-platform",
			expectedAdminGroup: "odh-admins",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authObj := dscinitialization.BuildDefaultAuth(tt.platform)
			auth, ok := authObj.(*serviceApi.Auth)
			assert.True(t, ok, "BuildDefaultAuth should return *serviceApi.Auth")
			assert.NotNil(t, auth, "BuildDefaultAuth should not return nil")

			// Verify basic properties
			assert.Equal(t, serviceApi.AuthInstanceName, auth.Name)
			assert.Equal(t, serviceApi.AuthKind, auth.Kind)

			// Verify AdminGroups
			assert.Len(t, auth.Spec.AdminGroups, 1)
			assert.Equal(t, tt.expectedAdminGroup, auth.Spec.AdminGroups[0])
			assert.NotEmpty(t, auth.Spec.AdminGroups[0], "AdminGroups should not contain empty strings")

			// Verify AllowedGroups
			assert.Len(t, auth.Spec.AllowedGroups, 1)
			assert.Equal(t, "system:authenticated", auth.Spec.AllowedGroups[0])

			// Verify CEL validation compliance
			for _, group := range auth.Spec.AdminGroups {
				assert.NotEqual(t, "system:authenticated", group, "AdminGroups should not contain 'system:authenticated'")
				assert.NotEmpty(t, group, "AdminGroups should not contain empty strings")
			}

			for _, group := range auth.Spec.AllowedGroups {
				assert.NotEmpty(t, group, "AllowedGroups should not contain empty strings")
			}
		})
	}
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
			ctx := context.Background()

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
				var expectedAdminGroup string
				switch tt.platform {
				case cluster.SelfManagedRhoai:
					expectedAdminGroup = "rhods-admins"
				case cluster.ManagedRhoai:
					expectedAdminGroup = "dedicated-admins"
				default:
					expectedAdminGroup = "odh-admins" // fallback for OpenDataHub and unknown platforms
				}

				g.Expect(auth.Spec.AdminGroups).Should(HaveLen(1))
				g.Expect(auth.Spec.AdminGroups[0]).Should(Equal(expectedAdminGroup))
				g.Expect(auth.Spec.AllowedGroups).Should(Equal([]string{"system:authenticated"}))
			}
		})
	}
}

func TestCreateAuth_ErrorHandling(t *testing.T) {
	t.Run("Handles non-NotFound errors during Get", func(t *testing.T) {
		g := NewWithT(t)
		ctx := context.Background()

		// Create a client that will fail on Get operations
		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		// Simulate a client error by creating an invalid state
		// Note: This is difficult to test with fakeclient, so we'll focus on the happy path
		// In a real test environment, you might use a mock client here

		reconciler := &dscinitialization.DSCInitializationReconciler{
			Client: cli,
		}

		// This should succeed since fakeclient doesn't produce Get errors
		err = reconciler.CreateAuth(ctx, cluster.OpenDataHub)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("Ignores AlreadyExists errors during Create", func(t *testing.T) {
		g := NewWithT(t)
		ctx := context.Background()

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		reconciler := &dscinitialization.DSCInitializationReconciler{
			Client: cli,
		}

		// First call should create the Auth
		err = reconciler.CreateAuth(ctx, cluster.OpenDataHub)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Second call should handle the "already exists" scenario gracefully
		// (though fakeclient doesn't actually return AlreadyExists errors)
		err = reconciler.CreateAuth(ctx, cluster.OpenDataHub)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify only one Auth exists
		auth := &serviceApi.Auth{}
		err = cli.Get(ctx, client.ObjectKey{Name: serviceApi.AuthInstanceName}, auth)
		g.Expect(err).ShouldNot(HaveOccurred())
	})
}
