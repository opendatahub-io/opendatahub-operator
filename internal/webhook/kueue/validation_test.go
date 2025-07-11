package kueue_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	kueuewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

const (
	testNamespace        = "test-ns"
	nsLabelManaged       = kueuewebhook.KueueManagedLabelKey
	legacyNsLabelManaged = kueuewebhook.KueueLegacyManagedLabelKey
	objLabelQueueName    = kueuewebhook.KueueQueueNameLabelKey
	validQueueName       = "queue"
)

// createDSCWithKueueState creates a DSC with the specified Kueue management state for testing.
func createDSCWithKueueState(state operatorv1.ManagementState) *dscv1.DataScienceCluster {
	dsc := envtestutil.NewDSC("default", "")
	dsc.Status.Components.Kueue = componentApi.DSCKueueStatus{
		KueueManagementSpec: componentApi.KueueManagementSpec{
			ManagementState: state,
		},
	}
	return dsc
}

// TestKueueWebhook_DeniesWhenDecoderNotInitialized tests that the webhook returns an error when the decoder is nil.
func TestKueueWebhook_DeniesWhenDecoderNotInitialized(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := context.Background()

	// Create validator WITHOUT decoder injection
	validator := &kueuewebhook.Validator{
		Name: "test-validator",
		// Decoder is intentionally nil to test the nil check
	}

	// Create a test workload and admission request
	workload := envtestutil.NewNotebook("test-workload", testNamespace, func(obj client.Object) {
		obj.SetLabels(map[string]string{objLabelQueueName: validQueueName})
	})

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

	// Handle the request
	resp := validator.Handle(ctx, req)

	// Should deny the request due to nil decoder
	g.Expect(resp.Allowed).To(BeFalse())
	g.Expect(resp.Result.Message).To(ContainSubstring("webhook decoder not initialized"))
}

// TestKueueWebhook_DeniesUnexpectedKind tests that the webhook properly rejects requests with unexpected kinds.
func TestKueueWebhook_DeniesUnexpectedKind(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := context.Background()
	sch, err := scheme.New()
	g.Expect(err).ToNot(HaveOccurred())

	// Create validator with proper setup
	cli := fake.NewClientBuilder().WithScheme(sch).Build()
	decoder := admission.NewDecoder(sch)
	validator := &kueuewebhook.Validator{
		Client:  cli,
		Name:    "test-validator",
		Decoder: decoder,
	}

	// Create a test object with an unexpected kind
	testObj := envtestutil.NewNotebook("test-unexpected", testNamespace)

	// Create request with an unexpected kind (using a different Group/Version/Kind)
	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		testObj,
		schema.GroupVersionKind{Group: "unexpected.io", Version: "v1", Kind: "UnexpectedKind"},
		metav1.GroupVersionResource{
			Group:    "unexpected.io",
			Version:  "v1",
			Resource: "unexpectedkinds",
		},
	)

	// Handle the request
	resp := validator.Handle(ctx, req)

	// Should deny the request due to unexpected kind
	g.Expect(resp.Allowed).To(BeFalse())
	g.Expect(resp.Result.Message).To(ContainSubstring("unexpected kind: UnexpectedKind"))
}

// TestKueueWebhook_AcceptsExpectedKinds tests that the webhook properly accepts requests with expected kinds.
func TestKueueWebhook_AcceptsExpectedKinds(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := context.Background()
	sch, err := scheme.New()
	g.Expect(err).ToNot(HaveOccurred())

	// Create validator with proper setup
	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(
		envtestutil.NewNamespace(testNamespace, map[string]string{nsLabelManaged: "true"}),
		createDSCWithKueueState(operatorv1.Managed),
	).Build()
	decoder := admission.NewDecoder(sch)
	validator := &kueuewebhook.Validator{
		Client:  cli,
		Name:    "test-validator",
		Decoder: decoder,
	}

	// Test cases for all expected kinds
	testCases := []struct {
		name     string
		gvk      schema.GroupVersionKind
		resource metav1.GroupVersionResource
		objFunc  func() client.Object
	}{
		{
			name: "Notebook",
			gvk:  gvk.Notebook,
			resource: metav1.GroupVersionResource{
				Group:    gvk.Notebook.Group,
				Version:  gvk.Notebook.Version,
				Resource: "notebooks",
			},
			objFunc: func() client.Object {
				return envtestutil.NewNotebook("test-notebook", testNamespace, func(obj client.Object) {
					obj.SetLabels(map[string]string{objLabelQueueName: validQueueName})
				})
			},
		},
		{
			name: "PyTorchJob",
			gvk:  gvk.PyTorchJob,
			resource: metav1.GroupVersionResource{
				Group:    gvk.PyTorchJob.Group,
				Version:  gvk.PyTorchJob.Version,
				Resource: "pytorchjobs",
			},
			objFunc: func() client.Object {
				return envtestutil.NewNotebook("test-pytorchjob", testNamespace, func(obj client.Object) {
					obj.SetLabels(map[string]string{objLabelQueueName: validQueueName})
				})
			},
		},
		{
			name: "RayJob",
			gvk:  gvk.RayJob,
			resource: metav1.GroupVersionResource{
				Group:    gvk.RayJob.Group,
				Version:  gvk.RayJob.Version,
				Resource: "rayjobs",
			},
			objFunc: func() client.Object {
				return envtestutil.NewNotebook("test-rayjob", testNamespace, func(obj client.Object) {
					obj.SetLabels(map[string]string{objLabelQueueName: validQueueName})
				})
			},
		},
		{
			name: "RayCluster",
			gvk:  gvk.RayCluster,
			resource: metav1.GroupVersionResource{
				Group:    gvk.RayCluster.Group,
				Version:  gvk.RayCluster.Version,
				Resource: "rayclusters",
			},
			objFunc: func() client.Object {
				return envtestutil.NewNotebook("test-raycluster", testNamespace, func(obj client.Object) {
					obj.SetLabels(map[string]string{objLabelQueueName: validQueueName})
				})
			},
		},
		{
			name: "InferenceService",
			gvk:  gvk.InferenceServices,
			resource: metav1.GroupVersionResource{
				Group:    gvk.InferenceServices.Group,
				Version:  gvk.InferenceServices.Version,
				Resource: "inferenceservices",
			},
			objFunc: func() client.Object {
				return envtestutil.NewNotebook("test-inferenceservice", testNamespace, func(obj client.Object) {
					obj.SetLabels(map[string]string{objLabelQueueName: validQueueName})
				})
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testObj := tc.objFunc()

			req := envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				testObj,
				tc.gvk,
				tc.resource,
			)

			// Handle the request
			resp := validator.Handle(ctx, req)

			// Should allow the request (kind check passes, validation passes)
			g.Expect(resp.Allowed).To(BeTrue())
		})
	}
}

// TestKueueWebhook_ValidatingWebhook exercises the validating webhook logic for Kueue label validation.
// It verifies that workloads are properly validated based on namespace labels, DSC state, and required Kueue labels
// using table-driven tests and a fake client.
func TestKueueWebhook_ValidatingWebhook(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := context.Background()
	sch, err := scheme.New()
	g.Expect(err).ToNot(HaveOccurred())

	cases := []struct {
		name         string
		existingObjs []client.Object
		req          admission.Request
		allowed      bool
		errorMessage string
	}{
		{
			name: "Kueue not enabled, skip validation",
			existingObjs: []client.Object{
				envtestutil.NewNamespace(testNamespace, map[string]string{nsLabelManaged: "true"}),
				createDSCWithKueueState(operatorv1.Removed),
			},
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewNotebook("test-notebook", testNamespace),
				gvk.Notebook,
				metav1.GroupVersionResource{
					Group:    gvk.Notebook.Group,
					Version:  gvk.Notebook.Version,
					Resource: "notebooks",
				},
			),
			allowed: true,
		},
		{
			name: "Namespace not labeled, skip validation",
			existingObjs: []client.Object{
				envtestutil.NewNamespace(testNamespace, map[string]string{}),
				createDSCWithKueueState(operatorv1.Managed),
			},
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewNotebook("test-notebook", testNamespace),
				gvk.Notebook,
				metav1.GroupVersionResource{
					Group:    gvk.Notebook.Group,
					Version:  gvk.Notebook.Version,
					Resource: "notebooks",
				},
			),
			allowed: true,
		},
		{
			name: "Missing Kueue label",
			existingObjs: []client.Object{
				envtestutil.NewNamespace(testNamespace, map[string]string{nsLabelManaged: "true"}),
				createDSCWithKueueState(operatorv1.Managed),
			},
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewNotebook("test-notebook", testNamespace),
				gvk.Notebook,
				metav1.GroupVersionResource{
					Group:    gvk.Notebook.Group,
					Version:  gvk.Notebook.Version,
					Resource: "notebooks",
				},
			),
			allowed:      false,
			errorMessage: "Kueue label validation failed: missing required label \"kueue.x-k8s.io/queue-name\"",
		},
		{
			name: "Empty Kueue label value",
			existingObjs: []client.Object{
				envtestutil.NewNamespace(testNamespace, map[string]string{nsLabelManaged: "true"}),
				createDSCWithKueueState(operatorv1.Managed),
			},
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewNotebook("test-notebook", testNamespace, func(obj client.Object) {
					obj.SetLabels(map[string]string{objLabelQueueName: ""})
				}),
				gvk.Notebook,
				metav1.GroupVersionResource{
					Group:    gvk.Notebook.Group,
					Version:  gvk.Notebook.Version,
					Resource: "notebooks",
				},
			),
			allowed:      false,
			errorMessage: "Kueue label validation failed: label \"kueue.x-k8s.io/queue-name\" is set but empty",
		},
		{
			name: "Valid Kueue label",
			existingObjs: []client.Object{
				envtestutil.NewNamespace(testNamespace, map[string]string{nsLabelManaged: "true"}),
				createDSCWithKueueState(operatorv1.Managed),
			},
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewNotebook("test-notebook", testNamespace, func(obj client.Object) {
					obj.SetLabels(map[string]string{objLabelQueueName: validQueueName})
				}),
				gvk.Notebook,
				metav1.GroupVersionResource{
					Group:    gvk.Notebook.Group,
					Version:  gvk.Notebook.Version,
					Resource: "notebooks",
				},
			),
			allowed: true,
		},
		{
			name: "Valid Kueue label with other extra labels",
			existingObjs: []client.Object{
				envtestutil.NewNamespace(testNamespace, map[string]string{nsLabelManaged: "true"}),
				createDSCWithKueueState(operatorv1.Managed),
			},
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewNotebook("test-notebook", testNamespace, func(obj client.Object) {
					obj.SetLabels(map[string]string{
						objLabelQueueName: validQueueName,
						"extra-label":     "extra-value",
					})
				}),
				gvk.Notebook,
				metav1.GroupVersionResource{
					Group:    gvk.Notebook.Group,
					Version:  gvk.Notebook.Version,
					Resource: "notebooks",
				},
			),
			allowed: true,
		},
		{
			name: "Legacy namespace label support",
			existingObjs: []client.Object{
				envtestutil.NewNamespace(testNamespace, map[string]string{legacyNsLabelManaged: "true"}),
				createDSCWithKueueState(operatorv1.Managed),
			},
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewNotebook("test-notebook", testNamespace, func(obj client.Object) {
					obj.SetLabels(map[string]string{objLabelQueueName: validQueueName})
				}),
				gvk.Notebook,
				metav1.GroupVersionResource{
					Group:    gvk.Notebook.Group,
					Version:  gvk.Notebook.Version,
					Resource: "notebooks",
				},
			),
			allowed: true,
		},
		{
			name: "Update operation with valid label",
			existingObjs: []client.Object{
				envtestutil.NewNamespace(testNamespace, map[string]string{nsLabelManaged: "true"}),
				createDSCWithKueueState(operatorv1.Managed),
			},
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Update,
				envtestutil.NewNotebook("test-notebook", testNamespace, func(obj client.Object) {
					obj.SetLabels(map[string]string{objLabelQueueName: validQueueName})
				}),
				gvk.Notebook,
				metav1.GroupVersionResource{
					Group:    gvk.Notebook.Group,
					Version:  gvk.Notebook.Version,
					Resource: "notebooks",
				},
			),
			allowed: true,
		},
		{
			name: "Update operation with missing label",
			existingObjs: []client.Object{
				envtestutil.NewNamespace(testNamespace, map[string]string{nsLabelManaged: "true"}),
				createDSCWithKueueState(operatorv1.Managed),
			},
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Update,
				envtestutil.NewNotebook("test-notebook", testNamespace),
				gvk.Notebook,
				metav1.GroupVersionResource{
					Group:    gvk.Notebook.Group,
					Version:  gvk.Notebook.Version,
					Resource: "notebooks",
				},
			),
			allowed:      false,
			errorMessage: "Kueue label validation failed: missing required label \"kueue.x-k8s.io/queue-name\"",
		},
		{
			name: "Delete operation should be allowed",
			existingObjs: []client.Object{
				envtestutil.NewNamespace(testNamespace, map[string]string{nsLabelManaged: "true"}),
				createDSCWithKueueState(operatorv1.Managed),
			},
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Delete,
				envtestutil.NewNotebook("test-notebook", testNamespace), // Labels don't matter for delete
				gvk.Notebook,
				metav1.GroupVersionResource{
					Group:    gvk.Notebook.Group,
					Version:  gvk.Notebook.Version,
					Resource: "notebooks",
				},
			),
			allowed: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(tc.existingObjs...).Build()
			decoder := admission.NewDecoder(sch)
			validator := &kueuewebhook.Validator{
				Client:  cli,
				Name:    "test",
				Decoder: decoder,
			}
			resp := validator.Handle(ctx, tc.req)
			g.Expect(resp.Allowed).To(Equal(tc.allowed))
			if !tc.allowed {
				g.Expect(resp.Result.Message).ToNot(BeEmpty(), "Expected error message when request is denied")
				if tc.errorMessage != "" {
					g.Expect(resp.Result.Message).To(ContainSubstring(tc.errorMessage), "Expected specific error message")
				}
			}
		})
	}
}
