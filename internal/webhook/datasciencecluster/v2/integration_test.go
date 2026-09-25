package v2_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/rs/xid"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	v2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v2"
	v3webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v3"
	dsciv3webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v1"
	dsciv2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

// createDSCI creates a DSCInitialization object in the given namespace and waits for it to become available.
// g: Gomega assertion helper. ctx: context for API calls. env: envtest environment. ns: namespace for the object.
func createDSCI(g Gomega, ctx context.Context, k8sClient client.Client) {
	dsci := envtestutil.NewDSCI("dsci-for-dsc")
	g.Expect(k8sClient.Create(ctx, dsci)).To(Succeed())
	// Wait for the object to be available
	g.Eventually(func() error {
		return k8sClient.Get(ctx, client.ObjectKeyFromObject(dsci), dsci)
	}, "10s", "1s").Should(Succeed(), "DSCI object should be available after creation")
}

// WithModelRegistryDefaulting returns a functional option that sets ModelRegistry fields to trigger defaulting logic in tests.
func WithModelRegistryDefaulting() func(*dscv2.DataScienceCluster) {
	return func(dsc *dscv2.DataScienceCluster) {
		dsc.Spec.Components.ModelRegistry.ManagementState = operatorv1.Managed
		dsc.Spec.Components.ModelRegistry.RegistriesNamespace = ""
	}
}

// TestDataScienceClusterV2_Integration exercises the validating and defaulting webhook logic for DataScienceCluster v2 resources.
// It uses table-driven tests to verify singleton enforcement, deletion, and defaulting behavior in a real envtest environment.
func TestDataScienceClusterV2_Integration(t *testing.T) {
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
				dsc := envtestutil.NewDSCV2("dsc-one")
				g.Expect(k8sClient.Create(ctx, dsc)).To(Succeed(), "should allow creation of a DataScienceCluster v2 when none exist")
			},
		},
		{
			name: "Denies creation if one already exists",
			setup: func(ns string) []client.Object {
				return []client.Object{
					envtestutil.NewDSCV2("existing"),
				}
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				dsc := envtestutil.NewDSCV2("dsc-two")
				err := k8sClient.Create(ctx, dsc)
				g.Expect(err).NotTo(Succeed(), "should not allow creation of a second DataScienceCluster v2")
			},
		},
		{
			name: "Allows deletion always",
			setup: func(ns string) []client.Object {
				return []client.Object{
					envtestutil.NewDSCV2("dsc-delete"),
				}
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				dsc := &dscv2.DataScienceCluster{}
				key := types.NamespacedName{Name: "dsc-delete", Namespace: ns}
				g.Expect(k8sClient.Get(ctx, key, dsc)).To(Succeed(), "should find the DataScienceCluster v2 created in setup")
				g.Expect(dsc.Name).To(Equal("dsc-delete"), "should have the expected name")
				g.Expect(k8sClient.Delete(ctx, dsc)).To(Succeed(), "should allow deletion of DataScienceCluster v2")
			},
		},
		{
			name: "Defaulting: sets ModelRegistry.RegistriesNamespace if empty and Managed",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				dsc := envtestutil.NewDSCV2("dsc-defaulting", WithModelRegistryDefaulting())
				g.Expect(k8sClient.Create(ctx, dsc)).To(Succeed(), "should allow creation of DataScienceCluster v2 for defaulting test")

				fetched := &dscv2.DataScienceCluster{}
				g.Eventually(func() string {
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: "dsc-defaulting", Namespace: ns}, fetched); err != nil {
						t.Logf("Get failed in Eventually polling: %v", err)
						return ""
					}
					return fetched.Spec.Components.ModelRegistry.RegistriesNamespace
				}).Should(Equal(componentApi.DefaultModelRegistriesNamespace), "should set ModelRegistry.RegistriesNamespace to default when empty and Managed")
			},
		},
		{
			name: "CEL: RegistriesNamespace immutable when ModelRegistry Managed; mutable when Removed",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				dsc := envtestutil.NewDSCV2("dsc-mr-cel", WithModelRegistryDefaulting())
				g.Expect(k8sClient.Create(ctx, dsc)).To(Succeed(), "should create DSC with ModelRegistry Managed and empty RegistriesNamespace")

				key := types.NamespacedName{Name: "dsc-mr-cel", Namespace: ns}
				fetched := &dscv2.DataScienceCluster{}
				g.Eventually(func() string {
					if err := k8sClient.Get(ctx, key, fetched); err != nil {
						return ""
					}
					return fetched.Spec.Components.ModelRegistry.RegistriesNamespace
				}).Should(Equal(componentApi.DefaultModelRegistriesNamespace), "mutating webhook should default RegistriesNamespace")

				g.Expect(k8sClient.Get(ctx, key, fetched)).To(Succeed())
				fetched.Spec.Components.ModelRegistry.RegistriesNamespace = "other-registry-ns"
				err := k8sClient.Update(ctx, fetched)
				g.Expect(err).To(HaveOccurred(), "CEL should reject changing RegistriesNamespace while Managed")
				g.Expect(err.Error()).To(ContainSubstring("RegistriesNamespace"))

				g.Expect(k8sClient.Get(ctx, key, fetched)).To(Succeed())
				fetched.Spec.Components.ModelRegistry.ManagementState = operatorv1.Removed
				g.Expect(k8sClient.Update(ctx, fetched)).To(Succeed(), "should allow setting ModelRegistry to Removed")

				g.Expect(k8sClient.Get(ctx, key, fetched)).To(Succeed())
				fetched.Spec.Components.ModelRegistry.RegistriesNamespace = "custom-ns-under-removed"
				g.Expect(k8sClient.Update(ctx, fetched)).To(Succeed(), "should allow changing RegistriesNamespace when not Managed")

				g.Expect(k8sClient.Get(ctx, key, fetched)).To(Succeed())
				g.Expect(fetched.Spec.Components.ModelRegistry.RegistriesNamespace).To(Equal("custom-ns-under-removed"))

				fetched.Spec.Components.ModelRegistry.ManagementState = operatorv1.Managed
				g.Expect(k8sClient.Update(ctx, fetched)).To(Succeed(), "should allow transition back to Managed with existing namespace")

				g.Expect(k8sClient.Get(ctx, key, fetched)).To(Succeed())
				fetched.Spec.Components.ModelRegistry.RegistriesNamespace = "another-ns"
				err = k8sClient.Update(ctx, fetched)
				g.Expect(err).To(HaveOccurred(), "CEL should reject changing RegistriesNamespace after Managed again")
				g.Expect(err.Error()).To(ContainSubstring("RegistriesNamespace"))
			},
		},
		{
			name: "Webhook: TrainingOperator (KFTO v1) re-enablement blocked on update",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: retiredOperatorManagedTest("dsc-kfto-webhook", func(dsc *dscv2.DataScienceCluster) {
				dsc.Spec.Components.TrainingOperator.ManagementState = operatorv1.Managed
			}, true, "obsolete"),
		},
		{
			name: "Webhook: TrainingOperator (KFTO v1) Managed rejected on create",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: retiredOperatorManagedTest("dsc-kfto-webhook-managed", func(dsc *dscv2.DataScienceCluster) {
				dsc.Spec.Components.TrainingOperator.ManagementState = operatorv1.Managed
			}, false, "obsolete"),
		},
		{
			name: "Webhook: LlamaStackOperator re-enablement blocked on update",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: retiredOperatorManagedTest("dsc-llamastack-webhook", func(dsc *dscv2.DataScienceCluster) {
				dsc.Spec.Components.LlamaStackOperator.ManagementState = operatorv1.Managed
			}, true, "replaced by OGX"),
		},
		{
			name: "Webhook: LlamaStackOperator Managed rejected on create",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: retiredOperatorManagedTest("dsc-llamastack-webhook-managed", func(dsc *dscv2.DataScienceCluster) {
				dsc.Spec.Components.LlamaStackOperator.ManagementState = operatorv1.Managed
			}, false, "replaced by OGX"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			t.Logf("Starting test case: %s", tc.name)
			ctx, env, teardown := envtestutil.SetupEnvAndClient(
				t,
				[]envt.RegisterWebhooksFn{
					v3webhook.RegisterWebhooks,
					dsciv3webhook.RegisterWebhooks,
					v2webhook.RegisterWebhooks,
					dsciv2webhook.RegisterWebhooks,
				},
				[]envt.RegisterControllersFn{},
				envtestutil.DefaultWebhookTimeout,
			)
			t.Cleanup(teardown)

			ns := xid.New().String()
			t.Logf("Using namespace: %s", ns)
			createDSCI(NewWithT(t), ctx, env.Client())

			if tc.setup != nil {
				for _, obj := range tc.setup(ns) {
					t.Logf("Creating setup object: %T %s", obj, client.ObjectKeyFromObject(obj))
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

func retiredOperatorManagedTest(
	name string,
	setManaged func(*dscv2.DataScienceCluster),
	update bool,
	message string,
) func(Gomega, context.Context, client.Client, string) {
	return func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
		dsc := envtestutil.NewDSCV2(name)
		if update {
			g.Expect(k8sClient.Create(ctx, dsc)).To(Succeed(), "should allow creation before attempting re-enablement")
			key := types.NamespacedName{Name: name, Namespace: ns}
			g.Expect(k8sClient.Get(ctx, key, dsc)).To(Succeed())
			setManaged(dsc)
			g.Expect(k8sClient.Update(ctx, dsc)).To(MatchError(ContainSubstring(message)))
			return
		}

		setManaged(dsc)
		g.Expect(k8sClient.Create(ctx, dsc)).To(MatchError(ContainSubstring(message)))
	}
}
