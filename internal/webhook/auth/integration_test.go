package auth_test

import (
	"context"
	"testing"

	"github.com/rs/xid"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/auth"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

// TestAuthWebhook_Integration exercises the validating webhook logic for Auth resources.
// It uses table-driven tests to verify group validation in a real envtest environment.
func TestAuthWebhook_Integration(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		setup func(ns string) []client.Object
		test  func(g Gomega, ctx context.Context, k8sClient client.Client, ns string)
	}{
		{
			name: "Valid groups: allows creation and update",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				// Test creation with valid groups
				validAuth := envtestutil.NewAuth("auth", ns, []string{"admin-group"}, []string{"allowed-group"})
				g.Expect(k8sClient.Create(ctx, validAuth)).To(Succeed(), "should allow creation with valid groups")

				// Test update with valid groups
				validAuth.Spec.AdminGroups = []string{"updated-admin-group"}
				validAuth.Spec.AllowedGroups = []string{"updated-allowed-group"}
				g.Expect(k8sClient.Update(ctx, validAuth)).To(Succeed(), "should allow update with valid groups")
			},
		},
		{
			name: "Invalid AdminGroups: denies creation with system:authenticated",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				invalidAuth := envtestutil.NewAuth("auth", ns, []string{"admin-group", "system:authenticated"}, []string{"allowed-group"})
				err := k8sClient.Create(ctx, invalidAuth)
				g.Expect(err).NotTo(Succeed(), "should deny creation with system:authenticated in AdminGroups")
				g.Expect(err.Error()).To(ContainSubstring("Invalid groups found in AdminGroups"), "error should mention AdminGroups")
				g.Expect(err.Error()).To(ContainSubstring("system:authenticated"), "error should mention system:authenticated")
			},
		},
		{
			name: "Invalid AdminGroups: denies creation with empty string",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				invalidAuth := envtestutil.NewAuth("auth", ns, []string{"admin-group", ""}, []string{"allowed-group"})
				err := k8sClient.Create(ctx, invalidAuth)
				g.Expect(err).NotTo(Succeed(), "should deny creation with empty string in AdminGroups")
				g.Expect(err.Error()).To(ContainSubstring("Invalid groups found in AdminGroups"), "error should mention AdminGroups")
			},
		},
		{
			name: "Valid AllowedGroups: allows creation with system:authenticated",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				validAuth := envtestutil.NewAuth("auth", ns, []string{"admin-group"}, []string{"allowed-group", "system:authenticated"})
				g.Expect(k8sClient.Create(ctx, validAuth)).To(Succeed(), "should allow creation with system:authenticated in AllowedGroups")
			},
		},
		{
			name: "Invalid AllowedGroups: denies creation with empty string",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				invalidAuth := envtestutil.NewAuth("auth", ns, []string{"admin-group"}, []string{"allowed-group", ""})
				err := k8sClient.Create(ctx, invalidAuth)
				g.Expect(err).NotTo(Succeed(), "should deny creation with empty string in AllowedGroups")
				g.Expect(err.Error()).To(ContainSubstring("Invalid groups found in AllowedGroups"), "error should mention AllowedGroups")
			},
		},
		{
			name: "Multiple invalid groups: denies creation with comprehensive error",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				invalidAuth := envtestutil.NewAuth("auth", ns, []string{"admin-group", "system:authenticated", ""}, []string{"allowed-group"})
				err := k8sClient.Create(ctx, invalidAuth)
				g.Expect(err).NotTo(Succeed(), "should deny creation with multiple invalid groups")
				g.Expect(err.Error()).To(ContainSubstring("Invalid groups found in AdminGroups"), "error should mention AdminGroups")
				g.Expect(err.Error()).To(ContainSubstring("system:authenticated"), "error should mention system:authenticated")
			},
		},
		{
			name: "Update validation: denies update to invalid groups",
			setup: func(ns string) []client.Object {
				// Create a valid Auth resource first
				return []client.Object{
					envtestutil.NewAuth("auth", ns, []string{"admin-group"}, []string{"allowed-group"}),
				}
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				// Get the existing Auth resource
				auth := &serviceApi.Auth{}
				key := types.NamespacedName{Name: "auth", Namespace: ns}
				g.Expect(k8sClient.Get(ctx, key, auth)).To(Succeed(), "should get Auth for update test")

				// Try to update with invalid groups (empty string)
				auth.Spec.AllowedGroups = []string{"allowed-group", ""}
				err := k8sClient.Update(ctx, auth)
				g.Expect(err).NotTo(Succeed(), "should deny update with invalid groups")
				g.Expect(err.Error()).To(ContainSubstring("Invalid groups found in AllowedGroups"), "error should mention AllowedGroups")
			},
		},
		{
			name: "Deletion: allows deletion regardless of group values",
			setup: func(ns string) []client.Object {
				// Create an Auth resource (even with technically invalid groups for deletion test)
				return []client.Object{
					envtestutil.NewAuth("auth", ns, []string{"admin-group"}, []string{"allowed-group"}),
				}
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				auth := &serviceApi.Auth{}
				key := types.NamespacedName{Name: "auth", Namespace: ns}
				g.Expect(k8sClient.Get(ctx, key, auth)).To(Succeed(), "should get Auth for deletion test")
				g.Expect(k8sClient.Delete(ctx, auth)).To(Succeed(), "should allow deletion regardless of group values")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			t.Logf("Starting test case: %s", tc.name)
			ctx, env, teardown := envtestutil.SetupEnvAndClient(
				t,
				[]envt.RegisterWebhooksFn{
					auth.RegisterWebhooks,
				},
				envtestutil.DefaultWebhookTimeout,
			)
			t.Cleanup(teardown)

			ns := xid.New().String()
			t.Logf("Using namespace: %s", ns)
			if tc.setup != nil {
				for _, obj := range tc.setup(ns) {
					t.Logf("Creating setup object: %+v", obj)
					g := NewWithT(t)
					g.Expect(env.Client().Create(ctx, obj)).To(Succeed(), "setup object creation should succeed")
				}
			}
			g := NewWithT(t)
			tc.test(g, ctx, env.Client(), ns)
			t.Logf("Finished test case: %s", tc.name)
		})
	}
}
