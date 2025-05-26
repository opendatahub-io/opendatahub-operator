package dscinitialization_test

import (
	"context"
	"testing"

	"github.com/rs/xid"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

// TestDSCIWebhook_Integration exercises the validating webhook logic for DSCInitialization resources.
// It uses table-driven tests to verify singleton enforcement and deletion restrictions in a real envtest environment.
func TestDSCIWebhook_Integration(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		setup func(ns string) []client.Object
		test  func(g Gomega, ctx context.Context, k8sClient client.Client, ns string)
	}{
		{
			name: "Singleton enforcement: allows creation if none exist, denies if one exists",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				dsci1 := envtestutil.NewDSCI("dsci-one", ns)
				g.Expect(k8sClient.Create(ctx, dsci1)).To(Succeed(), "should allow creation of first DSCI")
				dsci2 := envtestutil.NewDSCI("dsci-two", ns)
				err := k8sClient.Create(ctx, dsci2)
				g.Expect(err).NotTo(Succeed(), "should not allow creation of a second DSCI")
			},
		},
		{
			name: "Deletion restriction: allows deletion if no DSC exists",
			setup: func(ns string) []client.Object {
				return []client.Object{
					envtestutil.NewDSCI("dsci-delete", ns),
				}
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				dsci := &dsciv1.DSCInitialization{}
				key := types.NamespacedName{Name: "dsci-delete", Namespace: ns}
				g.Expect(k8sClient.Get(ctx, key, dsci)).To(Succeed(), "should get DSCI for deletion test")
				g.Expect(k8sClient.Delete(ctx, dsci)).To(Succeed(), "should allow deletion if no DSC exists")
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
					dscinitialization.RegisterWebhooks,
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
