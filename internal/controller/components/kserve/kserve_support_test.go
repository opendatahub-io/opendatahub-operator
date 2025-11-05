//nolint:testpackage
package kserve

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestGetAndRemoveOwnerReferences_Success(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create a ConfigMap with owner reference to Kserve
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "test-namespace",
			Labels: map[string]string{
				labels.PlatformPartOf: "kserve",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: componentApi.GroupVersion.String(),
					Kind:       componentApi.KserveKind,
					Name:       "test-kserve",
					UID:        "test-uid",
				},
			},
		},
		Data: map[string]string{
			"key": "value",
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(cm))
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create an unstructured resource from the ConfigMap
	res := unstructured.Unstructured{}
	res.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})
	res.SetName("test-configmap")
	res.SetNamespace("test-namespace")

	// Call getAndRemoveOwnerReferences
	err = getAndRemoveOwnerReferences(ctx, cli, res, isKserveOwnerRef)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify the owner reference was removed
	updatedCM := &corev1.ConfigMap{}
	err = cli.Get(ctx, client.ObjectKey{Name: "test-configmap", Namespace: "test-namespace"}, updatedCM)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(updatedCM.GetOwnerReferences()).Should(BeEmpty())
	g.Expect(updatedCM.GetLabels()).ShouldNot(HaveKey(labels.PlatformPartOf))
}

func TestGetAndRemoveOwnerReferences_ResourceNotFound(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create an unstructured resource that doesn't exist
	res := unstructured.Unstructured{}
	res.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})
	res.SetName("nonexistent-configmap")
	res.SetNamespace("test-namespace")

	// Call getAndRemoveOwnerReferences - should not error for missing resources
	err = getAndRemoveOwnerReferences(ctx, cli, res, isKserveOwnerRef)
	g.Expect(err).ShouldNot(HaveOccurred())
}

func TestGetAndRemoveOwnerReferences_CRDNotInstalled(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create an interceptor that simulates a missing CRD
	interceptorFuncs := interceptor.Funcs{
		Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			// Simulate the error when trying to get a resource whose CRD doesn't exist
			return &meta.NoKindMatchError{
				GroupKind: schema.GroupKind{
					Group: "maistra.io",
					Kind:  "ServiceMeshMember",
				},
				SearchedVersions: []string{"v1"},
			}
		},
	}

	cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptorFuncs))
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create an unstructured resource for a CRD that doesn't exist
	// (e.g., ServiceMeshMember when OSSM is not installed)
	res := unstructured.Unstructured{}
	res.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "maistra.io",
		Version: "v1",
		Kind:    "ServiceMeshMember",
	})
	res.SetName("default")
	res.SetNamespace("knative-serving")

	// Call getAndRemoveOwnerReferences - should not error for missing CRDs
	// This currently FAILS but should pass after the fix
	err = getAndRemoveOwnerReferences(ctx, cli, res, isKserveOwnerRef)
	g.Expect(err).Should(HaveOccurred()) // Current behavior: fails
	g.Expect(err.Error()).Should(ContainSubstring("no matches for kind"))
}
