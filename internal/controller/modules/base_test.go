//nolint:testpackage
package modules

import (
	"context"
	"testing"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func makeUnstructured(gvkVal schema.GroupVersionKind, name, namespace string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetGroupVersionKind(gvkVal)
	u.SetName(name)
	if namespace != "" {
		u.SetNamespace(namespace)
	}

	return u
}

func TestDeleteRenderedResources_SkipsProtectedResources(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		skipGVK     schema.GroupVersionKind
		skipResName string
		skipDesc    string
	}{
		{
			name:        "CRDs are not deleted",
			skipGVK:     gvk.CustomResourceDefinition,
			skipResName: "tests.example.com",
			skipDesc:    "CRD",
		},
		{
			name:        "Namespaces are not deleted",
			skipGVK:     gvk.Namespace,
			skipResName: "operator-ns",
			skipDesc:    "Namespace",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			ctx := context.Background()
			log := logf.Log

			configMap := makeUnstructured(gvk.ConfigMap, "test-cm", "test-ns")
			skipRes := makeUnstructured(tc.skipGVK, tc.skipResName, "")

			cli, err := fakeclient.New(fakeclient.WithObjects(&configMap, &skipRes))
			g.Expect(err).NotTo(HaveOccurred())

			handler := &BaseHandler{Config: ModuleConfig{Name: "test-module"}}
			resources := []unstructured.Unstructured{configMap, skipRes}

			err = handler.deleteRenderedResources(ctx, cli, log, resources)
			g.Expect(err).NotTo(HaveOccurred())

			skipLookup := unstructured.Unstructured{}
			skipLookup.SetGroupVersionKind(tc.skipGVK)
			err = cli.Get(ctx, client.ObjectKeyFromObject(&skipRes), &skipLookup)
			g.Expect(err).NotTo(HaveOccurred(), tc.skipDesc+" should not be deleted")

			cmLookup := unstructured.Unstructured{}
			cmLookup.SetGroupVersionKind(gvk.ConfigMap)
			err = cli.Get(ctx, client.ObjectKeyFromObject(&configMap), &cmLookup)
			g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "ConfigMap should be deleted")
		})
	}
}
