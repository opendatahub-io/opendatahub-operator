//nolint:testpackage
package modules

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	. "github.com/onsi/gomega"
)

func TestDeleteRenderedResources_SkipsNamespaces(t *testing.T) {
	g := NewGomegaWithT(t)

	var deletedKinds []string

	cli := fake.NewClientBuilder().
		WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
				u, ok := obj.(*unstructured.Unstructured)
				if ok {
					deletedKinds = append(deletedKinds, u.GetKind())
				}
				return nil
			},
		}).
		Build()

	resources := []unstructured.Unstructured{
		makeResource("Deployment", "apps", "controller-manager", "test-ns"),
		makeResource("Namespace", "", "my-module-system", ""),
		makeResource("ServiceAccount", "", "controller-manager", "test-ns"),
	}

	b := &BaseHandler{Config: ModuleConfig{Name: "test-module"}}
	err := b.deleteRenderedResources(context.Background(), cli, logr.Discard(), resources)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deletedKinds).To(ConsistOf("Deployment", "ServiceAccount"))
}

func TestDeleteRenderedResources_SkipsCRDs(t *testing.T) {
	g := NewGomegaWithT(t)

	var deletedKinds []string

	cli := fake.NewClientBuilder().
		WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
				u, ok := obj.(*unstructured.Unstructured)
				if ok {
					deletedKinds = append(deletedKinds, u.GetKind())
				}
				return nil
			},
		}).
		Build()

	resources := []unstructured.Unstructured{
		makeResource("Deployment", "apps", "controller-manager", "test-ns"),
		makeCRD("myresources.example.com"),
		makeResource("ClusterRole", "rbac.authorization.k8s.io", "manager-role", ""),
	}

	b := &BaseHandler{Config: ModuleConfig{Name: "test-module"}}
	err := b.deleteRenderedResources(context.Background(), cli, logr.Discard(), resources)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deletedKinds).To(ConsistOf("Deployment", "ClusterRole"))
}

func makeResource(kind, group, name, namespace string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: "v1", Kind: kind})
	u.SetName(name)
	if namespace != "" {
		u.SetNamespace(namespace)
	}
	return u
}

func makeCRD(name string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	})
	u.SetName(name)
	return u
}
