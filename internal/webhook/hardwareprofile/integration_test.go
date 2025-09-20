package hardwareprofile_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hwpv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	hardwareprofilewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/hardwareprofile"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

// PRIVATE HELPER FUNCTIONS

// WorkloadType is type of workload being tested.
type WorkloadType string

const (
	WorkloadTypeNotebook         WorkloadType = "Notebook"
	WorkloadTypeInferenceService WorkloadType = "InferenceService"
)

// expectResourceRequirementsAtPath is a generic helper that verifies resource requirements at a specific path.
func expectResourceRequirementsAtPath(
	g Gomega,
	scheme *runtime.Scheme,
	workload client.Object,
	expectedCPU, expectedMemory string,
	containersPath []string,
	workloadType WorkloadType) {
	workloadUnstructured, err := resources.ObjectToUnstructured(scheme, workload)
	g.Expect(err).ShouldNot(HaveOccurred(), "should convert workload to unstructured")

	// Use workload type instead of hardcoded path check
	if workloadType == WorkloadTypeInferenceService {
		// For InferenceService, work with the model object directly
		model, found, err := unstructured.NestedMap(workloadUnstructured.Object, containersPath...)
		g.Expect(err).ShouldNot(HaveOccurred(), "should get model from workload")
		g.Expect(found).Should(BeTrue(), "model should be found")

		requests, found, err := unstructured.NestedStringMap(model, "resources", "requests")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())

		// Check CPU if expected
		if expectedCPU != "" {
			g.Expect(requests).Should(HaveKeyWithValue("cpu", expectedCPU))
		}

		// Check memory if expected
		if expectedMemory != "" {
			g.Expect(requests).Should(HaveKeyWithValue("memory", expectedMemory))
		}
	} else {
		// For Notebook and other workloads, work with containers
		containers, found, err := unstructured.NestedSlice(workloadUnstructured.Object, containersPath...)
		g.Expect(err).ShouldNot(HaveOccurred(), "should get containers from workload")
		g.Expect(found).Should(BeTrue(), "containers should be found")
		g.Expect(containers).Should(HaveLen(1), "should have exactly one container")

		container, ok := containers[0].(map[string]interface{})
		g.Expect(ok).Should(BeTrue(), "container should be a map")

		requests, found, err := unstructured.NestedStringMap(container, "resources", "requests")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())

		// Check CPU if expected
		if expectedCPU != "" {
			g.Expect(requests).Should(HaveKeyWithValue("cpu", expectedCPU))
		}

		// Check memory if expected
		if expectedMemory != "" {
			g.Expect(requests).Should(HaveKeyWithValue("memory", expectedMemory))
		}
	}
}

// expectNodeSelectorAtPath verifies that a workload has the expected node selector at a specific path.
func expectNodeSelectorAtPath(g Gomega, scheme *runtime.Scheme, workload client.Object, expectedSelectors map[string]string, nodeSelectorPath []string) {
	workloadUnstructured, err := resources.ObjectToUnstructured(scheme, workload)
	g.Expect(err).ShouldNot(HaveOccurred(), "should convert workload to unstructured")

	nodeSelector, found, err := unstructured.NestedStringMap(workloadUnstructured.Object, nodeSelectorPath...)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).Should(BeTrue())

	for key, value := range expectedSelectors {
		g.Expect(nodeSelector).Should(HaveKeyWithValue(key, value))
	}
}

// expectTolerationsAtPath verifies that a workload has the expected tolerations at a specific path.
func expectTolerationsAtPath(g Gomega, scheme *runtime.Scheme, workload client.Object, expectedTolerations []map[string]string, tolerationsPath []string) {
	workloadUnstructured, err := resources.ObjectToUnstructured(scheme, workload)
	g.Expect(err).ShouldNot(HaveOccurred(), "should convert workload to unstructured")

	tolerations, found, err := unstructured.NestedSlice(workloadUnstructured.Object, tolerationsPath...)
	g.Expect(err).ShouldNot(HaveOccurred(), "should get tolerations from workload")
	g.Expect(found).Should(BeTrue(), "tolerations should be found")
	g.Expect(tolerations).Should(HaveLen(len(expectedTolerations)), "should have expected number of tolerations")

	for i, expectedToleration := range expectedTolerations {
		toleration, ok := tolerations[i].(map[string]interface{})
		g.Expect(ok).Should(BeTrue(), fmt.Sprintf("toleration %d should be a map", i))

		for key, value := range expectedToleration {
			g.Expect(toleration).Should(HaveKeyWithValue(key, value))
		}
	}
}

// createResourceHardwareProfile creates a hardware profile with resource identifiers for testing.
func createResourceHardwareProfile(name, namespace string) *hwpv1alpha1.HardwareProfile {
	return envtestutil.NewHardwareProfile(name, namespace,
		envtestutil.WithCPUIdentifier("2", "4", "8"),
		envtestutil.WithMemoryIdentifier("4Gi", "8Gi", "16Gi"),
		envtestutil.WithGPUIdentifier("nvidia.com/gpu", "1", "1", "4"),
	)
}

// createKueueHardwareProfile creates a hardware profile with Kueue configuration for testing.
func createKueueHardwareProfile(name, namespace, queueName string) *hwpv1alpha1.HardwareProfile {
	return envtestutil.NewHardwareProfile(name, namespace,
		envtestutil.WithKueueScheduling(queueName),
	)
}

// createNodeSchedulingHardwareProfile creates a hardware profile with node scheduling configuration.
func createNodeSchedulingHardwareProfile(name, namespace string) *hwpv1alpha1.HardwareProfile {
	return envtestutil.NewHardwareProfile(name, namespace,
		envtestutil.WithNodeScheduling(
			map[string]string{
				"accelerator": "nvidia-tesla-v100",
				"zone":        "us-west-1a",
			},
			[]corev1.Toleration{
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
		),
	)
}

// createSimpleHardwareProfile creates a basic hardware profile with minimal configuration.
func createSimpleHardwareProfile(name, namespace string) *hwpv1alpha1.HardwareProfile {
	return envtestutil.NewHardwareProfile(name, namespace,
		envtestutil.WithCPUIdentifier("0", "2"),
	)
}

// testNoHardwareProfileAnnotationForWorkload is a generic helper that tests webhook behavior
// when no hardware profile annotation is present for any workload type.
func testNoHardwareProfileAnnotationForWorkload(g Gomega, ctx context.Context, k8sClient client.Client,
	createWorkload func() client.Object, containersPath []string, workloadType WorkloadType) {
	workload := createWorkload()
	g.Expect(k8sClient.Create(ctx, workload)).Should(Succeed())

	// Verify no changes were made since no annotation was present
	g.Expect(workload.GetAnnotations()).Should(BeEmpty())

	// Additionally verify no resources were injected (more thorough check)
	workloadUnstructured, err := resources.ObjectToUnstructured(k8sClient.Scheme(), workload)
	g.Expect(err).ShouldNot(HaveOccurred(), "should convert workload to unstructured")

	// Use workload type instead of hardcoded path check
	if workloadType == WorkloadTypeInferenceService {
		// For InferenceService, work with the model object directly
		model, found, err := unstructured.NestedMap(workloadUnstructured.Object, containersPath...)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())

		// Should not have resources injected
		_, found, err = unstructured.NestedStringMap(model, "resources", "requests")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeFalse())
	} else {
		// For Notebook and other workloads, work with containers
		containers, found, err := unstructured.NestedSlice(workloadUnstructured.Object, containersPath...)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())
		g.Expect(containers).Should(HaveLen(1))

		container, ok := containers[0].(map[string]interface{})
		g.Expect(ok).Should(BeTrue())

		// Should not have resources injected
		_, found, err = unstructured.NestedStringMap(container, "resources", "requests")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeFalse())
	}
}

// testValidHardwareProfileWithResourcesForWorkload is a generic helper that tests webhook behavior
// with a valid hardware profile containing resource identifiers for any workload type.
func testValidHardwareProfileWithResourcesForWorkload(g Gomega, ctx context.Context, k8sClient client.Client, ns string,
	createWorkload func() client.Object, containersPath []string, workloadType WorkloadType) {
	// Create hardware profile with resource identifiers
	hwp := createResourceHardwareProfile("resource-profile", ns)
	g.Expect(k8sClient.Create(ctx, hwp)).Should(Succeed())

	// Create workload with hardware profile annotation
	workload := createWorkload()
	g.Expect(k8sClient.Create(ctx, workload)).Should(Succeed())

	// Verify resource requirements were applied
	expectResourceRequirementsAtPath(g, k8sClient.Scheme(), workload, "4", "8Gi", containersPath, workloadType)
}

// testHardwareProfileWithKueueForWorkload is a generic helper that tests webhook behavior
// with a hardware profile containing Kueue configuration for any workload type.
func testHardwareProfileWithKueueForWorkload(g Gomega, ctx context.Context, k8sClient client.Client, ns string,
	createWorkload func() client.Object, queueName string, expectedLabelKey string) {
	// Create hardware profile with Kueue configuration
	hwp := createKueueHardwareProfile("kueue-profile", ns, queueName)
	g.Expect(k8sClient.Create(ctx, hwp)).Should(Succeed())

	// Create workload with hardware profile annotation
	workload := createWorkload()
	g.Expect(k8sClient.Create(ctx, workload)).Should(Succeed())

	// Verify Kueue configuration was applied
	if expectedLabelKey != "" {
		// For both Notebook and InferenceService - uses labels
		g.Expect(resources.HasLabel(workload, expectedLabelKey, queueName)).Should(BeTrue())
	} else {
		// This branch should no longer be used since all workloads use labels
		actualQueueName := resources.GetAnnotation(workload, "kueue.x-k8s.io/queue-name")
		g.Expect(actualQueueName).Should(Equal(queueName))
	}
}

// testHardwareProfileWithNodeSchedulingForWorkload is a generic helper that tests webhook behavior
// with a hardware profile containing node scheduling configuration for any workload type.
func testHardwareProfileWithNodeSchedulingForWorkload(g Gomega, ctx context.Context, k8sClient client.Client, ns string,
	createWorkload func() client.Object, nodeSelectorPath, tolerationsPath []string) {
	// Create hardware profile with node scheduling
	hwp := createNodeSchedulingHardwareProfile("node-profile", ns)
	g.Expect(k8sClient.Create(ctx, hwp)).Should(Succeed())

	// Create workload with hardware profile annotation
	workload := createWorkload()
	g.Expect(k8sClient.Create(ctx, workload)).Should(Succeed())

	// Verify node selector was applied
	expectedSelectors := map[string]string{
		"accelerator": "nvidia-tesla-v100",
		"zone":        "us-west-1a",
	}
	expectNodeSelectorAtPath(g, k8sClient.Scheme(), workload, expectedSelectors, nodeSelectorPath)

	// Verify tolerations were applied
	expectedTolerations := []map[string]string{
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
	}
	expectTolerationsAtPath(g, k8sClient.Scheme(), workload, expectedTolerations, tolerationsPath)
}

// testEmptyHardwareProfileAnnotation tests webhook behavior when hardware profile annotation is empty.
func testEmptyHardwareProfileAnnotation(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
	notebook := envtestutil.NewNotebook("test-notebook-empty-annotation", ns,
		envtestutil.WithAnnotation("opendatahub.io/hardware-profile-name", ""),
	)

	g.Expect(k8sClient.Create(ctx, notebook)).Should(Succeed())

	// Verify no changes were made since annotation was empty
	g.Expect(resources.GetAnnotation(notebook, "opendatahub.io/hardware-profile-name")).Should(Equal(""))
}

// testNonExistentHardwareProfile tests webhook behavior when referencing a non-existent hardware profile.
func testNonExistentHardwareProfile(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
	notebook := envtestutil.NewNotebook("test-notebook-nonexistent", ns,
		envtestutil.WithHardwareProfile("nonexistent-profile"),
	)

	// This should fail because the hardware profile doesn't exist
	err := k8sClient.Create(ctx, notebook)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("hardware profile 'nonexistent-profile' not found"))
}

// testCrossNamespaceHardwareProfile tests webhook behavior when hardware profile is in a different namespace.
func testCrossNamespaceHardwareProfile(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
	// Create a second namespace for the hardware profile
	hwpNs := fmt.Sprintf("%s-hwp", ns)
	hwpNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: hwpNs},
	}
	g.Expect(k8sClient.Create(ctx, hwpNamespace)).To(Succeed())

	// Create hardware profile in the different namespace
	hwp := createSimpleHardwareProfile("cross-ns-profile", hwpNs)
	g.Expect(k8sClient.Create(ctx, hwp)).To(Succeed())

	// Create notebook with cross-namespace hardware profile annotation
	notebook := envtestutil.NewNotebook("test-notebook-cross-ns", ns,
		envtestutil.WithHardwareProfile("cross-ns-profile"),
		envtestutil.WithHardwareProfileNamespace(hwpNs),
	)

	g.Expect(k8sClient.Create(ctx, notebook)).To(Succeed())

	// Verify the hardware profile namespace annotation was set correctly
	g.Expect(resources.GetAnnotation(notebook, "opendatahub.io/hardware-profile-namespace")).Should(Equal(hwpNs))
}

// testUpdateOperationForWorkload is a generic helper for testing update operations.
func testUpdateOperationForWorkload(g Gomega, ctx context.Context, k8sClient client.Client, ns string,
	name string, createWorkload func() client.Object, createUnstructured func() *unstructured.Unstructured,
	containersPath []string, workloadType WorkloadType) {
	// Create hardware profile
	hwp := createSimpleHardwareProfile("update-profile", ns)
	g.Expect(k8sClient.Create(ctx, hwp)).To(Succeed())

	// Create workload without hardware profile annotation
	workload := createWorkload()
	g.Expect(k8sClient.Create(ctx, workload)).To(Succeed())

	// Update workload to add hardware profile annotation
	workloadCopy, ok := workload.DeepCopyObject().(client.Object)
	g.Expect(ok).To(BeTrue(), "workload copy should be client.Object")
	workloadCopy.SetAnnotations(map[string]string{
		"opendatahub.io/hardware-profile-name": "update-profile",
	})

	g.Expect(k8sClient.Update(ctx, workloadCopy)).To(Succeed())

	// Fetch the updated workload
	updatedWorkload := createUnstructured()
	g.Expect(k8sClient.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: ns,
	}, updatedWorkload)).To(Succeed())

	// Verify resource requirements were applied during update
	expectResourceRequirementsAtPath(g, k8sClient.Scheme(), updatedWorkload, "2", "", containersPath, workloadType)
}

// TEST FUNCTIONS

// TestHardwareProfileWebhook_Notebook for mutating webhook logic for hardware profile injection on Notebook workloads.
func TestHardwareProfileWebhook_Notebook(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		test func(g Gomega, ctx context.Context, k8sClient client.Client, ns string)
	}{
		{
			name: "notebook - no hardware profile annotation",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				config, err := hardwareprofilewebhook.GetWorkloadConfig("Notebook")
				g.Expect(err).ShouldNot(HaveOccurred())
				testNoHardwareProfileAnnotationForWorkload(g, ctx, k8sClient,
					func() client.Object { return envtestutil.NewNotebook("test-notebook-no-annotation", ns) },
					config.ContainersPath, WorkloadTypeNotebook)
			},
		},
		{
			name: "notebook - empty hardware profile annotation",
			test: testEmptyHardwareProfileAnnotation,
		},
		{
			name: "notebook - non-existent hardware profile",
			test: testNonExistentHardwareProfile,
		},
		{
			name: "notebook - valid hardware profile with resources",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				config, err := hardwareprofilewebhook.GetWorkloadConfig("Notebook")
				g.Expect(err).ShouldNot(HaveOccurred())
				testValidHardwareProfileWithResourcesForWorkload(g, ctx, k8sClient, ns,
					func() client.Object {
						return envtestutil.NewNotebook("test-notebook-resources", ns,
							envtestutil.WithHardwareProfile("resource-profile"))
					},
					config.ContainersPath, WorkloadTypeNotebook)
			},
		},
		{
			name: "notebook - hardware profile with Kueue",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				testHardwareProfileWithKueueForWorkload(g, ctx, k8sClient, ns,
					func() client.Object {
						return envtestutil.NewNotebook("test-notebook-kueue", ns,
							envtestutil.WithHardwareProfile("kueue-profile"))
					},
					"gpu-queue", "kueue.x-k8s.io/queue-name")
			},
		},
		{
			name: "notebook - hardware profile with node scheduling",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				config, err := hardwareprofilewebhook.GetWorkloadConfig("Notebook")
				g.Expect(err).ShouldNot(HaveOccurred())
				testHardwareProfileWithNodeSchedulingForWorkload(g, ctx, k8sClient, ns,
					func() client.Object {
						return envtestutil.NewNotebook("test-notebook-node", ns,
							envtestutil.WithHardwareProfile("node-profile"))
					},
					config.NodeSelectorPath, config.TolerationsPath)
			},
		},
		{
			name: "notebook - cross-namespace hardware profile",
			test: testCrossNamespaceHardwareProfile,
		},
		{
			name: "notebook - update operation",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				config, err := hardwareprofilewebhook.GetWorkloadConfig("Notebook")
				g.Expect(err).ShouldNot(HaveOccurred())
				testUpdateOperationForWorkload(g, ctx, k8sClient, ns, "test-notebook-update",
					func() client.Object { return envtestutil.NewNotebook("test-notebook-update", ns) },
					func() *unstructured.Unstructured {
						u := &unstructured.Unstructured{}
						u.SetAPIVersion("kubeflow.org/v1")
						u.SetKind("Notebook")
						return u
					},
					config.ContainersPath, WorkloadTypeNotebook)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			ctx, env, teardown := envtestutil.SetupEnvAndClientWithCRDs(
				t,
				[]envt.RegisterWebhooksFn{envtestutil.RegisterWebhooks},
				envtestutil.DefaultWebhookTimeout,
				envtestutil.WithNotebook(),
			)
			defer teardown()

			// Create test namespace
			ns := fmt.Sprintf("test-ns-%s", xid.New().String())
			testNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: ns},
			}
			g.Expect(env.Client().Create(ctx, testNamespace)).To(Succeed())

			// Add HardwareProfile types to scheme for client operations
			g.Expect(hwpv1alpha1.AddToScheme(env.Scheme())).To(Succeed())

			// Run the specific test case
			tc.test(g, ctx, env.Client(), ns)
		})
	}
}

// TestHardwareProfileWebhook_InferenceService for the mutating webhook logic for hardware profile injection on InferenceService workloads.
func TestHardwareProfileWebhook_InferenceService(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		test func(g Gomega, ctx context.Context, k8sClient client.Client, ns string)
	}{
		{
			name: "inference service - no hardware profile annotation",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				config, err := hardwareprofilewebhook.GetWorkloadConfig("InferenceService")
				g.Expect(err).ShouldNot(HaveOccurred())
				testNoHardwareProfileAnnotationForWorkload(g, ctx, k8sClient,
					func() client.Object {
						return envtestutil.NewInferenceService("test-inference-service-no-annotation", ns)
					},
					config.ContainersPath, WorkloadTypeInferenceService)
			},
		},
		{
			name: "inference service - valid hardware profile with resources",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				config, err := hardwareprofilewebhook.GetWorkloadConfig("InferenceService")
				g.Expect(err).ShouldNot(HaveOccurred())
				testValidHardwareProfileWithResourcesForWorkload(g, ctx, k8sClient, ns,
					func() client.Object {
						return envtestutil.NewInferenceService("test-inference-service-resources", ns,
							envtestutil.WithHardwareProfile("resource-profile"))
					},
					config.ContainersPath, WorkloadTypeInferenceService)
			},
		},
		{
			name: "inference service - hardware profile with node scheduling",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				config, err := hardwareprofilewebhook.GetWorkloadConfig("InferenceService")
				g.Expect(err).ShouldNot(HaveOccurred())
				testHardwareProfileWithNodeSchedulingForWorkload(g, ctx, k8sClient, ns,
					func() client.Object {
						return envtestutil.NewInferenceService("test-inference-service-node", ns,
							envtestutil.WithHardwareProfile("node-profile"))
					},
					config.NodeSelectorPath, config.TolerationsPath)
			},
		},
		{
			name: "inference service - hardware profile with Kueue",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				testHardwareProfileWithKueueForWorkload(g, ctx, k8sClient, ns,
					func() client.Object {
						return envtestutil.NewInferenceService("test-inference-service-kueue", ns,
							envtestutil.WithHardwareProfile("kueue-profile"))
					},
					"test-queue", "kueue.x-k8s.io/queue-name")
			},
		},
		{
			name: "inference service - update operation",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				config, err := hardwareprofilewebhook.GetWorkloadConfig("InferenceService")
				g.Expect(err).ShouldNot(HaveOccurred())
				testUpdateOperationForWorkload(g, ctx, k8sClient, ns, "test-inference-service-update",
					func() client.Object { return envtestutil.NewInferenceService("test-inference-service-update", ns) },
					func() *unstructured.Unstructured {
						u := &unstructured.Unstructured{}
						u.SetAPIVersion("serving.kserve.io/v1beta1")
						u.SetKind("InferenceService")
						return u
					},
					config.ContainersPath, WorkloadTypeInferenceService)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			ctx, env, teardown := envtestutil.SetupEnvAndClientWithCRDs(
				t,
				[]envt.RegisterWebhooksFn{envtestutil.RegisterWebhooks},
				envtestutil.DefaultWebhookTimeout,
				envtestutil.WithInferenceService(),
			)
			defer teardown()

			// Create test namespace
			ns := fmt.Sprintf("test-ns-%s", xid.New().String())
			testNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: ns},
			}
			g.Expect(env.Client().Create(ctx, testNamespace)).To(Succeed())

			// Add HardwareProfile types to scheme for client operations
			g.Expect(hwpv1alpha1.AddToScheme(env.Scheme())).To(Succeed())

			// Run the specific test case
			tc.test(g, ctx, env.Client(), ns)
		})
	}
}

// TestHardwareProfile_CRDValidation tests the CRD validation for HardwareProfile resources.
func TestHardwareProfile_CRDValidation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		hwpOptions    []envtestutil.ObjectOption
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid queue scheduling configuration",
			hwpOptions:  []envtestutil.ObjectOption{envtestutil.WithKueueScheduling("test-queue")},
			expectError: false,
		},
		{
			name: "valid node scheduling configuration",
			hwpOptions: []envtestutil.ObjectOption{
				envtestutil.WithNodeScheduling(
					map[string]string{
						"accelerator": "nvidia-tesla-v100",
						"zone":        "us-west-1a",
					},
					[]corev1.Toleration{
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
				),
			},
			expectError: false,
		},
		{
			name:          "invalid: queue scheduling without local queue name",
			hwpOptions:    []envtestutil.ObjectOption{envtestutil.WithKueueScheduling("")},
			expectError:   true,
			errorContains: "spec.scheduling.kueue.localQueueName",
		},
		{
			name: "invalid: queue scheduling with node configuration",
			// Primary scheduling type (queue) set last to determine final SchedulingType
			hwpOptions: []envtestutil.ObjectOption{
				envtestutil.WithNodeSelector(map[string]string{"test": "value"}),
				envtestutil.WithKueueScheduling("test-queue"),
			},
			expectError:   true,
			errorContains: "and the 'node' field must not be set",
		},
		{
			name: "invalid: node scheduling with kueue configuration",
			// Primary scheduling type (node) set last to determine final SchedulingType
			hwpOptions: []envtestutil.ObjectOption{
				envtestutil.WithKueueScheduling("test-queue"),
				envtestutil.WithNodeSelector(map[string]string{"test": "value"}),
			},
			expectError:   true,
			errorContains: "and the 'kueue' field must not be set",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			ctx, env, teardown := envtestutil.SetupEnvAndClientWithCRDs(
				t,
				[]envt.RegisterWebhooksFn{envtestutil.RegisterWebhooks},
				envtestutil.DefaultWebhookTimeout,
				envtestutil.WithNotebook(),
				envtestutil.WithInferenceService(),
			)
			t.Cleanup(teardown)

			// Create test namespace
			ns := fmt.Sprintf("test-ns-%s", xid.New().String())
			testNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: ns},
			}
			g.Expect(env.Client().Create(ctx, testNamespace)).To(Succeed())

			// Add HardwareProfile types to scheme for client operations
			g.Expect(hwpv1alpha1.AddToScheme(env.Scheme())).To(Succeed())

			// Create hardware profile with test case specific options
			hwp := envtestutil.NewHardwareProfile(fmt.Sprintf("test-hwp-%s", xid.New().String()), ns, tc.hwpOptions...)

			err := env.Client().Create(ctx, hwp)

			if tc.expectError {
				g.Expect(err).To(HaveOccurred(), "Expected creation to fail but it succeeded")
				g.Expect(err.Error()).To(ContainSubstring(tc.errorContains))
			} else {
				g.Expect(err).To(Succeed(), fmt.Sprintf("Expected creation to succeed but got: %v", err))
			}
		})
	}
}
