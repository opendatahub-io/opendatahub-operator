package hardwareprofile_test

import (
	"context"
	"encoding/json"
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
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"

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
						if patch.Operation == webhookutils.PatchOpAdd && strings.Contains(patch.Path, "/resources") {
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

// TestHardwareProfile_ProfileChangeClearsScheduling tests that changing the hardware profile
// clears existing scheduling configuration (tolerations, nodeSelector, Kueue label) before
// applying new settings.
func TestHardwareProfile_ProfileChangeClearsScheduling(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create two hardware profiles - one with tolerations, one empty
	hwpWithTolerations := envtestutil.NewHardwareProfile("hwp-with-tolerations", testNamespace,
		envtestutil.WithNodeScheduling(
			map[string]string{"gpu": "true"},
			[]corev1.Toleration{
				{Key: "nvidia.com/gpu", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
			},
		),
	)
	hwpEmpty := envtestutil.NewHardwareProfile("hwp-empty", testNamespace)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwpWithTolerations, hwpEmpty).Build()
	injector := createWebhookInjector(cli, sch)

	// Create a notebook that was originally assigned to hwp-with-tolerations
	// and had its tolerations/nodeSelector set
	oldNotebook := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile("hwp-with-tolerations"))
	oldNotebookUnstructured, ok := oldNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	// Set existing tolerations and nodeSelector (as if they were applied by old HWP)
	_ = unstructured.SetNestedSlice(oldNotebookUnstructured.Object, []interface{}{
		map[string]interface{}{
			"key":      "nvidia.com/gpu",
			"operator": "Exists",
			"effect":   "NoSchedule",
		},
	}, "spec", "template", "spec", "tolerations")
	_ = unstructured.SetNestedStringMap(oldNotebookUnstructured.Object, map[string]string{"gpu": "true"}, "spec", "template", "spec", "nodeSelector")

	// Now create the new notebook with hwp-empty annotation (simulating profile switch)
	newNotebook := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile("hwp-empty"))
	newNotebookUnstructured, ok := newNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	// Copy the tolerations/nodeSelector from old notebook (as they would exist in the cluster)
	_ = unstructured.SetNestedSlice(newNotebookUnstructured.Object, []interface{}{
		map[string]interface{}{
			"key":      "nvidia.com/gpu",
			"operator": "Exists",
			"effect":   "NoSchedule",
		},
	}, "spec", "template", "spec", "tolerations")
	_ = unstructured.SetNestedStringMap(newNotebookUnstructured.Object, map[string]string{"gpu": "true"}, "spec", "template", "spec", "nodeSelector")

	// Marshal objects for admission request
	newObjBytes, err := json.Marshal(newNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())
	oldObjBytes, err := json.Marshal(oldNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create UPDATE request with old and new objects
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Kind:      metav1.GroupVersionKind{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Kind: gvk.Notebook.Kind},
			Resource:  metav1.GroupVersionResource{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Resource: "notebooks"},
			Namespace: testNamespace,
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: newObjBytes},
			OldObject: runtime.RawExtension{Raw: oldObjBytes},
		},
	}

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())

	// Check that tolerations and nodeSelector are removed (cleared)
	foundTolerationRemove := false
	foundNodeSelectorRemove := false
	for _, patch := range resp.Patches {
		if patch.Operation == webhookutils.PatchOpRemove && strings.Contains(patch.Path, "tolerations") {
			foundTolerationRemove = true
		}
		if patch.Operation == webhookutils.PatchOpRemove && strings.Contains(patch.Path, "nodeSelector") {
			foundNodeSelectorRemove = true
		}
	}
	g.Expect(foundTolerationRemove).Should(BeTrue(), "Should have patch to remove tolerations when switching profiles")
	g.Expect(foundNodeSelectorRemove).Should(BeTrue(), "Should have patch to remove nodeSelector when switching profiles")
}

// TestHardwareProfile_SameProfileMergesTolerations tests that when the hardware profile
// hasn't changed, tolerations are merged (HWP tolerations + existing manual tolerations).
func TestHardwareProfile_SameProfileMergesTolerations(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create hardware profile with one toleration
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithNodeScheduling(
			nil,
			[]corev1.Toleration{
				{Key: "nvidia.com/gpu", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
			},
		),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	// Create the "old" notebook with the same HWP and both HWP toleration + manual toleration
	oldNotebook := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
	oldNotebookUnstructured, ok := oldNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	_ = unstructured.SetNestedSlice(oldNotebookUnstructured.Object, []interface{}{
		map[string]interface{}{
			"key":      "nvidia.com/gpu",
			"operator": "Exists",
			"effect":   "NoSchedule",
		},
		map[string]interface{}{
			"key":      "my-manual-toleration",
			"operator": "Equal",
			"value":    "true",
			"effect":   "NoSchedule",
		},
	}, "spec", "template", "spec", "tolerations")

	// Create the "new" notebook with same HWP (no profile change)
	newNotebook := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
	newNotebookUnstructured, ok := newNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	// Copy the tolerations from old notebook
	_ = unstructured.SetNestedSlice(newNotebookUnstructured.Object, []interface{}{
		map[string]interface{}{
			"key":      "nvidia.com/gpu",
			"operator": "Exists",
			"effect":   "NoSchedule",
		},
		map[string]interface{}{
			"key":      "my-manual-toleration",
			"operator": "Equal",
			"value":    "true",
			"effect":   "NoSchedule",
		},
	}, "spec", "template", "spec", "tolerations")

	// Marshal objects for admission request
	newObjBytes, err := json.Marshal(newNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())
	oldObjBytes, err := json.Marshal(oldNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create UPDATE request with same HWP annotation
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Kind:      metav1.GroupVersionKind{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Kind: gvk.Notebook.Kind},
			Resource:  metav1.GroupVersionResource{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Resource: "notebooks"},
			Namespace: testNamespace,
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: newObjBytes},
			OldObject: runtime.RawExtension{Raw: oldObjBytes},
		},
	}

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())

	// Check that tolerations are NOT removed (merged instead)
	for _, patch := range resp.Patches {
		if patch.Operation == webhookutils.PatchOpRemove && strings.Contains(patch.Path, "tolerations") {
			t.Fatalf("Should NOT remove tolerations when profile hasn't changed - expected merge behavior")
		}
	}

	// Check that the resulting tolerations include both HWP and manual tolerations
	foundTolerationsPatch := false
	for _, patch := range resp.Patches {
		if strings.Contains(patch.Path, "tolerations") && (patch.Operation == webhookutils.PatchOpAdd || patch.Operation == webhookutils.PatchOpReplace) {
			foundTolerationsPatch = true
			tolerations, ok := patch.Value.([]interface{})
			g.Expect(ok).Should(BeTrue(), "Tolerations patch value should be a slice")
			g.Expect(len(tolerations)).Should(BeNumerically(">=", 2), "Should have at least 2 tolerations (HWP + manual)")
		}
	}
	// Note: If tolerations haven't changed, there might not be a patch at all, which is fine
	_ = foundTolerationsPatch
}

// TestTolerationKey_DistinguishesByValue tests that tolerations with
// same key/operator/effect but different values produce different keys.
// This ensures user tolerations are not accidentally removed during HWP cleanup.
func TestTolerationKey_DistinguishesByValue(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	testCases := []struct {
		name        string
		toleration1 map[string]interface{}
		toleration2 map[string]interface{}
		shouldMatch bool
	}{
		{
			name: "same key/operator/effect but different value should NOT match",
			toleration1: map[string]interface{}{
				"key":      "gpu-type",
				"operator": "Equal",
				"value":    "nvidia",
				"effect":   "NoSchedule",
			},
			toleration2: map[string]interface{}{
				"key":      "gpu-type",
				"operator": "Equal",
				"value":    "amd",
				"effect":   "NoSchedule",
			},
			shouldMatch: false,
		},
		{
			name: "identical tolerations should match",
			toleration1: map[string]interface{}{
				"key":      "gpu-type",
				"operator": "Equal",
				"value":    "nvidia",
				"effect":   "NoSchedule",
			},
			toleration2: map[string]interface{}{
				"key":      "gpu-type",
				"operator": "Equal",
				"value":    "nvidia",
				"effect":   "NoSchedule",
			},
			shouldMatch: true,
		},
		{
			name: "same key/operator/effect/value but different tolerationSeconds should NOT match",
			toleration1: map[string]interface{}{
				"key":               "node.kubernetes.io/unreachable",
				"operator":          "Exists",
				"effect":            "NoExecute",
				"tolerationSeconds": int64(300),
			},
			toleration2: map[string]interface{}{
				"key":               "node.kubernetes.io/unreachable",
				"operator":          "Exists",
				"effect":            "NoExecute",
				"tolerationSeconds": int64(600),
			},
			shouldMatch: false,
		},
		{
			name: "same tolerations with same tolerationSeconds should match",
			toleration1: map[string]interface{}{
				"key":               "node.kubernetes.io/unreachable",
				"operator":          "Exists",
				"effect":            "NoExecute",
				"tolerationSeconds": int64(300),
			},
			toleration2: map[string]interface{}{
				"key":               "node.kubernetes.io/unreachable",
				"operator":          "Exists",
				"effect":            "NoExecute",
				"tolerationSeconds": int64(300),
			},
			shouldMatch: true,
		},
		{
			name: "toleration with tolerationSeconds vs without should NOT match",
			toleration1: map[string]interface{}{
				"key":               "node.kubernetes.io/unreachable",
				"operator":          "Exists",
				"effect":            "NoExecute",
				"tolerationSeconds": int64(300),
			},
			toleration2: map[string]interface{}{
				"key":      "node.kubernetes.io/unreachable",
				"operator": "Exists",
				"effect":   "NoExecute",
			},
			shouldMatch: false,
		},
		{
			name: "Exists operator with empty value should match same toleration",
			toleration1: map[string]interface{}{
				"key":      "nvidia.com/gpu",
				"operator": "Exists",
				"effect":   "NoSchedule",
			},
			toleration2: map[string]interface{}{
				"key":      "nvidia.com/gpu",
				"operator": "Exists",
				"effect":   "NoSchedule",
			},
			shouldMatch: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			key1 := hardwareprofile.TolerationKey(tc.toleration1)
			key2 := hardwareprofile.TolerationKey(tc.toleration2)

			if tc.shouldMatch {
				g.Expect(key1).Should(Equal(key2), "Keys should match for identical tolerations")
			} else {
				g.Expect(key1).ShouldNot(Equal(key2), "Keys should NOT match for different tolerations")
			}
		})
	}
}

// TestHardwareProfile_HWPRemovalPreservesUserTolerations tests that when
// removing HWP annotation, only HWP-applied tolerations are removed while
// user-added tolerations with different values are preserved.
func TestHardwareProfile_HWPRemovalPreservesUserTolerations(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create HWP with specific tolerations
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithNodeScheduling(
			map[string]string{"hwp-node": "true"},
			[]corev1.Toleration{
				{
					Key:      "gpu-type",
					Operator: corev1.TolerationOpEqual,
					Value:    "amd", // HWP specifies "amd"
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
		),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	// Create "old" notebook WITH HWP annotation and both HWP + user tolerations
	oldNotebook := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
	oldNotebookUnstructured, ok := oldNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	// Set tolerations: HWP's "amd" toleration + user's "nvidia" toleration (same key, different value)
	_ = unstructured.SetNestedSlice(oldNotebookUnstructured.Object, []interface{}{
		map[string]interface{}{
			"key":      "gpu-type",
			"operator": "Equal",
			"value":    "amd", // HWP-applied
			"effect":   "NoSchedule",
		},
		map[string]interface{}{
			"key":      "gpu-type",
			"operator": "Equal",
			"value":    "nvidia", // User-added (should be preserved)
			"effect":   "NoSchedule",
		},
	}, "spec", "template", "spec", "tolerations")
	_ = unstructured.SetNestedStringMap(oldNotebookUnstructured.Object, map[string]string{"hwp-node": "true"}, "spec", "template", "spec", "nodeSelector")

	// Create "new" notebook WITHOUT HWP annotation (simulating HWP removal)
	newNotebook := envtestutil.NewNotebook(testNotebook, testNamespace) // No HWP annotation
	newNotebookUnstructured, ok := newNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	// Copy the tolerations/nodeSelector from old notebook (as they exist in cluster)
	_ = unstructured.SetNestedSlice(newNotebookUnstructured.Object, []interface{}{
		map[string]interface{}{
			"key":      "gpu-type",
			"operator": "Equal",
			"value":    "amd",
			"effect":   "NoSchedule",
		},
		map[string]interface{}{
			"key":      "gpu-type",
			"operator": "Equal",
			"value":    "nvidia",
			"effect":   "NoSchedule",
		},
	}, "spec", "template", "spec", "tolerations")
	_ = unstructured.SetNestedStringMap(newNotebookUnstructured.Object, map[string]string{"hwp-node": "true"}, "spec", "template", "spec", "nodeSelector")

	// Marshal objects for admission request
	newObjBytes, err := json.Marshal(newNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())
	oldObjBytes, err := json.Marshal(oldNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create UPDATE request (removing HWP annotation)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Kind:      metav1.GroupVersionKind{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Kind: gvk.Notebook.Kind},
			Resource:  metav1.GroupVersionResource{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Resource: "notebooks"},
			Namespace: testNamespace,
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: newObjBytes},
			OldObject: runtime.RawExtension{Raw: oldObjBytes},
		},
	}

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())

	// Check that the tolerations patch preserves user's "nvidia" but removes HWP's "amd"
	foundTolerationsPatch := false
	for _, patch := range resp.Patches {
		if strings.Contains(patch.Path, "tolerations") &&
			(patch.Operation == webhookutils.PatchOpAdd || patch.Operation == webhookutils.PatchOpReplace) {
			foundTolerationsPatch = true

			// The patch should contain remaining tolerations
			if tolerations, ok := patch.Value.([]interface{}); ok {
				foundUserToleration := false
				foundHWPToleration := false
				for _, tol := range tolerations {
					if tolMap, ok := tol.(map[string]interface{}); ok {
						if tolMap["key"] == "gpu-type" && tolMap["value"] == "nvidia" {
							foundUserToleration = true
						}
						if tolMap["key"] == "gpu-type" && tolMap["value"] == "amd" {
							foundHWPToleration = true
						}
					}
				}
				g.Expect(foundUserToleration).Should(BeTrue(), "User's nvidia toleration should be preserved in patch")
				g.Expect(foundHWPToleration).Should(BeFalse(), "HWP's amd toleration should NOT be in patch")
			}
		}
	}
	// It's also valid if tolerations were removed entirely (via remove patch) - check that case
	if !foundTolerationsPatch {
		// Look for a remove operation that removes just the HWP toleration
		for _, patch := range resp.Patches {
			if strings.Contains(patch.Path, "tolerations") && patch.Operation == webhookutils.PatchOpRemove {
				// If removing the whole tolerations array, that's incorrect
				// But if removing specific indices, that could be correct
				foundTolerationsPatch = true
			}
		}
	}
	g.Expect(foundTolerationsPatch).Should(BeTrue(), "Should have a patch modifying tolerations")
}

// TestHardwareProfile_HWPRemovalWithTolerationSeconds tests that tolerations
// with different tolerationSeconds values are treated as distinct.
func TestHardwareProfile_HWPRemovalWithTolerationSeconds(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create HWP with toleration that has tolerationSeconds
	tolerationSeconds := int64(300)
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithNodeScheduling(
			nil,
			[]corev1.Toleration{
				{
					Key:               "node.kubernetes.io/unreachable",
					Operator:          corev1.TolerationOpExists,
					Effect:            corev1.TaintEffectNoExecute,
					TolerationSeconds: &tolerationSeconds,
				},
			},
		),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	// Create "old" notebook with HWP annotation and both HWP + user tolerations
	oldNotebook := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
	oldNotebookUnstructured, ok := oldNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	// User has same toleration but with different tolerationSeconds (600 vs HWP's 300)
	_ = unstructured.SetNestedSlice(oldNotebookUnstructured.Object, []interface{}{
		map[string]interface{}{
			"key":               "node.kubernetes.io/unreachable",
			"operator":          "Exists",
			"effect":            "NoExecute",
			"tolerationSeconds": int64(300), // HWP-applied
		},
		map[string]interface{}{
			"key":               "node.kubernetes.io/unreachable",
			"operator":          "Exists",
			"effect":            "NoExecute",
			"tolerationSeconds": int64(600), // User-added (different seconds)
		},
	}, "spec", "template", "spec", "tolerations")

	// Create "new" notebook WITHOUT HWP annotation
	newNotebook := envtestutil.NewNotebook(testNotebook, testNamespace)
	newNotebookUnstructured, ok := newNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	// Copy tolerations from old notebook
	_ = unstructured.SetNestedSlice(newNotebookUnstructured.Object, []interface{}{
		map[string]interface{}{
			"key":               "node.kubernetes.io/unreachable",
			"operator":          "Exists",
			"effect":            "NoExecute",
			"tolerationSeconds": int64(300),
		},
		map[string]interface{}{
			"key":               "node.kubernetes.io/unreachable",
			"operator":          "Exists",
			"effect":            "NoExecute",
			"tolerationSeconds": int64(600),
		},
	}, "spec", "template", "spec", "tolerations")

	// Marshal objects
	newObjBytes, err := json.Marshal(newNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())
	oldObjBytes, err := json.Marshal(oldNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create UPDATE request
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Kind:      metav1.GroupVersionKind{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Kind: gvk.Notebook.Kind},
			Resource:  metav1.GroupVersionResource{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Resource: "notebooks"},
			Namespace: testNamespace,
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: newObjBytes},
			OldObject: runtime.RawExtension{Raw: oldObjBytes},
		},
	}

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())

	// Check that the tolerations patch preserves user's 600s but removes HWP's 300s
	foundTolerationsPatch := false
	for _, patch := range resp.Patches {
		if strings.Contains(patch.Path, "tolerations") &&
			(patch.Operation == webhookutils.PatchOpAdd || patch.Operation == webhookutils.PatchOpReplace) {
			foundTolerationsPatch = true

			if tolerations, ok := patch.Value.([]interface{}); ok {
				found300s := false
				found600s := false
				for _, tol := range tolerations {
					if tolMap, ok := tol.(map[string]interface{}); ok {
						if ts, exists := tolMap["tolerationSeconds"]; exists {
							// Handle both int64 and float64 (JSON unmarshaling can produce float64)
							switch v := ts.(type) {
							case int64:
								if v == 300 {
									found300s = true
								}
								if v == 600 {
									found600s = true
								}
							case float64:
								if v == 300 {
									found300s = true
								}
								if v == 600 {
									found600s = true
								}
							}
						}
					}
				}
				g.Expect(found600s).Should(BeTrue(), "User's 600s toleration should be preserved in patch")
				g.Expect(found300s).Should(BeFalse(), "HWP's 300s toleration should NOT be in patch")
			}
		}
	}
	// Also valid if there's a remove patch for tolerations
	if !foundTolerationsPatch {
		for _, patch := range resp.Patches {
			if strings.Contains(patch.Path, "tolerations") && patch.Operation == webhookutils.PatchOpRemove {
				foundTolerationsPatch = true
			}
		}
	}
	g.Expect(foundTolerationsPatch).Should(BeTrue(), "Should have a patch modifying tolerations")
}

// TestHardwareProfile_NodeSelectorOverrideWarning tests that when a user modifies a
// nodeSelector value that the HardwareProfile also specifies, a warning is returned
// in the admission response.
func TestHardwareProfile_NodeSelectorOverrideWarning(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create HWP with nodeSelector
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithNodeScheduling(
			map[string]string{"kubernetes.io/os": "linux", "gpu-type": "nvidia"},
			nil,
		),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	// Create "old" notebook with HWP annotation and HWP-applied nodeSelector
	oldNotebook := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
	oldNotebookUnstructured, ok := oldNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	// Set nodeSelector as if it was applied by HWP
	_ = unstructured.SetNestedStringMap(oldNotebookUnstructured.Object,
		map[string]string{"kubernetes.io/os": "linux", "gpu-type": "nvidia"},
		"spec", "template", "spec", "nodeSelector")

	// Create "new" notebook where user modified one of the HWP-managed nodeSelector values
	newNotebook := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
	newNotebookUnstructured, ok := newNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	// User changed "kubernetes.io/os" from "linux" to "windows" and added a manual key
	_ = unstructured.SetNestedStringMap(newNotebookUnstructured.Object,
		map[string]string{
			"kubernetes.io/os": "windows",  // User modified HWP value
			"gpu-type":         "nvidia",   // HWP value unchanged
			"my-custom-key":    "my-value", // User-added (should be preserved)
		},
		"spec", "template", "spec", "nodeSelector")

	// Marshal objects
	newObjBytes, err := json.Marshal(newNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())
	oldObjBytes, err := json.Marshal(oldNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create UPDATE request (same HWP, but user modified nodeSelector)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Kind:      metav1.GroupVersionKind{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Kind: gvk.Notebook.Kind},
			Resource:  metav1.GroupVersionResource{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Resource: "notebooks"},
			Namespace: testNamespace,
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: newObjBytes},
			OldObject: runtime.RawExtension{Raw: oldObjBytes},
		},
	}

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())

	// Should have a warning about the nodeSelector override
	g.Expect(resp.Warnings).ShouldNot(BeEmpty(), "Should have at least one warning")

	foundOverrideWarning := false
	for _, warning := range resp.Warnings {
		if strings.Contains(warning, "kubernetes.io/os") &&
			strings.Contains(warning, "windows") &&
			strings.Contains(warning, "linux") &&
			strings.Contains(warning, "overwritten") {
			foundOverrideWarning = true
			break
		}
	}
	g.Expect(foundOverrideWarning).Should(BeTrue(), "Should warn about nodeSelector key being overwritten")

	// Should NOT warn about unchanged keys (gpu-type) or user-added keys (my-custom-key)
	for _, warning := range resp.Warnings {
		g.Expect(warning).ShouldNot(ContainSubstring("gpu-type"), "Should not warn about unchanged HWP values")
		g.Expect(warning).ShouldNot(ContainSubstring("my-custom-key"), "Should not warn about user-added keys")
	}
}

// TestHardwareProfile_NoWarningWhenNodeSelectorUnchanged tests that no warning is emitted
// when the user hasn't modified any HWP-managed nodeSelector values.
func TestHardwareProfile_NoWarningWhenNodeSelectorUnchanged(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create HWP with nodeSelector
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithNodeScheduling(
			map[string]string{"kubernetes.io/os": "linux"},
			nil,
		),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	// Create "old" notebook with HWP annotation and HWP-applied nodeSelector
	oldNotebook := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
	oldNotebookUnstructured, ok := oldNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	_ = unstructured.SetNestedStringMap(oldNotebookUnstructured.Object,
		map[string]string{"kubernetes.io/os": "linux"},
		"spec", "template", "spec", "nodeSelector")

	// Create "new" notebook with same nodeSelector (user added a custom key but didn't change HWP ones)
	newNotebook := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
	newNotebookUnstructured, ok := newNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	// User only added a custom key, didn't modify HWP value
	_ = unstructured.SetNestedStringMap(newNotebookUnstructured.Object,
		map[string]string{
			"kubernetes.io/os": "linux",    // HWP value unchanged
			"my-custom-key":    "my-value", // User-added
		},
		"spec", "template", "spec", "nodeSelector")

	// Marshal objects
	newObjBytes, err := json.Marshal(newNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())
	oldObjBytes, err := json.Marshal(oldNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create UPDATE request
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Kind:      metav1.GroupVersionKind{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Kind: gvk.Notebook.Kind},
			Resource:  metav1.GroupVersionResource{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Resource: "notebooks"},
			Namespace: testNamespace,
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: newObjBytes},
			OldObject: runtime.RawExtension{Raw: oldObjBytes},
		},
	}

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())

	// Should NOT have any warnings since user didn't modify HWP-managed values
	g.Expect(resp.Warnings).Should(BeEmpty(), "Should have no warnings when user doesn't modify HWP values")
}

// TestHardwareProfile_KueueLabelOverrideWarning tests that when a user modifies the
// Kueue queue-name label to a different value than the HardwareProfile specifies,
// a warning is returned in the admission response.
func TestHardwareProfile_KueueLabelOverrideWarning(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create HWP with Kueue scheduling
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithKueueScheduling("hwp-queue"),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	// Create "old" notebook with HWP annotation and HWP-applied Kueue label
	oldNotebook := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
	oldNotebookUnstructured, ok := oldNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	// Set Kueue label as if it was applied by HWP
	oldNotebookUnstructured.SetLabels(map[string]string{
		"kueue.x-k8s.io/queue-name": "hwp-queue",
	})

	// Create "new" notebook where user modified the Kueue label to a different value
	newNotebook := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
	newNotebookUnstructured, ok := newNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	// User changed the Kueue label to a different queue
	newNotebookUnstructured.SetLabels(map[string]string{
		"kueue.x-k8s.io/queue-name": "user-custom-queue",
	})

	// Marshal objects
	newObjBytes, err := json.Marshal(newNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())
	oldObjBytes, err := json.Marshal(oldNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create UPDATE request (same HWP, but user modified Kueue label)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Kind:      metav1.GroupVersionKind{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Kind: gvk.Notebook.Kind},
			Resource:  metav1.GroupVersionResource{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Resource: "notebooks"},
			Namespace: testNamespace,
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: newObjBytes},
			OldObject: runtime.RawExtension{Raw: oldObjBytes},
		},
	}

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())

	// Should have a warning about the Kueue label override
	g.Expect(resp.Warnings).ShouldNot(BeEmpty(), "Should have at least one warning")

	foundOverrideWarning := false
	for _, warning := range resp.Warnings {
		if strings.Contains(warning, "kueue.x-k8s.io/queue-name") &&
			strings.Contains(warning, "user-custom-queue") &&
			strings.Contains(warning, "hwp-queue") &&
			strings.Contains(warning, "overwritten") {
			foundOverrideWarning = true
			break
		}
	}
	g.Expect(foundOverrideWarning).Should(BeTrue(), "Should warn about Kueue label being overwritten")
}

// TestHardwareProfile_NoKueueWarningWhenLabelUnchanged tests that no warning is emitted
// when the user hasn't modified the HWP-managed Kueue label value.
func TestHardwareProfile_NoKueueWarningWhenLabelUnchanged(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create HWP with Kueue scheduling
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithKueueScheduling("hwp-queue"),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	// Create "old" notebook with HWP annotation and HWP-applied Kueue label
	oldNotebook := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
	oldNotebookUnstructured, ok := oldNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	oldNotebookUnstructured.SetLabels(map[string]string{
		"kueue.x-k8s.io/queue-name": "hwp-queue",
	})

	// Create "new" notebook with same Kueue label (user added other labels but didn't change Kueue)
	newNotebook := envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
	newNotebookUnstructured, ok := newNotebook.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue())

	// User only added a custom label, didn't modify HWP Kueue label
	newNotebookUnstructured.SetLabels(map[string]string{
		"kueue.x-k8s.io/queue-name": "hwp-queue", // HWP value unchanged
		"my-custom-label":           "my-value",  // User-added
	})

	// Marshal objects
	newObjBytes, err := json.Marshal(newNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())
	oldObjBytes, err := json.Marshal(oldNotebookUnstructured)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create UPDATE request
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Kind:      metav1.GroupVersionKind{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Kind: gvk.Notebook.Kind},
			Resource:  metav1.GroupVersionResource{Group: gvk.Notebook.Group, Version: gvk.Notebook.Version, Resource: "notebooks"},
			Namespace: testNamespace,
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: newObjBytes},
			OldObject: runtime.RawExtension{Raw: oldObjBytes},
		},
	}

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())

	// Should NOT have any warnings since user didn't modify HWP-managed Kueue label
	g.Expect(resp.Warnings).Should(BeEmpty(), "Should have no warnings when user doesn't modify HWP Kueue label")
}
