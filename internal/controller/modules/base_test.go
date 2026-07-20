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

func TestDeleteRenderedResources_SkipsCRDs(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	ctx := context.Background()
	log := logf.Log

	configMap := makeUnstructured(gvk.ConfigMap, "test-cm", "test-ns")
	crd := makeUnstructured(gvk.CustomResourceDefinition, "tests.example.com", "")

	cli, err := fakeclient.New(fakeclient.WithObjects(&configMap, &crd))
	g.Expect(err).NotTo(HaveOccurred())

	handler := &BaseHandler{Config: ModuleConfig{Name: "test-module"}}
	resources := []unstructured.Unstructured{configMap, crd}

	err = handler.deleteRenderedResources(ctx, cli, log, resources)
	g.Expect(err).NotTo(HaveOccurred())

	crdLookup := unstructured.Unstructured{}
	crdLookup.SetGroupVersionKind(gvk.CustomResourceDefinition)
	err = cli.Get(ctx, client.ObjectKeyFromObject(&crd), &crdLookup)
	g.Expect(err).NotTo(HaveOccurred(), "CRD should not be deleted")

	cmLookup := unstructured.Unstructured{}
	cmLookup.SetGroupVersionKind(gvk.ConfigMap)
	err = cli.Get(ctx, client.ObjectKeyFromObject(&configMap), &cmLookup)
	g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "ConfigMap should be deleted")
}

func TestDeleteRenderedResources_SkipsNamespaces(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	ctx := context.Background()
	log := logf.Log

	configMap := makeUnstructured(gvk.ConfigMap, "test-cm", "test-ns")
	ns := makeUnstructured(gvk.Namespace, "operator-ns", "")

	cli, err := fakeclient.New(fakeclient.WithObjects(&configMap, &ns))
	g.Expect(err).NotTo(HaveOccurred())

	handler := &BaseHandler{Config: ModuleConfig{Name: "test-module"}}
	resources := []unstructured.Unstructured{configMap, ns}

	err = handler.deleteRenderedResources(ctx, cli, log, resources)
	g.Expect(err).NotTo(HaveOccurred())

	nsLookup := unstructured.Unstructured{}
	nsLookup.SetGroupVersionKind(gvk.Namespace)
	err = cli.Get(ctx, client.ObjectKeyFromObject(&ns), &nsLookup)
	g.Expect(err).NotTo(HaveOccurred(), "Namespace should not be deleted")

	cmLookup := unstructured.Unstructured{}
	cmLookup.SetGroupVersionKind(gvk.ConfigMap)
	err = cli.Get(ctx, client.ObjectKeyFromObject(&configMap), &cmLookup)
	g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "ConfigMap should be deleted")
}
