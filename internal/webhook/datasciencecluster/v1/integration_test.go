package v1_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/rs/xid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	v1webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v1"
	v2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v2"
	dsciv1webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v1"
	dsciv2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

// createDSCI creates a DSCInitialization object in the given namespace and waits for it to become available.
// g: Gomega assertion helper. ctx: context for API calls. env: envtest environment. ns: namespace for the object.
func createDSCIV1(g Gomega, ctx context.Context, k8sClient client.Client) {
	dsci := envtestutil.NewDSCIV1("dsci-for-dsc")
	g.Expect(k8sClient.Create(ctx, dsci)).To(Succeed())
	// Wait for the object to be available
	g.Eventually(func() error {
		return k8sClient.Get(ctx, client.ObjectKeyFromObject(dsci), dsci)
	}, "10s", "1s").Should(Succeed(), "DSCI V1 object should be available after creation")
}

// WithModelRegistryDefaulting returns a functional option that sets ModelRegistry fields to trigger defaulting logic in tests.
func WithModelRegistryDefaulting() func(*dscv1.DataScienceCluster) {
	return func(dsc *dscv1.DataScienceCluster) {
		dsc.Spec.Components.ModelRegistry.ManagementState = operatorv1.Managed
		dsc.Spec.Components.ModelRegistry.RegistriesNamespace = ""
	}
}

// TestDataScienceClusterV1_Integration exercises the validating and defaulting webhook logic for DataScienceCluster v1 resources.
// It uses table-driven tests to verify singleton enforcement, deletion, and defaulting behavior in a real envtest environment.
func TestDataScienceClusterV1_Integration(t *testing.T) {
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
				dsc := envtestutil.NewDSCV1("dsc-one")
				g.Expect(k8sClient.Create(ctx, dsc)).To(Succeed(), "should allow creation of a DataScienceCluster v1 when none exist")
			},
		},
		{
			name: "Denies creation if one already exists",
			setup: func(ns string) []client.Object {
				return []client.Object{
					envtestutil.NewDSCV1("existing"),
				}
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				dsc := envtestutil.NewDSCV1("dsc-two")
				err := k8sClient.Create(ctx, dsc)
				g.Expect(err).NotTo(Succeed(), "should not allow creation of a second DataScienceCluster v1")
			},
		},
		{
			name: "Allows deletion always",
			setup: func(ns string) []client.Object {
				return []client.Object{
					envtestutil.NewDSCV1("dsc-delete"),
				}
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				dsc := &dscv1.DataScienceCluster{}
				key := types.NamespacedName{Name: "dsc-delete", Namespace: ns}
				g.Expect(k8sClient.Get(ctx, key, dsc)).To(Succeed(), "should find the DataScienceCluster v1 created in setup")
				g.Expect(dsc.Name).To(Equal("dsc-delete"), "should have the expected name")
				g.Expect(k8sClient.Delete(ctx, dsc)).To(Succeed(), "should allow deletion of DataScienceCluster v1")
			},
		},
		{
			name: "Defaulting: sets ModelRegistry.RegistriesNamespace if empty and Managed",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				dsc := envtestutil.NewDSCV1("dsc-defaulting", WithModelRegistryDefaulting())
				g.Expect(k8sClient.Create(ctx, dsc)).To(Succeed(), "should allow creation of DataScienceCluster v1 for defaulting test")

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
					v1webhook.RegisterWebhooks,
					dsciv1webhook.RegisterWebhooks,
					v2webhook.RegisterWebhooks,
					dsciv2webhook.RegisterWebhooks,
				},
				envtestutil.DefaultWebhookTimeout,
			)
			t.Cleanup(teardown)

			ns := xid.New().String()
			t.Logf("Using namespace: %s", ns)
			createDSCIV1(NewWithT(t), ctx, env.Client())
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

// TestDataScienceClusterV1V2_ConversionWebhook tests the conversion webhook between DSC v1 and v2.
// It verifies that DataSciencePipelines field is properly renamed to AIPipelines during conversion.
func TestDataScienceClusterV1V2_ConversionWebhook(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		test func(g Gomega, ctx context.Context, k8sClient client.Client, ns string)
	}{
		{
			name: "v1 to v2 conversion: DataSciencePipelines becomes AIPipelines",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				// Create a v1 DSC with DataSciencePipelines set
				dscV1 := envtestutil.NewDSCV1("dsc-v1-to-v2")
				dscV1.Spec.Components.DataSciencePipelines.ManagementState = operatorv1.Managed
				g.Expect(k8sClient.Create(ctx, dscV1)).To(Succeed(), "should create v1 DSC")

				// Fetch as v2 to verify conversion
				dscV2 := &dscv2.DataScienceCluster{}
				g.Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: "dsc-v1-to-v2", Namespace: ns}, dscV2)
				}, "10s", "1s").Should(Succeed(), "should fetch v1 DSC as v2")

				// Verify DataSciencePipelines was converted to AIPipelines
				g.Expect(dscV2.Spec.Components.AIPipelines.ManagementState).To(Equal(operatorv1.Managed),
					"AIPipelines should have the same ManagementState as v1 DataSciencePipelines")
			},
		},
		{
			name: "v2 to v1 conversion: AIPipelines becomes DataSciencePipelines",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				// Create a v2 DSC with AIPipelines set
				dscV2 := envtestutil.NewDSC("dsc-v2-to-v1")
				dscV2.Spec.Components.AIPipelines.ManagementState = operatorv1.Managed
				g.Expect(k8sClient.Create(ctx, dscV2)).To(Succeed(), "should create v2 DSC")

				// Fetch as v1 to verify conversion
				dscV1 := &dscv1.DataScienceCluster{}
				g.Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: "dsc-v2-to-v1", Namespace: ns}, dscV1)
				}, "10s", "1s").Should(Succeed(), "should fetch v2 DSC as v1")

				// Verify AIPipelines was converted to DataSciencePipelines
				g.Expect(dscV1.Spec.Components.DataSciencePipelines.ManagementState).To(Equal(operatorv1.Managed),
					"DataSciencePipelines should have the same ManagementState as v2 AIPipelines")
			},
		},
		{
			name: "Round-trip conversion: v1 -> v2 -> v1 preserves data",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				// Create a v1 DSC with DataSciencePipelines set
				dscV1Original := envtestutil.NewDSCV1("dsc-roundtrip")
				dscV1Original.Spec.Components.DataSciencePipelines.ManagementState = operatorv1.Managed
				g.Expect(k8sClient.Create(ctx, dscV1Original)).To(Succeed(), "should create v1 DSC")

				// Fetch as v2
				dscV2 := &dscv2.DataScienceCluster{}
				g.Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: "dsc-roundtrip", Namespace: ns}, dscV2)
				}, "10s", "1s").Should(Succeed(), "should fetch v1 DSC as v2")

				// Verify conversion to v2
				g.Expect(dscV2.Spec.Components.AIPipelines.ManagementState).To(Equal(operatorv1.Managed))

				// Fetch back as v1
				dscV1RoundTrip := &dscv1.DataScienceCluster{}
				g.Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: "dsc-roundtrip", Namespace: ns}, dscV1RoundTrip)
				}, "10s", "1s").Should(Succeed(), "should fetch v2 DSC back as v1")

				// Verify round-trip preserves data
				g.Expect(dscV1RoundTrip.Spec.Components.DataSciencePipelines.ManagementState).To(Equal(operatorv1.Managed),
					"DataSciencePipelines ManagementState should be preserved after round-trip conversion")
			},
		},
		{
			name: "Condition type conversion: DataSciencePipelinesReady <-> AIPipelinesReady",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				// Create a v2 DSC with AIPipelinesReady condition
				dscV2 := envtestutil.NewDSC("dsc-condition-convert")
				dscV2.Spec.Components.AIPipelines.ManagementState = operatorv1.Managed
				dscV2.Status.Conditions = []common.Condition{
					{
						Type:   "AIPipelinesReady",
						Status: metav1.ConditionTrue,
						Reason: "Ready",
					},
				}
				g.Expect(k8sClient.Create(ctx, dscV2)).To(Succeed(), "should create v2 DSC with AIPipelinesReady condition")

				// Update status with the condition
				dscV2.Status.Conditions = []common.Condition{
					{
						Type:   "AIPipelinesReady",
						Status: metav1.ConditionTrue,
						Reason: "Ready",
					},
				}
				g.Expect(k8sClient.Status().Update(ctx, dscV2)).To(Succeed())

				// Fetch as v1 and verify condition type is converted
				dscV1 := &dscv1.DataScienceCluster{}
				g.Eventually(func() bool {
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: "dsc-condition-convert", Namespace: ns}, dscV1); err != nil {
						return false
					}
					// Check if the condition type was converted to DataSciencePipelinesReady
					for _, cond := range dscV1.Status.Conditions {
						if cond.Type == "DataSciencePipelinesReady" && cond.Status == metav1.ConditionTrue {
							return true
						}
					}
					return false
				}, "10s", "1s").Should(BeTrue(), "AIPipelinesReady condition should be converted to DataSciencePipelinesReady when fetched as v1")

				// Verify no AIPipelinesReady condition exists in v1 view
				for _, cond := range dscV1.Status.Conditions {
					g.Expect(cond.Type).NotTo(Equal("AIPipelinesReady"), "v1 should not have AIPipelinesReady condition")
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			t.Logf("Starting conversion test case: %s", tc.name)
			ctx, env, teardown := envtestutil.SetupEnvAndClient(
				t,
				[]envt.RegisterWebhooksFn{
					v1webhook.RegisterWebhooks,
					dsciv1webhook.RegisterWebhooks,
					v2webhook.RegisterWebhooks,
					dsciv2webhook.RegisterWebhooks,
				},
				envtestutil.DefaultWebhookTimeout,
			)
			t.Cleanup(teardown)

			ns := xid.New().String()
			t.Logf("Using namespace: %s", ns)
			createDSCIV1(NewWithT(t), ctx, env.Client())

			g := NewWithT(t)
			tc.test(g, ctx, env.Client(), ns)
			t.Logf("Finished conversion test case: %s", tc.name)
		})
	}
}
