package kueue_test

import (
	"context"
	"strings"
	"testing"

	"github.com/onsi/gomega"
	operatorv1 "github.com/openshift/api/operator/v1"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	kueuewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
)

const (
	testNamespace        = "test-ns"
	nsLabelManaged       = kueuewebhook.KueueManagedLabelKey
	legacyNsLabelManaged = kueuewebhook.KueueLegacyManagedLabelKey
	objLabelQueueName    = kueuewebhook.KueueQueueNameLabelKey
	validQueueName       = "queue"
)

func newFakeClientWithObjects(sch *runtime.Scheme, nsLabels map[string]string, kueueState operatorv1.ManagementState) *fake.ClientBuilder {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   testNamespace,
			Labels: nsLabels,
		},
	}

	dsc := &dscv1.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
		Status: dscv1.DataScienceClusterStatus{
			Components: dscv1.ComponentsStatus{
				Kueue: componentApi.DSCKueueStatus{
					KueueManagementSpec: componentApi.KueueManagementSpec{
						ManagementState: kueueState,
					},
				},
			},
		},
	}

	return fake.NewClientBuilder().WithScheme(sch).WithRuntimeObjects(ns, dsc)
}

func createWorkload(gvk schema.GroupVersionKind, ns string, labels map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetNamespace(ns)
	obj.SetName("workload")
	obj.SetLabels(labels)
	return obj
}

func createAdmissionRequest(t *testing.T, operation admissionv1.Operation, obj runtime.Object, ns string, gvk schema.GroupVersionKind) admission.Request {
	t.Helper()
	raw, err := runtime.Encode(unstructured.UnstructuredJSONScheme, obj)
	if err != nil {
		t.Fatalf("failed to marshal object: %v", err)
	}

	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: operation,
			Object:    runtime.RawExtension{Raw: raw},
			Namespace: ns,
			Kind: metav1.GroupVersionKind{
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind,
			},
		},
	}
}

func TestKueueWebhookHandler(t *testing.T) {
	t.Parallel()
	g := gomega.NewWithT(t)
	ctx := context.Background()

	sch, err := scheme.New()
	g.Expect(err).ToNot(gomega.HaveOccurred())

	// Included NoteBook as representative GVK since the webhook logic is metadata-based
	// and does not vary across resource types (GVKs). Other GVKs like RayJob, etc.,
	// are not included here to avoid redundant tests. Add them only if GVK-specific logic
	// is introduced in the future.
	supportedGVKs := []schema.GroupVersionKind{
		{Group: "kubeflow.org", Version: "v1", Kind: "NoteBook"},
	}

	testCases := []struct {
		name          string
		operation     admissionv1.Operation
		nsLabels      map[string]string
		kueueState    operatorv1.ManagementState
		objLabels     map[string]string
		expectAllow   bool
		expectMessage string
	}{
		{
			name: "Kueue not enabled, skip validation",
			nsLabels: map[string]string{
				nsLabelManaged: "true",
			},
			kueueState: operatorv1.Removed,
			objLabels: map[string]string{
				nsLabelManaged: "true",
			},
			expectAllow:   true,
			expectMessage: "Kueue is not enabled in DSC, skipping Kueue label validation",
			operation:     admissionv1.Create,
		},
		{
			name:          "Namespace not labeled, skip validation",
			nsLabels:      map[string]string{},
			kueueState:    operatorv1.Managed,
			objLabels:     map[string]string{nsLabelManaged: "true"},
			expectAllow:   true,
			expectMessage: "Namespace \"test-ns\" is not labeled for Kueue (\"kueue.openshift.io/managed\"), skipping Kueue label validation",
			operation:     admissionv1.Create,
		},
		{
			name: "Missing Kueue label",
			nsLabels: map[string]string{
				nsLabelManaged: "true",
			},
			kueueState:    operatorv1.Managed,
			objLabels:     map[string]string{},
			expectAllow:   false,
			expectMessage: "Kueue label validation failed: missing required label \"kueue.x-k8s.io/queue-name\"",
			operation:     admissionv1.Create,
		},
		{
			name: "Empty Kueue label value",
			nsLabels: map[string]string{
				nsLabelManaged: "true",
			},
			kueueState: operatorv1.Managed,
			objLabels: map[string]string{
				objLabelQueueName: "",
			},
			expectAllow:   false,
			expectMessage: "Kueue label validation failed: label \"kueue.x-k8s.io/queue-name\" is set but empty",
			operation:     admissionv1.Create,
		},
		{
			name: "Valid Kueue label",
			nsLabels: map[string]string{
				nsLabelManaged: "true",
			},
			kueueState: operatorv1.Managed,
			objLabels: map[string]string{
				objLabelQueueName: validQueueName,
			},
			expectAllow:   true,
			expectMessage: "Kueue label validation passed for \"$Kind\" in namespace \"test-ns\"",
			operation:     admissionv1.Create,
		},
		{
			name: "Valid Kueue label with other extra labels",
			nsLabels: map[string]string{
				nsLabelManaged: "true",
			},
			kueueState: operatorv1.Managed,
			objLabels: map[string]string{
				objLabelQueueName:           validQueueName,
				"random.label.io/something": "yes",
			},
			expectAllow:   true,
			expectMessage: "Kueue label validation passed for \"$Kind\" in namespace \"test-ns\"",
			operation:     admissionv1.Create,
		},
		{
			name: "Incorrect Kueue label key",
			nsLabels: map[string]string{
				nsLabelManaged: "true",
			},
			kueueState: operatorv1.Managed,
			objLabels: map[string]string{
				"kueue.x-k8s.io/queue-naem": validQueueName,
			},
			expectAllow:   false,
			expectMessage: "Kueue label validation failed: missing required label \"kueue.x-k8s.io/queue-name\"",
			operation:     admissionv1.Create,
		},
		{
			name:       "Queue label present but namespace not labeled",
			nsLabels:   map[string]string{},
			kueueState: operatorv1.Managed,
			objLabels: map[string]string{
				objLabelQueueName: validQueueName,
			},
			expectAllow:   true,
			expectMessage: "Namespace \"test-ns\" is not labeled for Kueue (\"kueue.openshift.io/managed\"), skipping Kueue label validation",
			operation:     admissionv1.Create,
		},
		{
			name: "Namespace is Kueue-enabled with legacy label",
			nsLabels: map[string]string{
				legacyNsLabelManaged: "true",
			},
			kueueState: operatorv1.Managed,
			objLabels: map[string]string{
				objLabelQueueName: validQueueName,
			},
			expectAllow:   true,
			expectMessage: "Kueue label validation passed for \"$Kind\" in namespace \"test-ns\"",
			operation:     admissionv1.Create,
		},
		{
			name: "Kueue unmanaged state treated as enabled - success",
			nsLabels: map[string]string{
				nsLabelManaged: "true",
			},
			kueueState: operatorv1.Unmanaged,
			objLabels: map[string]string{
				objLabelQueueName: validQueueName,
			},
			expectAllow:   true,
			expectMessage: "Kueue label validation passed for \"$Kind\" in namespace \"test-ns\"",
			operation:     admissionv1.Create,
		},
	}

	for _, tc := range testCases {
		for _, gvk := range supportedGVKs {
			t.Run(tc.name+"_"+gvk.Kind, func(t *testing.T) {
				t.Parallel()

				cli := newFakeClientWithObjects(sch, tc.nsLabels, tc.kueueState).Build()
				workload := createWorkload(gvk, testNamespace, tc.objLabels)
				req := createAdmissionRequest(t, tc.operation, workload, testNamespace, gvk)

				decoder := admission.NewDecoder(sch)
				handler := &kueuewebhook.Validator{
					Client:  cli,
					Name:    "test",
					Decoder: decoder,
				}

				resp := handler.Handle(ctx, req)

				g.Expect(resp.Allowed).To(gomega.Equal(tc.expectAllow))
				if tc.expectMessage != "" {
					expectedMsg := strings.Replace(tc.expectMessage, "$Kind", gvk.Kind, 1)
					g.Expect(resp.Result.Message).To(gomega.ContainSubstring(expectedMsg))
				}
			})
		}
	}
}
