package auth_test

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/auth"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

// TestServiceHandler_GetName validates that the Auth service handler returns the correct
// service name for registration with the service registry. This test ensures:
//
// 1. The service name matches the expected constant
// 2. Service registry integration works properly
// 3. The handler can be properly identified in logs and metrics
//
// The service name is used throughout the operator for service identification,
// logging, and reconciliation tracking.
func TestServiceHandler_GetName(t *testing.T) {
	g := NewWithT(t)
	handler := &auth.ServiceHandler{}

	name := handler.GetName()
	g.Expect(name).Should(Equal(serviceApi.AuthServiceName))
}

// TestServiceHandler_GetManagementState validates that the Auth service returns the
// correct management state across different OpenShift AI platforms. This test ensures:
//
// 1. All platforms return "Managed" state (Auth is always managed)
// 2. Platform-specific behavior is consistent
// 3. The service integrates properly with the operator framework
//
// Management States:
// - Managed: The operator actively manages the resource
// - Unmanaged: The operator ignores the resource
// - Force: The operator manages the resource regardless of conflicts
//
// For Auth, we always return Managed because authentication and authorization
// are critical security components that should always be managed by the operator.
func TestServiceHandler_GetManagementState(t *testing.T) {
	handler := &auth.ServiceHandler{}

	tests := []struct {
		name          string
		platform      common.Platform
		dsci          *dsciv2.DSCInitialization
		expectedState operatorv1.ManagementState
	}{
		{
			name:          "should return Managed for any platform",
			platform:      cluster.OpenDataHub,
			dsci:          &dsciv2.DSCInitialization{},
			expectedState: operatorv1.Managed,
		},
		{
			name:          "should return Managed for self-managed RHOAI",
			platform:      cluster.SelfManagedRhoai,
			dsci:          &dsciv2.DSCInitialization{},
			expectedState: operatorv1.Managed,
		},
		{
			name:          "should return Managed for managed RHOAI",
			platform:      cluster.ManagedRhoai,
			dsci:          &dsciv2.DSCInitialization{},
			expectedState: operatorv1.Managed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			state := handler.GetManagementState(tt.platform, tt.dsci)
			g.Expect(state).Should(Equal(tt.expectedState))
		})
	}
}

// TestServiceHandler_Init validates that the Auth service handler initializes properly
// without errors. This test ensures:
//
// 1. Initialization completes successfully
// 2. No setup errors occur
// 3. The handler is ready for use after initialization
//
// The Init method is called during operator startup to allow services to perform
// any necessary setup before reconciliation begins.
func TestServiceHandler_Init(t *testing.T) {
	g := NewWithT(t)
	handler := &auth.ServiceHandler{}

	err := handler.Init(cluster.OpenDataHub)
	g.Expect(err).ShouldNot(HaveOccurred())
}

// TestIsIntegratedOAuth validates the OpenShift authentication method detection logic.
// This function determines whether the operator should create default admin groups
// based on the cluster's authentication configuration. The test ensures:
//
// 1. IntegratedOAuth is correctly identified as default (should create groups)
// 2. Empty auth type is correctly identified as default (should create groups)
// 3. Custom auth types are correctly identified as non-default (should not create groups)
// 4. Missing authentication objects are handled with proper errors
//
// Authentication Types and Behavior:
// - IntegratedOAuth (default): Create default admin groups
// - "" (empty, default): Create default admin groups
// - "None": Do not create default admin groups
// - Custom types: Do not create default admin groups
// - Missing object: Return error (cluster configuration issue)
//
// This is critical for security because it determines whether the operator will
// automatically create admin groups that could grant elevated access.
func TestIsIntegratedOAuth(t *testing.T) {
	ctx := t.Context()

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
			description:    "IntegratedOAuth should be considered default auth method",
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
			description:    "Empty type should be considered default auth method",
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
			description:    "Other auth types should not be considered default auth method",
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
			description:    "None should not be considered default auth method",
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

			var cli client.Client
			var err error

			if tt.authObject != nil {
				cli, err = fakeclient.New(
					fakeclient.WithObjects(tt.authObject),
				)
			} else {
				cli, err = fakeclient.New()
			}
			g.Expect(err).ShouldNot(HaveOccurred())

			result, err := cluster.IsIntegratedOAuth(ctx, cli)

			if tt.expectError {
				g.Expect(err).Should(HaveOccurred(), tt.description)
			} else {
				g.Expect(err).ShouldNot(HaveOccurred(), tt.description)
				g.Expect(result).Should(Equal(tt.expectedResult), tt.description)
			}
		})
	}
}
