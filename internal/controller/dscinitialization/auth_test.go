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

			authObj := dscinitialization.BuildDefaultAuth(tt.platform)
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
}

func TestManageAuthCR(t *testing.T) {
	tests := []struct {
		name              string
		platform          common.Platform
		isIntegratedOAuth bool
		existingAuth      *serviceApi.Auth
		expectAuthExists  bool
		expectError       bool
	}{
		{
			name:              "IntegratedOAuth: creates Auth when not exists",
			platform:          cluster.OpenDataHub,
			isIntegratedOAuth: true,
			existingAuth:      nil,
			expectAuthExists:  true,
			expectError:       false,
		},
		{
			name:              "IntegratedOAuth: keeps Auth when it exists",
			platform:          cluster.OpenDataHub,
			isIntegratedOAuth: true,
			existingAuth: &serviceApi.Auth{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.AuthInstanceName},
				Spec: serviceApi.AuthSpec{
					AdminGroups:   []string{"odh-admins"},
					AllowedGroups: []string{"system:authenticated"},
				},
			},
			expectAuthExists: true,
			expectError:      false,
		},
		{
			name:              "OIDC: do nothing when Auth does not exist",
			platform:          cluster.OpenDataHub,
			isIntegratedOAuth: false,
			existingAuth:      nil,
			expectAuthExists:  false,
			expectError:       false,
		},
		{
			name:              "OIDC: deletes Auth when it exists",
			platform:          cluster.OpenDataHub,
			isIntegratedOAuth: false,
			existingAuth: &serviceApi.Auth{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.AuthInstanceName},
				Spec: serviceApi.AuthSpec{
					AdminGroups:   []string{"existing-admin"},
					AllowedGroups: []string{"existing-allowed"},
				},
			},
			expectAuthExists: false,
			expectError:      false,
		},
		{
			name:              "IntegratedOAuth: creates Auth with correct admin group for SelfManagedRhoai",
			platform:          cluster.SelfManagedRhoai,
			isIntegratedOAuth: true,
			existingAuth:      nil,
			expectAuthExists:  true,
			expectError:       false,
		},
		{
			name:              "IntegratedOAuth: creates Auth with correct admin group for ManagedRhoai",
			platform:          cluster.ManagedRhoai,
			isIntegratedOAuth: true,
			existingAuth:      nil,
			expectAuthExists:  true,
			expectError:       false,
		},
		{
			name:              "IntegratedOAuth: handles empty platform gracefully",
			platform:          "",
			isIntegratedOAuth: true,
			existingAuth:      nil,
			expectAuthExists:  true,
			expectError:       false,
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

			// Call ManageAuthCR
			err = reconciler.ManageAuthCR(ctx, tt.platform, tt.isIntegratedOAuth)

			// Verify error expectation
			if tt.expectError {
				g.Expect(err).Should(HaveOccurred())
				return
			}
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify Auth resource exists
			auth := &serviceApi.Auth{}
			err = cli.Get(ctx, client.ObjectKey{Name: serviceApi.AuthInstanceName}, auth)

			if tt.expectAuthExists {
				g.Expect(err).ShouldNot(HaveOccurred(), "Auth CR should exist")

				// Verify correct admin group for new Auth CRs
				if tt.existingAuth == nil {
					expectedAdminGroup := expectedAdminGroupForPlatform(tt.platform)
					g.Expect(auth.Spec.AdminGroups).Should(HaveLen(1))
					g.Expect(auth.Spec.AdminGroups[0]).Should(Equal(expectedAdminGroup))
					g.Expect(auth.Spec.AllowedGroups).Should(Equal([]string{"system:authenticated"}))
				}
			} else {
				g.Expect(err).Should(HaveOccurred(), "Auth CR should not exist")
			}
		})
	}
}
