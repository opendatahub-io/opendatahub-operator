package kueue_test

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	kueuewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
)

func TestKueueWebhookHandler(t *testing.T) {
	t.Parallel()
	g := gomega.NewWithT(t)
	ctx := context.Background()
	sch, err := scheme.New()
	g.Expect(err).ToNot(gomega.HaveOccurred())

	ns := "test-ns"

	cases := []struct {
		// name is the test case name
		name string
		// operation is the type of admission operation being tested
		operation admissionv1.Operation
		// nsLabels are the labels on the namespace
		nsLabels map[string]string
		// withKueue indicates if Kueue is installed
		withKueue bool
		// objLabels are the labels on the object being validated
		objLabels map[string]string
		// expectAllow indicates if the validation should allow the operation
		expectAllow bool
		// expectMessage is the expected message in the response
		expectMessage string
	}{
		{
			name: "Kueue not installed, skip validation",
			nsLabels: map[string]string{
				"kueue.openshift.io/managed": "true",
			},
			withKueue:     false,
			objLabels:     map[string]string{},
			expectAllow:   true,
			expectMessage: "Kueue is not installed, skipping label validation",
			operation:     admissionv1.Create,
		},
		{
			name:          "Kueue installed but namespace not labeled, skip validation",
			nsLabels:      map[string]string{},
			withKueue:     true,
			objLabels:     map[string]string{},
			expectAllow:   true,
			expectMessage: "Namespace \"test-ns\" is not labeled for Kueue, skipping label validation",
			operation:     admissionv1.Create,
		},
		{
			name: "Kueue enabled, missing label",
			nsLabels: map[string]string{
				"kueue.openshift.io/managed": "true",
			},
			withKueue:     true,
			objLabels:     map[string]string{},
			expectAllow:   false,
			expectMessage: "Kueue label validation failed: missing required label \"kueue.x-k8s.io/queue-name\"",
			operation:     admissionv1.Create,
		},
		{
			name: "Kueue enabled, empty label value",
			nsLabels: map[string]string{
				"kueue.openshift.io/managed": "true",
			},
			withKueue: true,
			objLabels: map[string]string{
				"kueue.x-k8s.io/queue-name": "",
			},
			expectAllow:   false,
			expectMessage: "Kueue label validation failed: label \"kueue.x-k8s.io/queue-name\" is set but empty",
			operation:     admissionv1.Create,
		},
		{
			name: "Kueue enabled, valid label",
			nsLabels: map[string]string{
				"kueue.openshift.io/managed": "true",
			},
			withKueue: true,
			objLabels: map[string]string{
				"kueue.x-k8s.io/queue-name": "queue1",
			},
			expectAllow:   true,
			expectMessage: "Kueue label validation passed for \"Job\" in namespace \"test-ns\"",
			operation:     admissionv1.Create,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			objs := []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   ns,
						Labels: tc.nsLabels,
					},
				},
			}

			dsc := &dscv1.DataScienceCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
				Status: dscv1.DataScienceClusterStatus{
					InstalledComponents: map[string]bool{
						"kueue": tc.withKueue,
					},
				},
			}
			objs = append(objs, dsc)

			cli := fake.NewClientBuilder().WithScheme(sch).WithRuntimeObjects(objs...).Build()

			obj := &unstructured.Unstructured{}
			obj.SetAPIVersion("batch/v1")
			obj.SetKind("Job")
			obj.SetNamespace(ns)
			obj.SetName("workload")
			obj.SetLabels(tc.objLabels)

			raw, err := obj.MarshalJSON()
			g.Expect(err).ToNot(gomega.HaveOccurred())

			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: tc.operation,
					Object:    runtime.RawExtension{Raw: raw},
					Namespace: ns,
					Kind: metav1.GroupVersionKind{
						Group:   "batch",
						Version: "v1",
						Kind:    "Job",
					},
				},
			}

			decoder := admission.NewDecoder(sch)
			g.Expect(err).ToNot(gomega.HaveOccurred())

			handler := &kueuewebhook.Validator{
				Client:  cli,
				Name:    "test",
				Decoder: decoder,
			}

			resp := handler.Handle(ctx, req)

			g.Expect(resp.Allowed).To(gomega.Equal(tc.expectAllow))
			if tc.expectMessage != "" {
				g.Expect(resp.Result.Message).To(gomega.ContainSubstring(tc.expectMessage))
			}
		})
	}
}
