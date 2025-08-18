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

	hwpv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/hardwareprofile"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

const (
	testNamespace        = "test-ns"
	testNotebook         = "test-notebook"
	testInferenceService = "test-inference-service"
	testHardwareProfile  = "test-hardware-profile"
	testQueue            = "test-queue"
)

// setupTestEnvironment creates the common test environment for webhook tests.
func setupTestEnvironment(t *testing.T) (*runtime.Scheme, context.Context) {
	t.Helper()
	sch, err := scheme.New()
	NewWithT(t).Expect(err).ShouldNot(HaveOccurred())

	err = hwpv1alpha1.AddToScheme(sch)
	NewWithT(t).Expect(err).ShouldNot(HaveOccurred())

	return sch, context.Background()
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

// WorkloadTestConfig holds configuration for testing different workload types.
type WorkloadTestConfig struct {
	CreateWorkload func(name, namespace string, options ...envtestutil.ObjectOption) client.Object
	ContainersPath []string
	GVK            schema.GroupVersionKind
	ResourceName   string
}

// getNotebookConfig returns test configuration for Notebook workloads.
func getNotebookConfig() WorkloadTestConfig {
	return WorkloadTestConfig{
		CreateWorkload: envtestutil.NewNotebook,
		ContainersPath: []string{"spec", "template", "spec", "containers"},
		GVK:            gvk.Notebook,
		ResourceName:   "notebooks",
	}
}

// getInferenceServiceConfig returns test configuration for InferenceService workloads.
func getInferenceServiceConfig() WorkloadTestConfig {
	return WorkloadTestConfig{
		CreateWorkload: envtestutil.NewInferenceService,
		ContainersPath: []string{"spec", "predictor", "podSpec", "containers"},
		GVK:            gvk.InferenceServices,
		ResourceName:   "inferenceservices",
	}
}

// setContainerResourcesForWorkload sets container resources for any workload type using the provided container path.
func setContainerResourcesForWorkload(workload *unstructured.Unstructured, containersPath []string, resourceType, resourceKey, value string) {
	containers, _, err := unstructured.NestedSlice(workload.Object, containersPath...)
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
	_ = unstructured.SetNestedSlice(workload.Object, containers, containersPath...)
}

// setMultipleContainersForWorkload sets multiple containers for any workload type using the provided container path.
func setMultipleContainersForWorkload(workload *unstructured.Unstructured, containersPath []string, containers []interface{}) {
	_ = unstructured.SetNestedSlice(workload.Object, containers, containersPath...)
}

// setContainerResources is kept for backward compatibility with existing notebook tests.
func setContainerResources(notebook *unstructured.Unstructured, resourceType, resourceKey, value string) {
	setContainerResourcesForWorkload(notebook, []string{"spec", "template", "spec", "containers"}, resourceType, resourceKey, value)
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
			g := NewWithT(t)
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
	ctx := context.Background()
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
	g.Expect(resp.Result.Message).Should(ContainSubstring("failed to get hardware profile 'nonexistent'"))
}

// TestHardwareProfile_ResourceInjection tests that hardware profiles with resource requirements are applied correctly to both Notebook and InferenceService workloads.
func TestHardwareProfile_ResourceInjection(t *testing.T) {
	t.Parallel()
	sch, ctx := setupTestEnvironment(t)

	// Create hardware profile with CPU and memory identifiers
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace,
		envtestutil.WithCPUIdentifier("0", "4"),
		envtestutil.WithMemoryIdentifier("0", "8Gi"),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	workloadConfigs := []WorkloadTestConfig{
		getNotebookConfig(),
		getInferenceServiceConfig(),
	}

	for _, config := range workloadConfigs {
		t.Run(config.ResourceName, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			testCases := []struct {
				name                string
				setupWorkload       func() *unstructured.Unstructured
				expectResourcePatch bool
			}{
				{
					name: "applies resources when none exist",
					setupWorkload: func() *unstructured.Unstructured {
						workload := config.CreateWorkload(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile))
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
						workload, ok := config.CreateWorkload(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
						g.Expect(ok).Should(BeTrue(), "workload should be unstructured")

						// Set existing resources that should be preserved
						containers, _, _ := unstructured.NestedSlice(workload.Object, config.ContainersPath...)
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
						_ = unstructured.SetNestedSlice(workload.Object, containers, config.ContainersPath...)
						return workload
					},
					expectResourcePatch: false,
				},
				{
					name: "applies only missing resources when single container has partial resources",
					setupWorkload: func() *unstructured.Unstructured {
						workload, ok := config.CreateWorkload(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
						g.Expect(ok).Should(BeTrue(), "workload should be unstructured")

						// Set only CPU request, memory should be applied from HWP
						containers, _, _ := unstructured.NestedSlice(workload.Object, config.ContainersPath...)
						containerMap, ok := containers[0].(map[string]interface{})
						g.Expect(ok).Should(BeTrue(), "container should be a map")
						containerMap["resources"] = map[string]interface{}{
							"requests": map[string]interface{}{
								"cpu": "1",
							},
						}
						_ = unstructured.SetNestedSlice(workload.Object, containers, config.ContainersPath...)
						return workload
					},
					expectResourcePatch: true,
				},
				{
					name: "applies resources to containers without them when multiple containers exist",
					setupWorkload: func() *unstructured.Unstructured {
						workload, ok := config.CreateWorkload(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
						g.Expect(ok).Should(BeTrue(), "workload should be unstructured")

						// Create two containers - first has CPU, second has memory, both should get missing resources
						var containers []interface{}
						if config.GVK.Kind == gvk.Notebook.Kind {
							containers = []interface{}{
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
						} else {
							containers = []interface{}{
								map[string]interface{}{
									"name":  "main-container",
									"image": "tensorflow/serving:latest",
									"resources": map[string]interface{}{
										"requests": map[string]interface{}{
											"cpu": "1",
											// Missing memory - should get HWP memory
										},
									},
								},
								map[string]interface{}{
									"name":  "sidecar-container",
									"image": "istio/proxyv2:latest",
									"resources": map[string]interface{}{
										"requests": map[string]interface{}{
											"memory": "2Gi",
											// Missing CPU - should get HWP CPU
										},
									},
								},
							}
						}
						setMultipleContainersForWorkload(workload, config.ContainersPath, containers)
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
						config.GVK,
						metav1.GroupVersionResource{
							Group:    config.GVK.Group,
							Version:  config.GVK.Version,
							Resource: config.ResourceName,
						},
					)

					resp := injector.Handle(ctx, req)
					g.Expect(resp.Allowed).Should(BeTrue())
					g.Expect(hasResourcePatches(resp.Patches)).Should(Equal(tc.expectResourcePatch))
				})
			}
		})
	}
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
		envtestutil.WithResourceIdentifiers(hwpv1alpha1.HardwareIdentifier{
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

// TestHardwareProfile_SchedulingConfiguration tests that hardware profiles with different
// scheduling configurations are applied correctly to both Notebook and InferenceService workloads.
func TestHardwareProfile_SchedulingConfiguration(t *testing.T) {
	t.Parallel()
	sch, ctx := setupTestEnvironment(t)

	workloadConfigs := []WorkloadTestConfig{
		getNotebookConfig(),
		getInferenceServiceConfig(),
	}

	for _, config := range workloadConfigs {
		t.Run(config.ResourceName, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
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
						workload, ok := config.CreateWorkload(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
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
						workload, ok := config.CreateWorkload(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
						g.Expect(ok).Should(BeTrue(), "workload should be unstructured")
						return workload
					},
					expectPatches: true,
				},
			}

			// Add notebook-specific test case for existing resources
			if config.GVK.Kind == gvk.Notebook.Kind {
				testCases = append(testCases, struct {
					name          string
					hwpOptions    []envtestutil.ObjectOption
					setupWorkload func() *unstructured.Unstructured
					expectPatches bool
				}{
					name: "applies kueue scheduling even when resources exist",
					hwpOptions: []envtestutil.ObjectOption{
						envtestutil.WithCPUIdentifier("4", "4"),
						envtestutil.WithKueueScheduling(testQueue),
					},
					setupWorkload: func() *unstructured.Unstructured {
						workload, ok := config.CreateWorkload(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)).(*unstructured.Unstructured)
						g.Expect(ok).Should(BeTrue(), "workload should be unstructured")
						// Add existing CPU requests (this should prevent resource injection but allow scheduling)
						setContainerResources(workload, "requests", "cpu", "2")
						return workload
					},
					expectPatches: true,
				})
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
						config.GVK,
						metav1.GroupVersionResource{
							Group:    config.GVK.Group,
							Version:  config.GVK.Version,
							Resource: config.ResourceName,
						},
					)

					resp := injector.Handle(ctx, req)
					g.Expect(resp.Allowed).Should(BeTrue())
					g.Expect(resp.Patches).Should(Not(BeEmpty()))
				})
			}
		})
	}
}

// TestHardwareProfile_SupportsCrossNamespaceAccess tests that hardware profiles can be accessed from different namespaces for both Notebook and InferenceService workloads.
func TestHardwareProfile_SupportsCrossNamespaceAccess(t *testing.T) {
	t.Parallel()
	sch, ctx := setupTestEnvironment(t)

	hwpNamespace := "hwp-namespace"
	workloadNamespace := "workload-namespace"

	// Create hardware profile in different namespace
	hwp := envtestutil.NewHardwareProfile(testHardwareProfile, hwpNamespace,
		envtestutil.WithGPUIdentifier("nvidia.com/gpu", "1", "1"),
	)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	workloadConfigs := []WorkloadTestConfig{
		getNotebookConfig(),
		getInferenceServiceConfig(),
	}

	for _, config := range workloadConfigs {
		t.Run(config.ResourceName, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			var workload client.Object
			if config.GVK.Kind == gvk.Notebook.Kind {
				workload = config.CreateWorkload(testNotebook, workloadNamespace,
					envtestutil.WithHardwareProfile(testHardwareProfile),
					envtestutil.WithHardwareProfileNamespace(hwpNamespace),
				)
			} else {
				// InferenceService uses a different pattern for cross-namespace annotation
				workload = config.CreateWorkload(testInferenceService, workloadNamespace, func(obj client.Object) {
					annotations := obj.GetAnnotations()
					if annotations == nil {
						annotations = make(map[string]string)
					}
					annotations["opendatahub.io/hardware-profile-name"] = testHardwareProfile
					annotations["opendatahub.io/hardware-profile-namespace"] = hwpNamespace
					obj.SetAnnotations(annotations)
				})
			}

			req := envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				workload,
				config.GVK,
				metav1.GroupVersionResource{
					Group:    config.GVK.Group,
					Version:  config.GVK.Version,
					Resource: config.ResourceName,
				},
			)

			resp := injector.Handle(ctx, req)
			g.Expect(resp.Allowed).Should(BeTrue())
			g.Expect(resp.Patches).Should(Not(BeEmpty()))
		})
	}
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
					"opendatahub.io/hardware-profile-name": testHardwareProfile,
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
			expectMessage: "failed to get hardware profile 'non-existent'",
		},
		{
			name: "empty hardware profile configuration",
			injector: func() *hardwareprofile.Injector {
				// Create empty hardware profile
				hwp := envtestutil.NewHardwareProfile(testHardwareProfile, testNamespace)
				cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
				return createWebhookInjector(cli, sch)
			}(),
			workload:      envtestutil.NewNotebook(testNotebook, testNamespace, envtestutil.WithHardwareProfile(testHardwareProfile)),
			expectAllowed: true,
			expectMessage: "Hardware profile has no configuration to apply",
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
				envtestutil.WithResourceIdentifiers(hwpv1alpha1.HardwareIdentifier{
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
				"opendatahub.io/hardware-profile-name": testHardwareProfile,
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
		"opendatahub.io/hardware-profile-name": testHardwareProfile,
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
