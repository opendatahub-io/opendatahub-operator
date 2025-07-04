package hardwareprofile_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	hwpv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/hardwareprofile"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// Helper functions for test verification.
func expectResourceRequirements(g Gomega, notebook client.Object, expectedCPU, expectedMemory string) {
	notebookUnstructured, ok := notebook.(*unstructured.Unstructured)
	g.Expect(ok).To(BeTrue(), "notebook should be an unstructured object")

	containers, found, err := unstructured.NestedSlice(notebookUnstructured.Object, "spec", "template", "spec", "containers")
	g.Expect(err).ToNot(HaveOccurred(), "should get containers from notebook")
	g.Expect(found).To(BeTrue(), "containers should be found")
	g.Expect(containers).To(HaveLen(1), "should have exactly one container")

	container, ok := containers[0].(map[string]interface{})
	g.Expect(ok).To(BeTrue(), "container should be a map")

	requests, found, err := unstructured.NestedStringMap(container, "resources", "requests")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeTrue())

	// Check CPU if expected
	if expectedCPU != "" {
		g.Expect(requests).To(HaveKeyWithValue("cpu", expectedCPU))
	}

	// Check memory if expected
	if expectedMemory != "" {
		g.Expect(requests).To(HaveKeyWithValue("memory", expectedMemory))
	}
}

func expectNodeSelector(g Gomega, notebook client.Object, expectedSelectors map[string]string) {
	notebookUnstructured, ok := notebook.(*unstructured.Unstructured)
	g.Expect(ok).To(BeTrue(), "notebook should be an unstructured object")

	nodeSelector, found, err := unstructured.NestedStringMap(notebookUnstructured.Object, "spec", "template", "spec", "nodeSelector")

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeTrue())

	for key, value := range expectedSelectors {
		g.Expect(nodeSelector).To(HaveKeyWithValue(key, value))
	}
}

func expectTolerations(g Gomega, notebook client.Object, expectedTolerations []map[string]string) {
	notebookUnstructured, ok := notebook.(*unstructured.Unstructured)
	g.Expect(ok).To(BeTrue(), "notebook should be an unstructured object")

	tolerations, found, err := unstructured.NestedSlice(notebookUnstructured.Object, "spec", "template", "spec", "tolerations")
	g.Expect(err).ToNot(HaveOccurred(), "should get tolerations from notebook")
	g.Expect(found).To(BeTrue(), "tolerations should be found")
	g.Expect(tolerations).To(HaveLen(len(expectedTolerations)), "should have expected number of tolerations")

	for i, expectedToleration := range expectedTolerations {
		toleration, ok := tolerations[i].(map[string]interface{})
		g.Expect(ok).To(BeTrue(), fmt.Sprintf("toleration %d should be a map", i))

		for key, value := range expectedToleration {
			g.Expect(toleration).To(HaveKeyWithValue(key, value))
		}
	}
}

// registerWebhooksWithManualDecoder registers webhooks using the generalized envtestutil function.
// This is needed because envtest doesn't automatically handle decoder injection like a real cluster does.
func registerWebhooksWithManualDecoder(mgr manager.Manager) error {
	// Use WithHandlers to pass multiple handlers - the function will automatically
	// detect which handlers need decoder injection
	return envtestutil.RegisterWebhooksWithManualDecoder(mgr,
		envtestutil.WithHandlers(
			&kueue.Validator{
				Client: mgr.GetAPIReader(),
				Name:   "kueue-validating",
			},
			&hardwareprofile.Injector{
				Client: mgr.GetAPIReader(),
				Name:   "hardwareprofile-injector",
			},
		),
	)
}

// createResourceHardwareProfile creates a hardware profile with resource identifiers for testing.
func createResourceHardwareProfile(name, namespace string) *hwpv1alpha1.HardwareProfile {
	maxCount8 := intstr.FromString("8")
	maxCount16Gi := intstr.FromString("16Gi")
	maxCount4 := intstr.FromString("4")

	return envtestutil.NewHWP(name, namespace, func(hwp *hwpv1alpha1.HardwareProfile) {
		hwp.Spec.Identifiers = []hwpv1alpha1.HardwareIdentifier{
			{
				DisplayName:  "CPU",
				Identifier:   "cpu",
				MinCount:     intstr.FromString("2"),
				MaxCount:     &maxCount8,
				DefaultCount: intstr.FromString("4"),
			},
			{
				DisplayName:  "Memory",
				Identifier:   "memory",
				MinCount:     intstr.FromString("4Gi"),
				MaxCount:     &maxCount16Gi,
				DefaultCount: intstr.FromString("8Gi"),
			},
			{
				DisplayName:  "GPU",
				Identifier:   "nvidia.com/gpu",
				MinCount:     intstr.FromString("1"),
				MaxCount:     &maxCount4,
				DefaultCount: intstr.FromString("1"),
			},
		}
	})
}

// createKueueHardwareProfile creates a hardware profile with Kueue configuration for testing.
func createKueueHardwareProfile(name, namespace, queueName string) *hwpv1alpha1.HardwareProfile {
	return envtestutil.NewHWP(name, namespace, func(hwp *hwpv1alpha1.HardwareProfile) {
		hwp.Spec.SchedulingSpec = &hwpv1alpha1.SchedulingSpec{
			SchedulingType: hwpv1alpha1.QueueScheduling,
			Kueue: &hwpv1alpha1.KueueSchedulingSpec{
				LocalQueueName: queueName,
			},
		}
	})
}

// createNodeSchedulingHardwareProfile creates a hardware profile with node scheduling configuration.
func createNodeSchedulingHardwareProfile(name, namespace string) *hwpv1alpha1.HardwareProfile {
	return envtestutil.NewHWP(name, namespace, func(hwp *hwpv1alpha1.HardwareProfile) {
		hwp.Spec.SchedulingSpec = &hwpv1alpha1.SchedulingSpec{
			SchedulingType: hwpv1alpha1.NodeScheduling,
			Node: &hwpv1alpha1.NodeSchedulingSpec{
				NodeSelector: map[string]string{
					"accelerator": "nvidia-tesla-v100",
					"zone":        "us-west-1a",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "nvidia.com/gpu",
						Operator: corev1.TolerationOpEqual,
						Value:    "present",
						Effect:   corev1.TaintEffectNoSchedule,
					},
					{
						Key:      "high-memory",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
		}
	})
}

// createSimpleHardwareProfile creates a basic hardware profile with minimal configuration.
func createSimpleHardwareProfile(name, namespace string) *hwpv1alpha1.HardwareProfile {
	return envtestutil.NewHWP(name, namespace, func(hwp *hwpv1alpha1.HardwareProfile) {
		hwp.Spec.Identifiers = []hwpv1alpha1.HardwareIdentifier{
			{
				DisplayName:  "CPU",
				Identifier:   "cpu",
				DefaultCount: intstr.FromString("2"),
			},
		}
	})
}

// TestHardwareProfileWebhook_Integration exercises the mutating webhook logic for hardware profile injection.
func TestHardwareProfileWebhook_Integration(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		setup func(ns string) []client.Object
		test  func(g Gomega, ctx context.Context, k8sClient client.Client, ns string)
	}{
		{
			name: "No hardware profile annotation: should allow without modification",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				notebook := envtestutil.NewNotebook(testNotebook, ns)
				g.Expect(k8sClient.Create(ctx, notebook)).To(Succeed(), "should allow creation without hardware profile annotation")

				// Verify no hardware profile namespace annotation was added
				annotations := notebook.GetAnnotations()
				g.Expect(annotations).ToNot(HaveKey(hardwareprofile.HardwareProfileNamespaceAnnotation))
			},
		},
		{
			name: "Empty hardware profile annotation: should allow without modification",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				notebook := envtestutil.NewNotebook(testNotebook, ns,
					envtestutil.WithAnnotation(hardwareprofile.HardwareProfileNameAnnotation, ""))
				g.Expect(k8sClient.Create(ctx, notebook)).To(Succeed(), "should allow creation with empty hardware profile annotation")

				// Verify no hardware profile namespace annotation was added
				annotations := notebook.GetAnnotations()
				g.Expect(annotations).ToNot(HaveKey(hardwareprofile.HardwareProfileNamespaceAnnotation))
			},
		},
		{
			name: "Non-existent hardware profile: should deny with error",
			setup: func(ns string) []client.Object {
				return nil
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				notebook := envtestutil.NewNotebook(testNotebook, ns,
					envtestutil.WithHardwareProfile("non-existent-profile"))

				err := k8sClient.Create(ctx, notebook)
				g.Expect(err).To(HaveOccurred(), "should deny creation with non-existent hardware profile")
				g.Expect(err.Error()).To(ContainSubstring("failed to get hardware profile"), "error should mention hardware profile not found")
				g.Expect(err.Error()).To(ContainSubstring("non-existent-profile"), "error should mention the profile name")
			},
		},
		{
			name: "Valid hardware profile with resource identifiers: should inject resources",
			setup: func(ns string) []client.Object {
				return []client.Object{createResourceHardwareProfile(testHardwareProfile, ns)}
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				notebook := envtestutil.NewNotebook(testNotebook, ns,
					envtestutil.WithHardwareProfile(testHardwareProfile))

				g.Expect(k8sClient.Create(ctx, notebook)).To(Succeed(), "should allow creation with valid hardware profile")

				// Re-fetch the notebook from the server to see webhook modifications
				createdNotebook := &unstructured.Unstructured{}
				createdNotebook.SetGroupVersionKind(gvk.Notebook)
				key := types.NamespacedName{Name: testNotebook, Namespace: ns}
				g.Expect(k8sClient.Get(ctx, key, createdNotebook)).To(Succeed(), "should get created notebook")

				// Verify hardware profile namespace annotation was added
				annotations := createdNotebook.GetAnnotations()
				g.Expect(annotations).To(HaveKeyWithValue(hardwareprofile.HardwareProfileNamespaceAnnotation, ns))

				// Verify resource requirements were injected
				expectResourceRequirements(g, createdNotebook, "4", "8Gi")
			},
		},
		{
			name: "Hardware profile with Kueue configuration: should inject queue label",
			setup: func(ns string) []client.Object {
				return []client.Object{createKueueHardwareProfile("test-profile-kueue", ns, testQueue)}
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				notebook := envtestutil.NewNotebook(testNotebook, ns,
					envtestutil.WithHardwareProfile("test-profile-kueue"))

				g.Expect(k8sClient.Create(ctx, notebook)).To(Succeed(), "should allow creation with Kueue hardware profile")

				// Re-fetch the notebook from the server to see webhook modifications
				createdNotebook := &unstructured.Unstructured{}
				createdNotebook.SetGroupVersionKind(gvk.Notebook)
				key := types.NamespacedName{Name: testNotebook, Namespace: ns}
				g.Expect(k8sClient.Get(ctx, key, createdNotebook)).To(Succeed(), "should get created notebook")

				// Verify Kueue label was added
				labels := createdNotebook.GetLabels()
				g.Expect(labels).To(HaveKeyWithValue("kueue.x-k8s.io/queue-name", testQueue))
			},
		},
		{
			name: "Hardware profile with node scheduling: should inject nodeSelector and tolerations",
			setup: func(ns string) []client.Object {
				return []client.Object{createNodeSchedulingHardwareProfile("test-profile-node", ns)}
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				notebook := envtestutil.NewNotebook(testNotebook, ns,
					envtestutil.WithHardwareProfile("test-profile-node"))

				g.Expect(k8sClient.Create(ctx, notebook)).To(Succeed(), "should allow creation with node scheduling hardware profile")

				// Re-fetch the notebook from the server to see webhook modifications
				createdNotebook := &unstructured.Unstructured{}
				createdNotebook.SetGroupVersionKind(gvk.Notebook)
				key := types.NamespacedName{Name: testNotebook, Namespace: ns}
				g.Expect(k8sClient.Get(ctx, key, createdNotebook)).To(Succeed(), "should get created notebook")

				// Verify nodeSelector was injected
				expectNodeSelector(g, createdNotebook, map[string]string{
					"accelerator": "nvidia-tesla-v100",
					"zone":        "us-west-1a",
				})

				// Verify tolerations were injected
				expectTolerations(g, createdNotebook, []map[string]string{
					{
						"key":      "nvidia.com/gpu",
						"operator": "Equal",
						"value":    "present",
						"effect":   "NoSchedule",
					},
					{
						"key":      "high-memory",
						"operator": "Exists",
						"effect":   "NoSchedule",
					},
				})
			},
		},
		{
			name: "Cross-namespace hardware profile: should fetch from specified namespace",
			setup: func(ns string) []client.Object {
				otherNs := ns + "-other"
				hwp := createSimpleHardwareProfile("cross-ns-profile", otherNs)

				// Create the other namespace
				otherNamespace := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: otherNs},
				}

				return []client.Object{otherNamespace, hwp}
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				otherNs := ns + "-other"
				notebook := envtestutil.NewNotebook(testNotebook, ns,
					envtestutil.WithHardwareProfile("cross-ns-profile"),
					envtestutil.WithHardwareProfileNamespace(otherNs))

				g.Expect(k8sClient.Create(ctx, notebook)).To(Succeed(), "should allow creation with cross-namespace hardware profile")

				// Re-fetch the notebook from the server to see webhook modifications
				createdNotebook := &unstructured.Unstructured{}
				createdNotebook.SetGroupVersionKind(gvk.Notebook)
				key := types.NamespacedName{Name: testNotebook, Namespace: ns}
				g.Expect(k8sClient.Get(ctx, key, createdNotebook)).To(Succeed(), "should get created notebook")

				// Verify hardware profile namespace annotation was set to the cross-namespace value
				annotations := createdNotebook.GetAnnotations()
				g.Expect(annotations).To(HaveKeyWithValue(hardwareprofile.HardwareProfileNamespaceAnnotation, otherNs))

				// Verify resource requirements were injected from cross-namespace profile
				expectResourceRequirements(g, createdNotebook, "2", "")
			},
		},
		{
			name: "Update operation: should inject hardware profile on updates",
			setup: func(ns string) []client.Object {
				hwp := createSimpleHardwareProfile("update-profile", ns)

				// Create an existing notebook without hardware profile
				notebook := envtestutil.NewNotebook("existing-notebook", ns)

				return []client.Object{hwp, notebook}
			},
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				// Get the existing notebook and update it
				existing := &unstructured.Unstructured{}
				existing.SetGroupVersionKind(gvk.Notebook)
				key := types.NamespacedName{Name: "existing-notebook", Namespace: ns}
				g.Expect(k8sClient.Get(ctx, key, existing)).To(Succeed(), "should get existing notebook")

				// Add hardware profile annotation and update
				annotations := existing.GetAnnotations()
				if annotations == nil {
					annotations = make(map[string]string)
				}
				annotations[hardwareprofile.HardwareProfileNameAnnotation] = "update-profile"
				existing.SetAnnotations(annotations)

				g.Expect(k8sClient.Update(ctx, existing)).To(Succeed(), "should allow update with hardware profile annotation")

				// Verify hardware profile was injected during update
				updatedNotebook := &unstructured.Unstructured{}
				updatedNotebook.SetGroupVersionKind(gvk.Notebook)
				g.Expect(k8sClient.Get(ctx, key, updatedNotebook)).To(Succeed(), "should get updated notebook")

				updatedAnnotations := updatedNotebook.GetAnnotations()
				g.Expect(updatedAnnotations).To(HaveKeyWithValue(hardwareprofile.HardwareProfileNamespaceAnnotation, ns))

				// Verify resource requirements were injected
				expectResourceRequirements(g, updatedNotebook, "2", "")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			t.Logf("Starting test case: %s", tc.name)

			ctx, env, teardown := envtestutil.SetupEnvAndClientWithNotebook(
				t,
				[]envt.RegisterWebhooksFn{
					registerWebhooksWithManualDecoder,
				},
				20*time.Second,
			)
			t.Cleanup(teardown)

			ns := xid.New().String()
			t.Logf("Using namespace: %s", ns)

			// Create the test namespace (needed for namespaced resources)
			testNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: ns},
			}
			g := NewWithT(t)
			g.Expect(env.Client().Create(ctx, testNamespace)).To(Succeed(), "test namespace creation should succeed")

			// Setup test objects
			if tc.setup != nil {
				for _, obj := range tc.setup(ns) {
					t.Logf("Creating setup object: %+v", obj)
					g.Expect(env.Client().Create(ctx, obj)).To(Succeed(), "setup object creation should succeed")
				}
			}
			tc.test(g, ctx, env.Client(), ns)
			t.Logf("Finished test case: %s", tc.name)
		})
	}
}

// TestHardwareProfile_CRDValidation exercises the validating logic for HardwareProfile resources.
// It verifies the union discriminating CEL rules for queue vs node based scheduling when creating HardwareProfiles.
func TestHardwareProfile_CRDValidation(t *testing.T) {
	testCases := []struct {
		name string
		test func(g Gomega, ctx context.Context, k8sClient client.Client, ns string)
	}{
		{
			name: "Allows creation of queue-based HardwareProfile when NodeSchedulingSpec is nil",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				hwp := createKueueHardwareProfile("valid-queue-based-hwp", ns, testQueue)
				g.Expect(k8sClient.Create(ctx, hwp)).To(Succeed(), "should allow creation of a queue-based HardwareProfile when NodeSchedulingSpec is nil")
			},
		},
		{
			name: "Denies creation of queue-based HardwareProfile when NodeSchedulingSpec is not nil",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				// Create a queue-based profile and then add node scheduling config to make it invalid
				hwp := createKueueHardwareProfile("invalid-queue-based-hwp", ns, testQueue)
				// Add node scheduling config to make it invalid (both queue and node scheduling)
				hwp.Spec.SchedulingSpec.Node = &hwpv1alpha1.NodeSchedulingSpec{
					NodeSelector: map[string]string{
						"test": "node",
					},
				}
				err := k8sClient.Create(ctx, hwp)
				g.Expect(err).NotTo(Succeed(), "should not allow creation of a queue-based HardwareProfile when NodeSchedulingSpec is not nil")
			},
		},
		{
			name: "Allows creation of node-based HardwareProfile when KueueSchedulingSpec is nil",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				hwp := createNodeSchedulingHardwareProfile("valid-node-based-hwp", ns)
				g.Expect(k8sClient.Create(ctx, hwp)).To(Succeed(), "should allow creation of a node-based HardwareProfile when KueueSchedulingSpec is nil")
			},
		},
		{
			name: "Denies creation of node-based HardwareProfile when KueueSchedulingSpec is not nil",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				// Create a node-based profile and then add queue scheduling config to make it invalid
				hwp := createNodeSchedulingHardwareProfile("invalid-node-based-hwp", ns)
				// Add queue scheduling config to make it invalid (both queue and node scheduling)
				hwp.Spec.SchedulingSpec.Kueue = &hwpv1alpha1.KueueSchedulingSpec{
					LocalQueueName: testQueue,
				}
				err := k8sClient.Create(ctx, hwp)
				g.Expect(err).NotTo(Succeed(), "should not allow creation of a node-based HardwareProfile when KueueSchedulingSpec is not nil")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("Starting test case: %s", tc.name)

			g := NewWithT(t)
			gctx, cancel := context.WithCancel(context.Background())

			s, err := scheme.New()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(hwpv1alpha1.AddToScheme(s)).To(Succeed())

			env, err := envt.New(envt.WithManager(), envt.WithScheme(s))
			g.Expect(err).ShouldNot(HaveOccurred())

			t.Cleanup(func() {
				cancel()

				err := env.Stop()
				g.Expect(err).NotTo(HaveOccurred())
			})

			ns := corev1.Namespace{}
			ns.Name = xid.New().String()
			err = env.Client().Create(gctx, &ns)
			g.Expect(err).ShouldNot(HaveOccurred())
			t.Logf("Using namespace: %s", ns.Name)

			tc.test(g, gctx, env.Client(), ns.Name)
			t.Logf("Finished test case: %s", tc.name)
		})
	}
}
