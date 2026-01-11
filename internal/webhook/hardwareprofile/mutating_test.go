package hardwareprofile_test

import (
	"context"
	"strings"
	"testing"

	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/hardwareprofile"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

const (
	testNamespace           = "test-ns"
	hwpNamespace            = "hwp-ns"
	testNotebook            = "test-notebook"
	testInferenceService    = "test-isvc"
	testLLMInferenceService = "test-llmisvc"
	testHardwareProfile     = "test-hardware-profile"
	testQueue               = "test-queue"
)

// setupTestEnvironment creates the common test environment for webhook tests.
func setupTestEnvironment(t *testing.T) (*runtime.Scheme, context.Context) {
	t.Helper()
	sch, err := scheme.New()
	NewWithT(t).Expect(err).ShouldNot(HaveOccurred())

	err = infrav1.AddToScheme(sch)
	NewWithT(t).Expect(err).ShouldNot(HaveOccurred())

	return sch, t.Context()
}

// createWebhookInjector creates a webhook injector with the given client and scheme.
func createWebhookInjector(cli client.Client, sch *runtime.Scheme) *hardwareprofile.Injector {
	injector := &hardwareprofile.Injector{
		Client:  cli,
		Decoder: admission.NewDecoder(sch),
		Name:    "test",
	}
	return injector
}

// Helper functions for test simplification.

// setContainerResources sets container resources for notebook workloads.
func setContainerResources(notebook *unstructured.Unstructured, resourceType, resourceKey, value string) {
	containers, _, err := unstructured.NestedSlice(notebook.Object, "spec", "template", "spec", "containers")
	if err != nil {
		return
	}
	if len(containers) == 0 {
		return
	}
	containerMap, ok := containers[0].(map[string]interface{})
	if !ok {
		return
	}
	containerMap["resources"] = map[string]interface{}{
		resourceType: map[string]interface{}{
			resourceKey: value,
		},
	}
	_ = unstructured.SetNestedSlice(notebook.Object, containers, "spec", "template", "spec", "containers")
}

func hasResourcePatches(patches []jsonpatch.JsonPatchOperation) bool {
	for _, patch := range patches {
		if strings.Contains(patch.Path, "/resources") {
			return true
		}
	}
	return false
}

// TestHardwareProfile_AllowsRequests tests various scenarios where the webhook should allow requests without processing.
func TestHardwareProfile_AllowsRequests(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	testCases := []struct {
		name      string
		operation admissionv1.Operation
		notebook  client.Object
	}{
		{
			name:      "requests without hardware profile annotation",
			operation: admissionv1.Create,
			notebook:  envtestutil.NewNotebook(testNotebook, testNamespace),
		},
		{
			name:      "unsupported operations (DELETE)",
			operation: admissionv1.Delete,
			notebook:  envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)),
		},
		{
			name:      "empty hardware profile annotation value",
			operation: admissionv1.Create,
			notebook:  envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithAnnotation(hardwareprofile.HardwareProfileNameAnnotation, "")),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cli := fake.NewClientBuilder().WithScheme(sch).Build()
			injector := createWebhookInjector(cli, sch)

			req := envtestutil.NewAdmissionRequest(
				t,
				tc.operation,
				tc.notebook,
				gvk.Notebook,
				metav1.GroupVersionResource{
					Group:    gvk.Notebook.Group,
					Version:  gvk.Notebook.Version,
					Resource: "notebooks",
				},
			)

			resp := injector.Handle(ctx, req)
			g.Expect(resp.Allowed).Should(BeTrue())
			g.Expect(resp.Patches).Should(BeEmpty())
		})
	}
}

// TestHardwareProfile_DeniesWhenDecoderNotInitialized tests that requests are denied when decoder is nil.
func TestHardwareProfile_DeniesWhenDecoderNotInitialized(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	// Create injector WITHOUT decoder injection
	injector := &hardwareprofile.Injector{
		Name: "test-injector",
		// Decoder is intentionally nil to test the nil check
	}

	// Create a test request
	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		envtestutil.NewNotebook(testNotebook, testNamespace),
		gvk.Notebook,
		metav1.GroupVersionResource{
			Group:    gvk.Notebook.Group,
			Version:  gvk.Notebook.Version,
			Resource: "notebooks",
		},
	)

	// Handle the request
	ctx := t.Context()
	resp := injector.Handle(ctx, req)

	// Should deny the request due to nil decoder
	g.Expect(resp.Allowed).Should(BeFalse())
	g.Expect(resp.Result.Message).Should(ContainSubstring("webhook decoder not initialized"))
}

// TestHardwareProfile_DeniesWhenProfileNotFound tests that requests are denied when hardware profile is not found.
func TestHardwareProfile_DeniesWhenProfileNotFound(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)
	cli := fake.NewClientBuilder().WithScheme(sch).Build()
	injector := createWebhookInjector(cli, sch)

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile("nonexistent")),
		gvk.Notebook,
		metav1.GroupVersionResource{
			Group:    gvk.Notebook.Group,
			Version:  gvk.Notebook.Version,
			Resource: "notebooks",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeFalse())
	g.Expect(resp.Result.Message).Should(ContainSubstring("hardware profile 'nonexistent' not found"))
}

// TestHardwareProfile_AppliesKueueConfiguration tests that hardware profiles with Kueue configuration are applied correctly.
func TestHardwareProfile_AppliesKueueConfiguration(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithMemoryIdentifier("4Gi", "8Gi"),
		envtestutil.WithKueueScheduling(testQueue, "high-priority"),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)),
		gvk.Notebook,
		metav1.GroupVersionResource{
			Group:    gvk.Notebook.Group,
			Version:  gvk.Notebook.Version,
			Resource: "notebooks",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())
	g.Expect(resp.Patches).Should(Not(BeEmpty()))
}

// TestHardwareProfile_SetsNamespaceAnnotation tests that hardware profile namespace annotation is set correctly.
func TestHardwareProfile_SetsNamespaceAnnotation(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithResourceIdentifiers(infrav1.HardwareIdentifier{
			DisplayName:  "Test Resource",
			Identifier:   "test.com/resource",
			MinCount:     intstr.FromString("1"),
			DefaultCount: intstr.FromString("1"),
		}),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)),
		gvk.Notebook,
		metav1.GroupVersionResource{
			Group:    gvk.Notebook.Group,
			Version:  gvk.Notebook.Version,
			Resource: "notebooks",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())
	g.Expect(resp.Patches).Should(Not(BeEmpty()))
}

// TestHardwareProfile_HandlesUpdateOperations tests that update operations are handled correctly.
func TestHardwareProfile_HandlesUpdateOperations(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create a hardware profile with multiple types of specifications
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithMemoryIdentifier("4Gi", "8Gi"),
		envtestutil.WithKueueScheduling("update-test-queue"),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	// Test UPDATE operation specifically
	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Update,
		envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)),
		gvk.Notebook,
		metav1.GroupVersionResource{
			Group:    gvk.Notebook.Group,
			Version:  gvk.Notebook.Version,
			Resource: "notebooks",
		},
	)

	resp := injector.Handle(ctx, req)

	// Verify update operation is processed correctly
	g.Expect(resp.Allowed).Should(BeTrue())
	g.Expect(resp.Patches).Should(Not(BeEmpty()))

	// Verify specific patches are applied for update operations
	g.Expect(resp.Result).Should(BeNil(), "Update operations should not return error results")
}

// TestHardwareProfile_ErrorPaths tests various error conditions in the webhook.
func TestHardwareProfile_ErrorPaths(t *testing.T) {
	t.Parallel()
	sch, ctx := setupTestEnvironment(t)

	testCases := []struct {
		name          string
		injector      *hardwareprofile.Injector
		workload      client.Object
		expectAllowed bool
		expectMessage string
	}{
		{
			name: "nil decoder",
			injector: &hardwareprofile.Injector{
				Client: fake.NewClientBuilder().WithScheme(sch).Build(),
				Name:   "test",
				// Decoder is nil
			},
			workload:      envtestutil.NewNotebook(testNotebook, testNamespace),
			expectAllowed: false,
			expectMessage: "webhook decoder not initialized",
		},
		{
			name:     "unexpected kind",
			injector: createWebhookInjector(fake.NewClientBuilder().WithScheme(sch).Build(), sch),
			workload: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: testNamespace,
				},
			},
			expectAllowed: false,
			expectMessage: "unexpected kind: Pod",
		},
		{
			name:     "missing hardware profile namespace",
			injector: createWebhookInjector(fake.NewClientBuilder().WithScheme(sch).Build(), sch),
			workload: func() client.Object {
				notebook := &unstructured.Unstructured{}
				notebook.SetGroupVersionKind(gvk.Notebook)
				notebook.SetName(testNotebook)
				// No namespace set
				notebook.SetAnnotations(map[string]string{
					hardwareprofile.HardwareProfileNameAnnotation: testHardwareProfile,
				})
				return notebook
			}(),
			expectAllowed: false,
			expectMessage: "unable to determine hardware profile namespace",
		},
		{
			name:          "hardware profile not found",
			injector:      createWebhookInjector(fake.NewClientBuilder().WithScheme(sch).Build(), sch),
			workload:      envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile("non-existent")),
			expectAllowed: false,
			expectMessage: "hardware profile 'non-existent' not found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			// Use default GVK and GVR for Notebook since all error cases test notebooks or pods
			var gvkToUse schema.GroupVersionKind
			var gvrToUse metav1.GroupVersionResource

			if _, ok := tc.workload.(*corev1.Pod); ok {
				// Handle Pod case
				gvkToUse = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
				gvrToUse = metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
			} else {
				// Default to Notebook case
				gvkToUse = gvk.Notebook
				gvrToUse = metav1.GroupVersionResource{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Resource: "notebooks"}
			}

			req := envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				tc.workload,
				gvkToUse,
				gvrToUse,
			)

			resp := tc.injector.Handle(ctx, req)

			g.Expect(resp.Allowed).Should(Equal(tc.expectAllowed))
			if tc.expectMessage != "" {
				g.Expect(resp.Result.Message).Should(ContainSubstring(tc.expectMessage))
			}
		})
	}
}

// TestHardwareProfile_ConvertIntOrStringToQuantity tests the quantity conversion utility.
func TestHardwareProfile_ConvertIntOrStringToQuantity(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		input       intstr.IntOrString
		expectError bool
		expected    string
	}{
		{
			name:        "integer value",
			input:       intstr.FromInt(4),
			expectError: false,
			expected:    "4",
		},
		{
			name:        "string value",
			input:       intstr.FromString("8Gi"),
			expectError: false,
			expected:    "8Gi",
		},
		{
			name:        "invalid string value",
			input:       intstr.FromString("invalid-quantity"),
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			// We need to test this through the webhook since the function is not exported
			// Create a hardware profile with the test value
			hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
				envtestutil.WithResourceIdentifiers(infrav1.HardwareIdentifier{
					DisplayName:  "Test Resource",
					Identifier:   "test.com/resource",
					DefaultCount: tc.input,
				}),
			)

			sch, ctx := setupTestEnvironment(t)
			cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
			injector := createWebhookInjector(cli, sch)

			// Create a minimal notebook without any existing containers/resources
			notebook := &unstructured.Unstructured{}
			notebook.SetGroupVersionKind(gvk.Notebook)
			notebook.SetName(testNotebook)
			notebook.SetNamespace(testNamespace)
			notebook.SetAnnotations(map[string]string{
				hardwareprofile.HardwareProfileNameAnnotation: testHardwareProfile,
			})
			// Set minimal spec structure without containers so resources will be injected
			err := unstructured.SetNestedMap(notebook.Object, map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "notebook",
								"image": "notebook:latest",
								// No resources defined - will trigger injection
							},
						},
					},
				},
			}, "spec")
			g.Expect(err).ShouldNot(HaveOccurred())

			req := envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				notebook,
				gvk.Notebook,
				metav1.GroupVersionResource{
					Group:    gvk.Notebook.Group,
					Version:  gvk.Notebook.Version,
					Resource: "notebooks",
				},
			)

			resp := injector.Handle(ctx, req)

			if tc.expectError {
				g.Expect(resp.Allowed).Should(BeFalse())
				g.Expect(resp.Result.Code).Should(Equal(int32(500)))
			} else {
				g.Expect(resp.Allowed).Should(BeTrue())
				if !tc.expectError {
					// Verify the patch contains the expected quantity
					patchFound := false
					for _, patch := range resp.Patches {
						if patch.Operation == "add" && strings.Contains(patch.Path, "/resources") {
							// The patch value should be a map containing requests with our resource
							if resourcesMap, ok := patch.Value.(map[string]interface{}); ok {
								if requests, ok := resourcesMap["requests"].(map[string]interface{}); ok {
									if quantity, exists := requests["test.com/resource"]; exists {
										g.Expect(quantity).Should(Equal(tc.expected))
										patchFound = true
										break
									}
								}
							}
						}
					}
					// For successful cases, we should always find a patch since we're adding a new resource
					if tc.expected != "" {
						g.Expect(patchFound).Should(BeTrue(), "Expected patch with quantity not found")
					}
				}
			}
		})
	}
}

// TestHardwareProfile_UnsupportedWorkloadKind tests error handling for malformed workload structures.
func TestHardwareProfile_UnsupportedWorkloadKind(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create hardware profile with node scheduling
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithNodeSelector(map[string]string{"test": "value"}),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	// Test with a supported kind but with malformed container structure to trigger error paths
	notebookUnstructured := &unstructured.Unstructured{}
	notebookUnstructured.SetGroupVersionKind(gvk.Notebook)
	notebookUnstructured.SetName(testNotebook)
	notebookUnstructured.SetNamespace(testNamespace)
	notebookUnstructured.SetAnnotations(map[string]string{
		hardwareprofile.HardwareProfileNameAnnotation: testHardwareProfile,
	})

	// Set malformed spec that will cause container access to fail
	err := unstructured.SetNestedMap(notebookUnstructured.Object, map[string]interface{}{
		"template": map[string]interface{}{
			"spec": "invalid-spec-should-be-map", // This will cause an error
		},
	}, "spec")
	g.Expect(err).ShouldNot(HaveOccurred()) // SetNestedMap should succeed

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		notebookUnstructured,
		gvk.Notebook,
		metav1.GroupVersionResource{
			Group:    gvk.Notebook.Group,
			Version:  gvk.Notebook.Version,
			Resource: "notebooks",
		},
	)

	resp := injector.Handle(ctx, req)
	// The webhook should fail when trying to access containers in the malformed structure
	g.Expect(resp.Allowed).Should(BeFalse())
	g.Expect(resp.Result.Code).Should(Equal(int32(500)))
}

// test base on different workload types:
// - Notebook

// TestHardwareProfile_SchedulingConfiguration_Notebook tests that hardware profiles with different
// scheduling configurations are applied correctly to Notebook workloads.
func TestHardwareProfile_SchedulingConfiguration_Notebook(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	testCases := []struct {
		name          string
		hwpOptions    []envtestutil.ObjectOption
		setupWorkload func() *unstructured.Unstructured
		expectPatches bool
	}{
		{
			name: "applies node scheduling to clean workload",
			hwpOptions: []envtestutil.ObjectOption{
				envtestutil.WithCPUIdentifier("2", "2"),
				envtestutil.WithNodeScheduling(
					map[string]string{"node-type": "gpu-node"},
					[]corev1.Toleration{{
						Key:      "nvidia.com/gpu",
						Operator: corev1.TolerationOpEqual,
						Value:    "true",
						Effect:   corev1.TaintEffectNoSchedule,
					}},
				),
			},
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")
				return workload
			},
			expectPatches: true,
		},
		{
			name: "applies kueue scheduling to clean workload",
			hwpOptions: []envtestutil.ObjectOption{
				envtestutil.WithMemoryIdentifier("4Gi", "4Gi"),
				envtestutil.WithKueueScheduling(testQueue, "high-priority"),
			},
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")
				return workload
			},
			expectPatches: true,
		},
		{
			name: "applies kueue scheduling even when resources exist",
			hwpOptions: []envtestutil.ObjectOption{
				envtestutil.WithCPUIdentifier("4", "4"),
				envtestutil.WithKueueScheduling(testQueue),
			},
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")
				// Add existing CPU requests (this should prevent resource injection but allow scheduling)
				setContainerResources(workload, "requests", "cpu", "2")
				return workload
			},
			expectPatches: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace, tc.hwpOptions...)
			cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
			injector := createWebhookInjector(cli, sch)

			workload := tc.setupWorkload()

			req := envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				workload,
				gvk.Notebook,
				metav1.GroupVersionResource{
					Group:    gvk.Notebook.Group,
					Version:  gvk.Notebook.Version,
					Resource: "notebooks",
				},
			)

			resp := injector.Handle(ctx, req)
			g.Expect(resp.Allowed).Should(BeTrue())
			g.Expect(resp.Patches).Should(Not(BeEmpty()))
		})
	}
}

// TestHardwareProfile_ClearsKueue ensures that when a workload
// is annotated with a HardwareProfile that has no SchedulingSpec, any existing kueue label
// configuration is removed by the webhook.
func TestHardwareProfile_ClearsKueue(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create a HardwareProfile that has no scheduling spec (empty/no config)
	newProfileName := "profile-without-sched"
	hwpNew := envtestutil.NewHardwareProfile(newProfileName, testNamespace)

	// Build fake client with the new profile only
	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwpNew).Build()
	injector := createWebhookInjector(cli, sch)

	// Create a Notebook workload that currently has scheduling fields present
	notebook := &unstructured.Unstructured{}
	notebook.SetGroupVersionKind(gvk.Notebook)
	notebook.SetName(testNotebook)
	notebook.SetNamespace(testNamespace)
	// Set annotation to point at the new profile (which lacks scheduling)
	notebook.SetAnnotations(map[string]string{
		hardwareprofile.HardwareProfileNameAnnotation: newProfileName,
	})
	// Add an existing kueue label (upstream form)
	notebook.SetLabels(map[string]string{"kueue.x-k8s.io/queue-name": "old-queue"})

	// Populate spec.template.spec with nodeSelector, tolerations and a minimal container
	err := unstructured.SetNestedMap(notebook.Object, map[string]interface{}{
		"template": map[string]interface{}{
			"spec": map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{"name": "notebook", "image": "notebook:latest"},
				},
			},
		},
	}, "spec")
	g.Expect(err).ShouldNot(HaveOccurred())

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Update,
		notebook,
		gvk.Notebook,
		metav1.GroupVersionResource{
			Group:    gvk.Notebook.Group,
			Version:  gvk.Notebook.Version,
			Resource: "notebooks",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())

	// Expect patches that remove nodeSelector, tolerations and the kueue label
	foundKueueLabel := false

	for _, patch := range resp.Patches {
		if strings.Contains(patch.Path, "queue-name") {
			foundKueueLabel = true
		}
	}

	g.Expect(foundKueueLabel).Should(BeTrue(), "expected a patch removing kueue label")
}

// TestHardwareProfile_ClearsHWP ensures that when a workload
// is annotated with a HardwareProfile that has no SchedulingSpec, any existing scheduling
// configuration (nodeSelector, tolerations) on the workload is removed by the webhook.
func TestHardwareProfile_ClearsHWP(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create a HardwareProfile that has no scheduling spec (empty/no config)
	newProfileName := "profile-without-sched"
	hwpNew := envtestutil.NewHardwareProfile(newProfileName, testNamespace)

	// Build fake client with the new profile only
	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwpNew).Build()
	injector := createWebhookInjector(cli, sch)

	// Create a Notebook workload that currently has scheduling fields present
	notebook := &unstructured.Unstructured{}
	notebook.SetGroupVersionKind(gvk.Notebook)
	notebook.SetName(testNotebook)
	notebook.SetNamespace(testNamespace)
	// Set annotation to point at the new profile (which lacks scheduling)
	notebook.SetAnnotations(map[string]string{
		hardwareprofile.HardwareProfileNameAnnotation: newProfileName,
	})
	// Populate spec.template.spec with nodeSelector, tolerations and a minimal container
	err := unstructured.SetNestedMap(notebook.Object, map[string]interface{}{
		"template": map[string]interface{}{
			"spec": map[string]interface{}{
				"nodeSelector": map[string]interface{}{"node-type": "gpu-node"},
				"tolerations": []interface{}{
					map[string]interface{}{
						"key":      "nvidia.com/gpu",
						"operator": "Equal",
						"value":    "true",
						"effect":   "NoSchedule",
					},
				},
				"containers": []interface{}{
					map[string]interface{}{"name": "notebook", "image": "notebook:latest"},
				},
			},
		},
	}, "spec")
	g.Expect(err).ShouldNot(HaveOccurred())

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Update,
		notebook,
		gvk.Notebook,
		metav1.GroupVersionResource{
			Group:    gvk.Notebook.Group,
			Version:  gvk.Notebook.Version,
			Resource: "notebooks",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())

	// Expect patches that remove nodeSelector, tolerations and the kueue label
	foundNodeSelector := false
	foundTolerations := false
	for _, patch := range resp.Patches {
		if strings.Contains(patch.Path, "nodeSelector") {
			foundNodeSelector = true
		}
		if strings.Contains(patch.Path, "tolerations") {
			foundTolerations = true
		}
	}

	g.Expect(foundNodeSelector).Should(BeTrue(), "expected a patch removing nodeSelector")
	g.Expect(foundTolerations).Should(BeTrue(), "expected a patch removing tolerations")
}

// TestHardwareProfile_ResourceInjection_Notebook tests that hardware profiles with resource requirements are applied correctly to Notebook workloads.
func TestHardwareProfile_ResourceInjection_Notebook(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create hardware profile with CPU and memory identifiers (including limits)
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithCPUIdentifier("0", "4", "8"),         // min: 0, default: 4, max: 8
		envtestutil.WithMemoryIdentifier("0", "8Gi", "16Gi"), // min: 0, default: 8Gi, max: 16Gi
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	testCases := []struct {
		name                string
		setupWorkload       func() *unstructured.Unstructured
		expectResourcePatch bool
	}{
		{
			name: "applies resources when none exist",
			setupWorkload: func() *unstructured.Unstructured {
				workload := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
				workloadUnstructured, ok := workload.(*unstructured.Unstructured)
				if !ok {
					return nil
				}
				return workloadUnstructured
			},
			expectResourcePatch: true,
		},
		{
			name: "preserves existing resources",
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")

				// Set existing resources that should be preserved
				containers, _, _ := unstructured.NestedSlice(workload.Object, "spec", "template", "spec", "containers")
				containerMap, ok := containers[0].(map[string]interface{})
				g.Expect(ok).Should(BeTrue(), "container should be a map")
				containerMap["resources"] = map[string]interface{}{
					"requests": map[string]interface{}{
						"cpu":    "2",
						"memory": "4Gi",
					},
					"limits": map[string]interface{}{
						"cpu":    "4",
						"memory": "8Gi",
					},
				}
				_ = unstructured.SetNestedSlice(workload.Object, containers, "spec", "template", "spec", "containers")
				return workload
			},
			expectResourcePatch: false,
		},
		{
			name: "applies only missing resources when single container has partial resources",
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")

				// Set only CPU request, memory should be applied from HWP
				containers, _, _ := unstructured.NestedSlice(workload.Object, "spec", "template", "spec", "containers")
				containerMap, ok := containers[0].(map[string]interface{})
				g.Expect(ok).Should(BeTrue(), "container should be a map")
				containerMap["resources"] = map[string]interface{}{
					"requests": map[string]interface{}{
						"cpu": "1",
					},
				}
				_ = unstructured.SetNestedSlice(workload.Object, containers, "spec", "template", "spec", "containers")
				return workload
			},
			expectResourcePatch: true,
		},
		{
			name: "applies resources to containers without them when multiple containers exist",
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")

				// For Notebooks, create two containers - first has CPU, second has memory, both should get missing resources
				containers := []interface{}{
					map[string]interface{}{
						"name":  "main-container",
						"image": "notebook:latest",
						"resources": map[string]interface{}{
							"requests": map[string]interface{}{
								"cpu": "1",
								// Missing memory - should get HWP memory
							},
						},
					},
					map[string]interface{}{
						"name":  "sidecar-container",
						"image": "sidecar:latest",
						"resources": map[string]interface{}{
							"requests": map[string]interface{}{
								"memory": "2Gi",
								// Missing CPU - should get HWP CPU
							},
						},
					},
				}
				_ = unstructured.SetNestedSlice(workload.Object, containers, "spec", "template", "spec", "containers")
				return workload
			},
			expectResourcePatch: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			workload := tc.setupWorkload()

			req := envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				workload,
				gvk.Notebook,
				metav1.GroupVersionResource{
					Group:    gvk.Notebook.Group,
					Version:  gvk.Notebook.Version,
					Resource: "notebooks",
				},
			)

			resp := injector.Handle(ctx, req)
			g.Expect(resp.Allowed).Should(BeTrue())
			g.Expect(hasResourcePatches(resp.Patches)).Should(Equal(tc.expectResourcePatch))
		})
	}
}

// TestHardwareProfile_SupportsCrossNamespaceAccess_Notebook tests that hardware profiles can be accessed from different namespaces for Notebook workloads.
func TestHardwareProfile_SupportsCrossNamespaceAccess_Notebook(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create hardware profile in different namespace
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, hwpNamespace,
		envtestutil.WithGPUIdentifier("nvidia.com/gpu", "1", "1"),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	workload := envtestutil.NewNotebook(testNotebook, testNamespace,
		envtestutil.WithHardwareProfile(testHardwareProfile),
		envtestutil.WithHardwareProfileNamespace(hwpNamespace),
	)

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		workload,
		gvk.Notebook,
		metav1.GroupVersionResource{
			Group:    gvk.Notebook.Group,
			Version:  gvk.Notebook.Version,
			Resource: "notebooks",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())
	g.Expect(resp.Patches).Should(Not(BeEmpty()))
}

// TestHardwareProfile_ResourceLimits_Notebook tests that hardware profiles with MaxCount are applied as limits for Notebook workloads.
func TestHardwareProfile_ResourceLimits_Notebook(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create hardware profile with CPU and memory identifiers that include limits
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithCPUIdentifier("1", "2", "4"),                   // min: 1, default: 2, max: 4
		envtestutil.WithMemoryIdentifier("1Gi", "2Gi", "4Gi"),          // min: 1Gi, default: 2Gi, max: 4Gi
		envtestutil.WithGPUIdentifier("nvidia.com/gpu", "0", "1", "2"), // min: 0, default: 1, max: 2
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	workload := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
	workloadUnstructured, ok := workload.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue(), "workload should be unstructured")

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		workloadUnstructured,
		gvk.Notebook,
		metav1.GroupVersionResource{
			Group:    gvk.Notebook.Group,
			Version:  gvk.Notebook.Version,
			Resource: "notebooks",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())
	g.Expect(resp.Patches).Should(Not(BeEmpty()))

	// Verify that resources were applied
	hasResourcesPatch := false
	for _, patch := range resp.Patches {
		if strings.Contains(patch.Path, "/resources") {
			hasResourcesPatch = true

			// Check if the patch value contains both requests and limits
			if resourcesMap, ok := patch.Value.(map[string]interface{}); ok {
				hasRequests := false
				hasLimits := false

				if requests, ok := resourcesMap["requests"].(map[string]interface{}); ok && len(requests) > 0 {
					hasRequests = true
				}
				if limits, ok := resourcesMap["limits"].(map[string]interface{}); ok && len(limits) > 0 {
					hasLimits = true
				}

				g.Expect(hasRequests).Should(BeTrue(), "Resources patch should contain requests")
				g.Expect(hasLimits).Should(BeTrue(), "Resources patch should contain limits")
			}
			break
		}
	}

	g.Expect(hasResourcesPatch).Should(BeTrue(), "Should have resources patch")
}

// - InferenceService

// TestHardwareProfile_ResourceInjection_InferenceService tests that hardware profiles with resource requirements are applied correctly to InferenceService workloads.
func TestHardwareProfile_ResourceInjection_InferenceService(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create hardware profile with CPU and memory identifiers (including limits)
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithCPUIdentifier("0", "4", "8"),         // min: 0, default: 4, max: 8
		envtestutil.WithMemoryIdentifier("0", "8Gi", "16Gi"), // min: 0, default: 8Gi, max: 16Gi
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	testCases := []struct {
		name                string
		setupWorkload       func() *unstructured.Unstructured
		expectResourcePatch bool
	}{
		{
			name: "applies resources when none exist",
			setupWorkload: func() *unstructured.Unstructured {
				workload := envtestutil.NewInferenceService(testInferenceService, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
				workloadUnstructured, ok := workload.(*unstructured.Unstructured)
				if !ok {
					return nil
				}
				return workloadUnstructured
			},
			expectResourcePatch: true,
		},
		{
			name: "preserves existing resources",
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewInferenceService(testInferenceService, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")

				// Set existing resources that should be preserved
				model, _, _ := unstructured.NestedMap(workload.Object, "spec", "predictor", "model")
				if model == nil {
					model = make(map[string]interface{})
				}
				model["resources"] = map[string]interface{}{
					"requests": map[string]interface{}{
						"cpu":    "2",
						"memory": "4Gi",
					},
					"limits": map[string]interface{}{
						"cpu":    "4",
						"memory": "8Gi",
					},
				}
				_ = unstructured.SetNestedMap(workload.Object, model, "spec", "predictor", "model")
				return workload
			},
			expectResourcePatch: false,
		},
		{
			name: "applies only missing resources when model has partial resources",
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewInferenceService(testInferenceService, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")

				// Set only CPU request, memory should be applied from HWP
				model, _, _ := unstructured.NestedMap(workload.Object, "spec", "predictor", "model")
				if model == nil {
					model = make(map[string]interface{})
				}
				model["resources"] = map[string]interface{}{
					"requests": map[string]interface{}{
						"cpu": "1",
					},
				}
				_ = unstructured.SetNestedMap(workload.Object, model, "spec", "predictor", "model")
				return workload
			},
			expectResourcePatch: true,
		},
		{
			name: "applies resources to model without them",
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewInferenceService(testInferenceService, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")

				// For InferenceServices, work with the model object - set partial resources
				model := map[string]interface{}{
					"name":  "test-model",
					"image": "tensorflow/serving:latest",
					"resources": map[string]interface{}{
						"requests": map[string]interface{}{
							"cpu": "1",
							// Missing memory - should get HWP memory
						},
					},
				}
				_ = unstructured.SetNestedMap(workload.Object, model, "spec", "predictor", "model")
				return workload
			},
			expectResourcePatch: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			workload := tc.setupWorkload()

			req := envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				workload,
				gvk.InferenceServices,
				metav1.GroupVersionResource{
					Group:    gvk.InferenceServices.Group,
					Version:  gvk.InferenceServices.Version,
					Resource: "inferenceservices",
				},
			)

			resp := injector.Handle(ctx, req)
			g.Expect(resp.Allowed).Should(BeTrue())
			g.Expect(hasResourcePatches(resp.Patches)).Should(Equal(tc.expectResourcePatch))
		})
	}
}

// TestHardwareProfile_SchedulingConfiguration_InferenceService tests that hardware profiles with different
// scheduling configurations are applied correctly to InferenceService workloads.
func TestHardwareProfile_SchedulingConfiguration_InferenceService(t *testing.T) { //nolint:dupl
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	testCases := []struct {
		name          string
		hwpOptions    []envtestutil.ObjectOption
		setupWorkload func() *unstructured.Unstructured
		expectPatches bool
	}{
		{
			name: "applies node scheduling to clean workload",
			hwpOptions: []envtestutil.ObjectOption{
				envtestutil.WithCPUIdentifier("2", "2"),
				envtestutil.WithNodeScheduling(
					map[string]string{"node-type": "gpu-node"},
					[]corev1.Toleration{{
						Key:      "nvidia.com/gpu",
						Operator: corev1.TolerationOpEqual,
						Value:    "true",
						Effect:   corev1.TaintEffectNoSchedule,
					}},
				),
			},
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewInferenceService(testInferenceService, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")
				return workload
			},
			expectPatches: true,
		},
		{
			name: "applies kueue scheduling to clean workload",
			hwpOptions: []envtestutil.ObjectOption{
				envtestutil.WithMemoryIdentifier("4Gi", "4Gi"),
				envtestutil.WithKueueScheduling(testQueue, "high-priority"),
			},
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewInferenceService(testInferenceService, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")
				return workload
			},
			expectPatches: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace, tc.hwpOptions...)
			cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
			injector := createWebhookInjector(cli, sch)

			workload := tc.setupWorkload()

			req := envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				workload,
				gvk.InferenceServices,
				metav1.GroupVersionResource{
					Group:    gvk.InferenceServices.Group,
					Version:  gvk.InferenceServices.Version,
					Resource: "inferenceservices",
				},
			)

			resp := injector.Handle(ctx, req)
			g.Expect(resp.Allowed).Should(BeTrue())
			g.Expect(resp.Patches).Should(Not(BeEmpty()))
		})
	}
}

// TestHardwareProfile_SupportsCrossNamespaceAccess_InferenceService tests that hardware profiles can be accessed from different namespaces for InferenceService workloads.
func TestHardwareProfile_SupportsCrossNamespaceAccess_InferenceService(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create hardware profile in different namespace
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, hwpNamespace,
		envtestutil.WithGPUIdentifier("nvidia.com/gpu", "1", "1"),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	// InferenceService uses a different pattern for cross-namespace annotation
	workload := envtestutil.NewInferenceService(testInferenceService, testNamespace, func(obj client.Object) {
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[hardwareprofile.HardwareProfileNameAnnotation] = testHardwareProfile
		annotations[hardwareprofile.HardwareProfileNamespaceAnnotation] = hwpNamespace
		obj.SetAnnotations(annotations)
	})

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		workload,
		gvk.InferenceServices,
		metav1.GroupVersionResource{
			Group:    gvk.InferenceServices.Group,
			Version:  gvk.InferenceServices.Version,
			Resource: "inferenceservices",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())
	g.Expect(resp.Patches).Should(Not(BeEmpty()))
}

// TestHardwareProfile_ResourceLimits_InferenceService tests that hardware profiles with MaxCount are applied as limits for InferenceService workloads.
func TestHardwareProfile_ResourceLimits_InferenceService(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create hardware profile with CPU and memory identifiers without limits
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithCPUIdentifier("1", "2"),                // min: 1, default: 2 (no max)
		envtestutil.WithMemoryIdentifier("1Gi", "2Gi"),         // min: 1Gi, default: 2Gi (no max)
		envtestutil.WithGPUIdentifier("adm.com/gpu", "0", "1"), // min: 0, default: 1 (no max)
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	workload := envtestutil.NewInferenceService(testInferenceService, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
	workloadUnstructured, ok := workload.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue(), "workload should be unstructured")

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		workloadUnstructured,
		gvk.InferenceServices,
		metav1.GroupVersionResource{
			Group:    gvk.InferenceServices.Group,
			Version:  gvk.InferenceServices.Version,
			Resource: "inferenceservices",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())
	g.Expect(resp.Patches).Should(Not(BeEmpty()))

	// Verify that resources were applied
	hasResourcesPatch := false
	for _, patch := range resp.Patches {
		if strings.Contains(patch.Path, "/resources") {
			hasResourcesPatch = true

			// Check if the patch value contains both requests and limits
			if resourcesMap, ok := patch.Value.(map[string]interface{}); ok {
				hasRequests := false
				hasLimits := false

				if requests, ok := resourcesMap["requests"].(map[string]interface{}); ok && len(requests) > 0 {
					hasRequests = true
				}
				if limits, ok := resourcesMap["limits"].(map[string]interface{}); ok && len(limits) > 0 {
					hasLimits = true
				}

				g.Expect(hasRequests).Should(BeTrue(), "Resources patch should contain requests")
				g.Expect(hasLimits).Should(BeFalse(), "Resources patch should not contain limits when max values are not set")
			}
			break
		}
	}

	g.Expect(hasResourcesPatch).Should(BeTrue(), "Should have resources patch")
}

// - LLMInferenceService
// TestHardwareProfile_ResourceInjection_LLMInferenceService tests that hardware profiles with resource requirements are applied correctly to LlmInferenceService workloads.
func TestHardwareProfile_ResourceInjection_LLMInferenceService(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create hardware profile with CPU and memory identifiers (including limits)
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithCPUIdentifier("0", "4", "8"),         // min: 0, default: 4, max: 8
		envtestutil.WithMemoryIdentifier("0", "8Gi", "16Gi"), // min: 0, default: 8Gi, max: 16Gi
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	testCases := []struct {
		name                string
		setupWorkload       func() *unstructured.Unstructured
		expectResourcePatch bool
	}{
		{
			name: "applies resources when none exist in .spec.template.containers",
			setupWorkload: func() *unstructured.Unstructured {
				workload := envtestutil.NewLLMInferenceService(testLLMInferenceService, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
				workloadUnstructured, ok := workload.(*unstructured.Unstructured)
				if !ok {
					return nil
				}
				return workloadUnstructured
			},
			expectResourcePatch: true,
		},
		{
			name: "preserves existing resources in .spec.template.containers",
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewLLMInferenceService(testLLMInferenceService, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")

				// Set existing resources that should be preserved
				containers := []interface{}{
					map[string]interface{}{
						"name":  "llm-container",
						"image": "opendatahub/llm-model-server:latest",
						"resources": map[string]interface{}{
							"requests": map[string]interface{}{
								"cpu":    "2",
								"memory": "4Gi",
							},
							"limits": map[string]interface{}{
								"cpu":    "4",
								"memory": "8Gi",
							},
						},
					},
				}
				_ = unstructured.SetNestedSlice(workload.Object, containers, "spec", "template", "containers")
				return workload
			},
			expectResourcePatch: false,
		},
		{
			name: "applies only missing resources when .spec.template.containers has partial resources",
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewLLMInferenceService(testLLMInferenceService, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")

				// Set only CPU request, memory should be applied from HWP
				containers := []interface{}{
					map[string]interface{}{
						"name":  "llm-container",
						"image": "opendatahub/llm-model-server:latest",
						"resources": map[string]interface{}{
							"requests": map[string]interface{}{
								"cpu": "1",
							},
						},
					},
				}
				_ = unstructured.SetNestedSlice(workload.Object, containers, "spec", "template", "containers")
				return workload
			},
			expectResourcePatch: true,
		},
		{
			name: "applies resources to .spec.template.containers without them",
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewLLMInferenceService(testLLMInferenceService, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")

				containers := []interface{}{
					map[string]interface{}{
						"name":  "llm-container",
						"image": "opendatahub/llm-model-server:latest",
						"resources": map[string]interface{}{
							"requests": map[string]interface{}{
								"cpu": "1",
								// Missing memory - should get HWProfile memory
							},
						},
					},
				}
				_ = unstructured.SetNestedSlice(workload.Object, containers, "spec", "template", "containers")
				return workload
			},
			expectResourcePatch: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			workload := tc.setupWorkload()

			req := envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				workload,
				gvk.LLMInferenceServiceV1Alpha1,
				metav1.GroupVersionResource{
					Group:    gvk.LLMInferenceServiceV1Alpha1.Group,
					Version:  gvk.LLMInferenceServiceV1Alpha1.Version,
					Resource: "llminferenceservices",
				},
			)

			resp := injector.Handle(ctx, req)
			g.Expect(resp.Allowed).Should(BeTrue())

			g.Expect(hasResourcePatches(resp.Patches)).Should(Equal(tc.expectResourcePatch))
		})
	}
}

// TestHardwareProfile_CreatesContainerStructure_LLMInferenceService tests is specical for LLMInferenceService if user set .spec:{}.
func TestHardwareProfile_CreatesContainerStructure_LLMInferenceService(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create hardware profile with CPU and memory identifiers (including limits)
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithCPUIdentifier("0", "4", "8"),         // min: 0, default: 4, max: 8
		envtestutil.WithMemoryIdentifier("0", "8Gi", "16Gi"), // min: 0, default: 8Gi, max: 16Gi
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	// Create LLMInferenceService with empty spec: {}
	workload := &unstructured.Unstructured{}
	workload.SetAPIVersion(gvk.LLMInferenceServiceV1Alpha1.GroupVersion().String())
	workload.SetKind(gvk.LLMInferenceServiceV1Alpha1.Kind)
	workload.SetName(testLLMInferenceService)
	workload.SetNamespace(testNamespace)
	workload.SetAnnotations(map[string]string{
		hardwareprofile.HardwareProfileNameAnnotation: testHardwareProfile,
	})
	// Set spec: {}
	workload.Object["spec"] = map[string]interface{}{}

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		workload,
		gvk.LLMInferenceServiceV1Alpha1,
		metav1.GroupVersionResource{
			Group:    gvk.LLMInferenceServiceV1Alpha1.Group,
			Version:  gvk.LLMInferenceServiceV1Alpha1.Version,
			Resource: "llminferenceservices",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())

	// Verify that we have patches that create the container structure and inject resources
	hasContainerPatches := false
	hasResourcePatches := false
	for _, patch := range resp.Patches {
		if strings.Contains(patch.Path, "/spec/template") {
			if patchValue, ok := patch.Value.(map[string]interface{}); ok {
				if containers, ok := patchValue["containers"].([]interface{}); ok && len(containers) > 0 {
					if container, ok := containers[0].(map[string]interface{}); ok {
						// Verify the container has the expected name "main"
						if name, ok := container["name"].(string); ok && name == "main" {
							hasContainerPatches = true
						}
						if _, hasResources := container["resources"]; hasResources {
							hasResourcePatches = true
						}
					}
				}
			}
		}
	}
	g.Expect(hasContainerPatches).Should(BeTrue(), "should create container default name as main")
	g.Expect(hasResourcePatches).Should(BeTrue(), "should inject resources into created container")
}

// TestHardwareProfile_SchedulingConfiguration_LLMInferenceService tests that hardware profiles with different
// scheduling configurations are applied correctly to LlmInferenceService workloads.
func TestHardwareProfile_SchedulingConfiguration_LLMInferenceService(t *testing.T) { //nolint:dupl
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	testCases := []struct {
		name          string
		hwpOptions    []envtestutil.ObjectOption
		setupWorkload func() *unstructured.Unstructured
		expectPatches bool
	}{
		{
			name: "applies node scheduling to clean workload",
			hwpOptions: []envtestutil.ObjectOption{
				envtestutil.WithCPUIdentifier("2", "2"),
				envtestutil.WithNodeScheduling(
					map[string]string{"node-type": "gpu-node"},
					[]corev1.Toleration{{
						Key:      "nvidia.com/gpu",
						Operator: corev1.TolerationOpEqual,
						Value:    "true",
						Effect:   corev1.TaintEffectNoSchedule,
					}},
				),
			},
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewLLMInferenceService(testLLMInferenceService, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")
				return workload
			},
			expectPatches: true,
		},
		{
			name: "applies kueue scheduling to clean workload",
			hwpOptions: []envtestutil.ObjectOption{
				envtestutil.WithMemoryIdentifier("4Gi", "4Gi"),
				envtestutil.WithKueueScheduling(testQueue, "high-priority"),
			},
			setupWorkload: func() *unstructured.Unstructured {
				workload, ok := envtestutil.NewLLMInferenceService(testLLMInferenceService, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
				g.Expect(ok).Should(BeTrue(), "workload should be unstructured")
				return workload
			},
			expectPatches: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace, tc.hwpOptions...)
			cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
			injector := createWebhookInjector(cli, sch)

			workload := tc.setupWorkload()

			req := envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				workload,
				gvk.LLMInferenceServiceV1Alpha1,
				metav1.GroupVersionResource{
					Group:    gvk.LLMInferenceServiceV1Alpha1.Group,
					Version:  gvk.LLMInferenceServiceV1Alpha1.Version,
					Resource: "llminferenceservices",
				},
			)

			resp := injector.Handle(ctx, req)
			g.Expect(resp.Allowed).Should(BeTrue())
			g.Expect(resp.Patches).Should(Not(BeEmpty()))
		})
	}
}

// TestHardwareProfile_SupportsCrossNamespaceAccess_LLMInferenceService tests that hardware profiles can be accessed from different namespaces for LlmInferenceService workloads.
func TestHardwareProfile_SupportsCrossNamespaceAccess_LLMInferenceService(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create hardware profile in different namespace
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, hwpNamespace,
		envtestutil.WithGPUIdentifier("nvidia.com/gpu", "1", "1"),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	// InferenceService uses a different pattern for cross-namespace annotation
	workload := envtestutil.NewLLMInferenceService(testLLMInferenceService, testNamespace, func(obj client.Object) {
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[hardwareprofile.HardwareProfileNameAnnotation] = testHardwareProfile
		annotations[hardwareprofile.HardwareProfileNamespaceAnnotation] = hwpNamespace
		obj.SetAnnotations(annotations)
	})

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		workload,
		gvk.LLMInferenceServiceV1Alpha1,
		metav1.GroupVersionResource{
			Group:    gvk.LLMInferenceServiceV1Alpha1.Group,
			Version:  gvk.LLMInferenceServiceV1Alpha1.Version,
			Resource: "llminferenceservices",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())
	g.Expect(resp.Patches).Should(Not(BeEmpty()))
}

// TestHardwareProfile_ResourceLimits_LLMInferenceService tests that hardware profiles with MaxCount are applied as limits for LlmInferenceService workloads.
func TestHardwareProfile_ResourceLimits_LLMInferenceService(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create hardware profile with CPU and memory identifiers without limits
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithCPUIdentifier("1", "2"),                // min: 1, default: 2 (no max)
		envtestutil.WithMemoryIdentifier("1Gi", "2Gi"),         // min: 1Gi, default: 2Gi (no max)
		envtestutil.WithGPUIdentifier("adm.com/gpu", "0", "1"), // min: 0, default: 1 (no max)
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	workload := envtestutil.NewLLMInferenceService(testLLMInferenceService, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
	workloadUnstructured, ok := workload.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue(), "workload should be unstructured")

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		workloadUnstructured,
		gvk.LLMInferenceServiceV1Alpha1,
		metav1.GroupVersionResource{
			Group:    gvk.LLMInferenceServiceV1Alpha1.Group,
			Version:  gvk.LLMInferenceServiceV1Alpha1.Version,
			Resource: "llminferenceservices",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())
	g.Expect(resp.Patches).Should(Not(BeEmpty()))

	// Verify that resources were applied
	hasResourcesPatch := false
	for _, patch := range resp.Patches {
		if strings.Contains(patch.Path, "/resources") {
			hasResourcesPatch = true

			// Check if the patch value contains both requests and limits
			if resourcesMap, ok := patch.Value.(map[string]interface{}); ok {
				hasRequests := false
				hasLimits := false

				if requests, ok := resourcesMap["requests"].(map[string]interface{}); ok && len(requests) > 0 {
					hasRequests = true
				}
				if limits, ok := resourcesMap["limits"].(map[string]interface{}); ok && len(limits) > 0 {
					hasLimits = true
				}

				g.Expect(hasRequests).Should(BeTrue(), "Resources patch should contain requests")
				g.Expect(hasLimits).Should(BeFalse(), "Resources patch should not contain limits when max values are not set")
			}
			break
		}
	}

	g.Expect(hasResourcesPatch).Should(BeTrue(), "Should have resources patch")
}
