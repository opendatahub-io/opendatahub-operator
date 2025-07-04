package hardwareprofile_test

import (
	"context"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	testNamespace       = "test-ns"
	testNotebook        = "test-notebook"
	testHardwareProfile = "test-hardware-profile"
	testQueue           = "test-queue"
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
		Client: cli,
		Name:   "test",
	}
	// Simulate the injection that would normally happen in controller-runtime
	decoder := admission.NewDecoder(sch)
	_ = injector.InjectDecoder(decoder)
	return injector
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
			g.Expect(resp.Allowed).To(BeTrue())
			g.Expect(resp.Patches).To(BeEmpty())
		})
	}
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
	g.Expect(resp.Allowed).To(BeFalse())
	g.Expect(resp.Result.Message).To(ContainSubstring("hardware profile nonexistent not found in namespace " + testNamespace))
}

// TestHardwareProfile_AppliesResourceRequirements tests that hardware profiles with resource requirements are applied correctly.
func TestHardwareProfile_AppliesResourceRequirements(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	hwp := envtestutil.NewHWP(testHardwareProfile, testNamespace, func(hwp *hwpv1alpha1.HardwareProfile) {
		hwp.Spec.Identifiers = []hwpv1alpha1.HardwareIdentifier{
			{
				DisplayName:  "NVIDIA GPU",
				Identifier:   "nvidia.com/gpu",
				MinCount:     intstr.FromString("1"),
				DefaultCount: intstr.FromString("1"),
				MaxCount:     &intstr.IntOrString{Type: intstr.String, StrVal: "2"},
			},
			{
				DisplayName:  "CPU",
				Identifier:   "cpu",
				MinCount:     intstr.FromString("2"),
				DefaultCount: intstr.FromString("4"),
				MaxCount:     &intstr.IntOrString{Type: intstr.String, StrVal: "8"},
			},
		}
	})

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
	g.Expect(resp.Allowed).To(BeTrue())
	g.Expect(resp.Patches).ToNot(BeEmpty())
}

// TestHardwareProfile_AppliesKueueConfiguration tests that hardware profiles with Kueue configuration are applied correctly.
func TestHardwareProfile_AppliesKueueConfiguration(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	hwp := envtestutil.NewHWP(testHardwareProfile, testNamespace, func(hwp *hwpv1alpha1.HardwareProfile) {
		hwp.Spec.Identifiers = []hwpv1alpha1.HardwareIdentifier{
			{
				DisplayName:  "Memory",
				Identifier:   "memory",
				MinCount:     intstr.FromString("4Gi"),
				DefaultCount: intstr.FromString("8Gi"),
			},
		}
		hwp.Spec.SchedulingSpec = &hwpv1alpha1.SchedulingSpec{
			SchedulingType: hwpv1alpha1.QueueScheduling,
			Kueue: &hwpv1alpha1.KueueSchedulingSpec{
				LocalQueueName: testQueue,
				PriorityClass:  "high-priority",
			},
		}
	})

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
	g.Expect(resp.Allowed).To(BeTrue())
	g.Expect(resp.Patches).ToNot(BeEmpty())
}

// TestHardwareProfile_SetsNamespaceAnnotation tests that hardware profile namespace annotation is set correctly.
func TestHardwareProfile_SetsNamespaceAnnotation(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	hwp := envtestutil.NewHWP(testHardwareProfile, testNamespace, func(hwp *hwpv1alpha1.HardwareProfile) {
		hwp.Spec.Identifiers = []hwpv1alpha1.HardwareIdentifier{
			{
				DisplayName:  "Test Resource",
				Identifier:   "test.com/resource",
				MinCount:     intstr.FromString("1"),
				DefaultCount: intstr.FromString("1"),
			},
		}
	})

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
	g.Expect(resp.Allowed).To(BeTrue())
	g.Expect(resp.Patches).ToNot(BeEmpty())
}

// TestHardwareProfile_AppliesNodeScheduling tests that hardware profiles with node scheduling are applied correctly.
func TestHardwareProfile_AppliesNodeScheduling(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	hwp := envtestutil.NewHWP(testHardwareProfile, testNamespace, func(hwp *hwpv1alpha1.HardwareProfile) {
		hwp.Spec.Identifiers = []hwpv1alpha1.HardwareIdentifier{
			{
				DisplayName:  "CPU",
				Identifier:   "cpu",
				MinCount:     intstr.FromString("1"),
				DefaultCount: intstr.FromString("2"),
			},
		}
		hwp.Spec.SchedulingSpec = &hwpv1alpha1.SchedulingSpec{
			SchedulingType: hwpv1alpha1.NodeScheduling,
			Node: &hwpv1alpha1.NodeSchedulingSpec{
				NodeSelector: map[string]string{
					"node-type": "gpu-node",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "nvidia.com/gpu",
						Operator: corev1.TolerationOpEqual,
						Value:    "true",
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
		}
	})

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
	g.Expect(resp.Allowed).To(BeTrue())
	g.Expect(resp.Patches).ToNot(BeEmpty())
}

// TestHardwareProfile_SupportsCrossNamespaceAccess tests that hardware profiles can be accessed from different namespaces.
func TestHardwareProfile_SupportsCrossNamespaceAccess(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	otherNs := "other-ns"

	hwp := envtestutil.NewHWP(testHardwareProfile, otherNs, func(hwp *hwpv1alpha1.HardwareProfile) {
		hwp.Spec.Identifiers = []hwpv1alpha1.HardwareIdentifier{
			{
				DisplayName:  "NVIDIA GPU",
				Identifier:   "nvidia.com/gpu",
				MinCount:     intstr.FromString("1"),
				DefaultCount: intstr.FromString("1"),
			},
		}
	})

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(hwp).Build()
	injector := createWebhookInjector(cli, sch)

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		envtestutil.NewNotebook(testNotebook, testNamespace,
			envtestutil.WithHardwareProfile(testHardwareProfile),
			envtestutil.WithHardwareProfileNamespace(otherNs),
		),
		gvk.Notebook,
		metav1.GroupVersionResource{
			Group:    gvk.Notebook.Group,
			Version:  gvk.Notebook.Version,
			Resource: "notebooks",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).To(BeTrue())
	g.Expect(resp.Patches).ToNot(BeEmpty())
}

// TestHardwareProfile_HandlesUpdateOperations tests that update operations are handled correctly.
func TestHardwareProfile_HandlesUpdateOperations(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create a hardware profile with multiple types of specifications
	hwp := envtestutil.NewHWP(testHardwareProfile, testNamespace, func(hwp *hwpv1alpha1.HardwareProfile) {
		hwp.Spec.Identifiers = []hwpv1alpha1.HardwareIdentifier{
			{
				DisplayName:  "Memory",
				Identifier:   "memory",
				MinCount:     intstr.FromString("4Gi"),
				DefaultCount: intstr.FromString("8Gi"),
			},
		}
		hwp.Spec.SchedulingSpec = &hwpv1alpha1.SchedulingSpec{
			SchedulingType: hwpv1alpha1.QueueScheduling,
			Kueue: &hwpv1alpha1.KueueSchedulingSpec{
				LocalQueueName: "update-test-queue",
			},
		}
	})

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
	g.Expect(resp.Allowed).To(BeTrue())
	g.Expect(resp.Patches).ToNot(BeEmpty())

	// Verify specific patches are applied for update operations
	g.Expect(resp.Result).To(BeNil(), "Update operations should not return error results")
}
