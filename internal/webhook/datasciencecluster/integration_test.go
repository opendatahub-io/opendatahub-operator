package datasciencecluster_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/rs/xid"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

// createDSCI creates a DSCInitialization object in the given namespace and waits for it to become available.
// g: Gomega assertion helper. ctx: context for API calls. env: envtest environment. ns: namespace for the object.
func createDSCI(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
	dsci := envtestutil.NewDSCI("dsci-for-dsc", ns)
	g.Expect(k8sClient.Create(ctx, dsci)).To(Succeed())
	// Wait for the object to be available
	g.Eventually(func() error {
		return k8sClient.Get(ctx, client.ObjectKeyFromObject(dsci), dsci)
	}, "10s", "1s").Should(Succeed(), "DSCI object should be available after creation")
}

// WithModelRegistryDefaulting returns a functional option that sets ModelRegistry fields to trigger defaulting logic in tests.
func WithModelRegistryDefaulting() func(*dscv1.DataScienceCluster) {
	return func(dsc *dscv1.DataScienceCluster) {
		dsc.Spec.Components.ModelRegistry.ManagementState = operatorv1.Managed
		dsc.Spec.Components.ModelRegistry.RegistriesNamespace = ""
	}
}

// TestDataScienceCluster_Integration exercises the validating and defaulting webhook logic for DataScienceCluster resources.
// It uses table-driven tests to verify singleton enforcement, deletion, and defaulting behavior in a real envtest environment.
func TestDataScienceCluster_Integration(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		setup func(ns string) []client.Object
		test  func(g Gomega, ctx context.Context, k8sClient client.Client, ns string)
	}{
		{
			name: "Allows creation if none exist",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				dsc := envtestutil.NewDSC("dsc-one", ns)
				g.Expect(k8sClient.Create(ctx, dsc)).To(Succeed(), "should allow creation of a DataScienceCluster when none exist")
			},
		},
		{
			name: "Denies creation if one already exists",
			setup: func(ns string) []client.Object {
				return []client.Object{
					envtestutil.NewDSC("existing", ns),
				}
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				dsc := envtestutil.NewDSC("dsc-two", ns)
				err := k8sClient.Create(ctx, dsc)
				g.Expect(err).NotTo(Succeed(), "should not allow creation of a second DataScienceCluster")
			},
		},
		{
			name: "Allows deletion always",
			setup: func(ns string) []client.Object {
				return []client.Object{
					envtestutil.NewDSC("dsc-delete", ns),
				}
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				dsc := &dscv1.DataScienceCluster{}
				key := types.NamespacedName{Name: "dsc-delete", Namespace: ns}
				g.Expect(k8sClient.Get(ctx, key, dsc)).To(Succeed(), "should find the DataScienceCluster created in setup")
				g.Expect(dsc.Name).To(Equal("dsc-delete"), "should have the expected name")
				g.Expect(k8sClient.Delete(ctx, dsc)).To(Succeed(), "should allow deletion of DataScienceCluster")
			},
		},
		{
			name: "Defaulting: sets ModelRegistry.RegistriesNamespace if empty and Managed",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				dsc := envtestutil.NewDSC("dsc-defaulting", ns, WithModelRegistryDefaulting())
				g.Expect(k8sClient.Create(ctx, dsc)).To(Succeed(), "should allow creation of DataScienceCluster for defaulting test")

				fetched := &dscv1.DataScienceCluster{}
				g.Eventually(func() string {
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: "dsc-defaulting", Namespace: ns}, fetched); err != nil {
						t.Logf("Get failed in Eventually polling: %v", err)
						return ""
					}
					return fetched.Spec.Components.ModelRegistry.RegistriesNamespace
				}).Should(Equal(modelregistryctrl.DefaultModelRegistriesNamespace), "should set ModelRegistry.RegistriesNamespace to default when empty and Managed")
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
					datasciencecluster.RegisterWebhooks,
					dscinitialization.RegisterWebhooks,
				},
				envtestutil.DefaultWebhookTimeout,
			)
			t.Cleanup(teardown)

			ns := xid.New().String()
			t.Logf("Using namespace: %s", ns)
			createDSCI(NewWithT(t), ctx, env.Client(), ns)

			if tc.setup != nil {
				for _, obj := range tc.setup(ns) {
					t.Logf("Creating setup object: %+v", obj)
					g := NewWithT(t)
					g.Expect(env.Client().Create(ctx, obj)).To(Succeed(), "setup object creation should succeed")
					// Verify the object was created
					g.Eventually(func() error {
						return env.Client().Get(ctx, client.ObjectKeyFromObject(obj), obj)
					}, "10s", "1s").Should(Succeed(), "setup object should be available")
				}
			}
			g := NewWithT(t)
			tc.test(g, ctx, env.Client(), ns)
			t.Logf("Finished test case: %s", tc.name)
		})
	}
}
