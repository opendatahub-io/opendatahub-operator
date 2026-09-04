package hardwareprofile_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
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
	WorkloadTypeInferenceService    WorkloadType = "InferenceService"
	WorkloadTypeLlmInferenceService WorkloadType = "LlmInferenceService"
	WorkloadLabelKueueQueueName     string       = "kueue.x-k8s.io/queue-name"
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
		// For container-based workloads, work with containers
		containers, found, err := unstructured.NestedSlice(workloadUnstructured.Object, containersPath...)
		g.Expect(err).ShouldNot(HaveOccurred(), "should get containers from workload")
		g.Expect(found).Should(BeTrue(), "containers should be found")
		g.Expect(containers).Should(HaveLen(1), "should have exactly one container")

		container, ok := containers[0].(map[string]any)
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
		toleration, ok := tolerations[i].(map[string]any)
		g.Expect(ok).Should(BeTrue(), fmt.Sprintf("toleration %d should be a map", i))

		for key, value := range expectedToleration {
			g.Expect(toleration).Should(HaveKeyWithValue(key, value))
		}
	}
}

// createResourceHardwareProfile creates a hardware profile with resource identifiers for testing.
func createResourceHardwareProfile(name, namespace string) *infrav1.HardwareProfile {
	return envtestutil.NewHardwareProfile(name, namespace,
		envtestutil.WithCPUIdentifier("2", "4", "8"),
		envtestutil.WithMemoryIdentifier("4Gi", "8Gi", "16Gi"),
		envtestutil.WithGPUIdentifier("nvidia.com/gpu", "1", "1", "4"),
	)
}

// createKueueHardwareProfile creates a hardware profile with Kueue configuration for testing.
func createKueueHardwareProfile(name, namespace, queueName string) *infrav1.HardwareProfile {
	return envtestutil.NewHardwareProfile(name, namespace,
		envtestutil.WithKueueScheduling(queueName),
	)
}

// createNodeSchedulingHardwareProfile creates a hardware profile with node scheduling configuration.
func createNodeSchedulingHardwareProfile(name, namespace string) *infrav1.HardwareProfile {
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
func createSimpleHardwareProfile(name, namespace string) *infrav1.HardwareProfile {
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
		// For container-based workloads, work with containers
		containers, found, err := unstructured.NestedSlice(workloadUnstructured.Object, containersPath...)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())
		g.Expect(containers).Should(HaveLen(1))

		container, ok := containers[0].(map[string]any)
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
		// For workloads using labels - uses labels
		g.Expect(resources.HasLabel(workload, expectedLabelKey, queueName)).Should(BeTrue())
	} else {
		// This branch should no longer be used since all workloads use labels
		actualQueueName := resources.GetAnnotation(workload, WorkloadLabelKueueQueueName)
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
		hardwareprofilewebhook.HardwareProfileNameAnnotation: "update-profile",
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

// TestHardwareProfileWebhook_LlmInferenceService for the mutating webhook logic for hardware profile injection on LlmInferenceService workloads.
func TestHardwareProfileWebhook_LlmInferenceService(t *testing.T) {
	t.Skip("HWP injection for LLMInferenceService migrated to odh-model-controller (RHOAIENG-62536)")
	t.Parallel()

	testCases := []struct {
		name string
		test func(g Gomega, ctx context.Context, k8sClient client.Client, ns string)
	}{
		// LLMInferenceService test cases
		{
			name: "llminferenceservice - no hardware profile annotation",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				config, err := hardwareprofilewebhook.GetWorkloadConfig("LLMInferenceService")
				g.Expect(err).ShouldNot(HaveOccurred())
				testUpdateOperationForWorkload(g, ctx, k8sClient, ns, "test-llmisvce-no-annotation",
					func() client.Object { return envtestutil.NewLLMInferenceService("test-llmisvce-no-annotation", ns) },
					func() *unstructured.Unstructured {
						u := &unstructured.Unstructured{}
						u.SetAPIVersion("serving.kserve.io/v1alpha1")
						u.SetKind("LLMInferenceService")
						return u
					},
					config.ContainersPath, WorkloadTypeLlmInferenceService)
			},
		},
		{
			name: "llminferenceservice - valid hardwareprofile with resources",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				config, err := hardwareprofilewebhook.GetWorkloadConfig("LLMInferenceService")
				g.Expect(err).ShouldNot(HaveOccurred())
				testValidHardwareProfileWithResourcesForWorkload(g, ctx, k8sClient, ns,
					func() client.Object {
						return envtestutil.NewLLMInferenceService("test-llmisvc-resources", ns,
							envtestutil.WithHardwareProfile("resource-profile"))
					},
					config.ContainersPath, WorkloadTypeLlmInferenceService)
			},
		},
		{
			name: "llminferenceservice - hardware profile with node scheduling",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				config, err := hardwareprofilewebhook.GetWorkloadConfig("LLMInferenceService")
				g.Expect(err).ShouldNot(HaveOccurred())
				testHardwareProfileWithNodeSchedulingForWorkload(g, ctx, k8sClient, ns,
					func() client.Object {
						return envtestutil.NewLLMInferenceService("test-llmisvc-node", ns,
							envtestutil.WithHardwareProfile("node-profile"))
					},
					config.NodeSelectorPath, config.TolerationsPath)
			},
		},
		{
			name: "llminferenceservice - hardware profile with kueue scheduling",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				testHardwareProfileWithKueueForWorkload(g, ctx, k8sClient, ns,
					func() client.Object {
						return envtestutil.NewLLMInferenceService("test-llmisvc-kueue", ns,
							envtestutil.WithHardwareProfile("kueue-profile"))
					},
					"test-queue", WorkloadLabelKueueQueueName)
			},
		},
	}
	ctx, env := envtestutil.SetupSharedEnvForSubtests(
		t,
		[]envt.RegisterWebhooksFn{envtestutil.RegisterWebhooks},
		[]envt.RegisterControllersFn{},
		envtestutil.DefaultWebhookTimeout,
		envtestutil.WithLlmInferenceService(),
	)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			// Create test namespace
			ns := fmt.Sprintf("test-ns-%s", xid.New().String())
			testNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: ns},
			}
			g.Expect(env.Client().Create(ctx, testNamespace)).To(Succeed())

			// Run the specific test case
			tc.test(g, ctx, env.Client(), ns)
		})
	}
}

// TestHardwareProfileWebhook_InferenceService for the mutating webhook logic for hardware profile injection on InferenceService workloads.
func TestHardwareProfileWebhook_InferenceService(t *testing.T) {
	t.Skip("HWP injection for InferenceService migrated to odh-model-controller (RHOAIENG-62536)")
	t.Parallel()
	testCases := []struct {
		name string
		test func(g Gomega, ctx context.Context, k8sClient client.Client, ns string)
	}{
		{
			name: "inferenceservice - no hardware profile annotation",
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
			name: "inferenceservice - valid hardware profile with resources",
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
			name: "inferenceservice - hardware profile with node scheduling",
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
			name: "inferenceservice - hardware profile with Kueue",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				testHardwareProfileWithKueueForWorkload(g, ctx, k8sClient, ns,
					func() client.Object {
						return envtestutil.NewInferenceService("test-inference-service-kueue", ns,
							envtestutil.WithHardwareProfile("kueue-profile"))
					},
					"test-queue", WorkloadLabelKueueQueueName)
			},
		},
		{
			name: "inferenceservice - update operation",
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
	ctx, env := envtestutil.SetupSharedEnvForSubtests(
		t,
		[]envt.RegisterWebhooksFn{envtestutil.RegisterWebhooks},
		[]envt.RegisterControllersFn{},
		envtestutil.DefaultWebhookTimeout,
		envtestutil.WithInferenceService(),
	)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			// Create test namespace
			ns := fmt.Sprintf("test-ns-%s", xid.New().String())
			testNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: ns},
			}
			g.Expect(env.Client().Create(ctx, testNamespace)).To(Succeed())

			// Run the specific test case
			tc.test(g, ctx, env.Client(), ns)
		})
	}
}

// TestHardwareProfile_CRDValidation tests the CRD validation for HardwareProfile resources.
func TestHardwareProfile_CRDValidation(t *testing.T) {
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
			name:        "valid DRA configuration",
			hwpOptions:  []envtestutil.ObjectOption{envtestutil.WithDRA("gpu-claim")},
			expectError: false,
		},
		{
			name:          "invalid DRA ResourceClaimTemplate name",
			hwpOptions:    []envtestutil.ObjectOption{envtestutil.WithDRA("GPU-claim")},
			expectError:   true,
			errorContains: "resourceClaimTemplateName",
		},
		{
			name:          "invalid DRA ResourceClaimTemplate name length",
			hwpOptions:    []envtestutil.ObjectOption{envtestutil.WithDRA(strings.Repeat("a", 254))},
			expectError:   true,
			errorContains: "resourceClaimTemplateName",
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

	ctx, env := envtestutil.SetupSharedEnvForSubtests(
		t,
		[]envt.RegisterWebhooksFn{envtestutil.RegisterWebhooks},
		[]envt.RegisterControllersFn{},
		envtestutil.DefaultWebhookTimeout,
		envtestutil.WithInferenceService(),
	)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			// Create test namespace
			ns := fmt.Sprintf("test-ns-%s", xid.New().String())
			testNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: ns},
			}
			g.Expect(env.Client().Create(ctx, testNamespace)).To(Succeed())

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
