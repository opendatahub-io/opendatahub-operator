//nolint:testpackage
package modelsasservice

import (
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func newTenantWithFinalizer(name string, finalizers ...string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk.Tenant)
	u.SetName(name)
	u.SetNamespace(MaaSSubscriptionNamespace)
	u.SetFinalizers(finalizers)

	return u
}

func buildClientWithTenants(g Gomega, tenants ...*unstructured.Unstructured) client.Client {
	objs := make([]client.Object, 0, len(tenants))
	for _, t := range tenants {
		objs = append(objs, t)
	}

	cli, err := fakeclient.New(
		fakeclient.WithObjects(objs...),
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: gvk.Tenant, Scope: apimeta.RESTScopeNamespace},
		),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	return cli
}

func TestStripTenantFinalizer(t *testing.T) {
	t.Run("should strip tenant-cleanup finalizer", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		tenant := newTenantWithFinalizer("default-tenant", tenantCleanupFinalizer)
		cli := buildClientWithTenants(g, tenant)

		rr := &odhtypes.ReconciliationRequest{
			Client: cli,
			Instance: &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
			},
		}

		err := stripTenantFinalizer(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk.Tenant)
		g.Expect(cli.List(ctx, list)).Should(Succeed())
		g.Expect(list.Items).Should(HaveLen(1))
		g.Expect(list.Items[0].GetFinalizers()).Should(BeEmpty())
	})

	t.Run("should preserve other finalizers", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		tenant := newTenantWithFinalizer("default-tenant", "some-other/finalizer", tenantCleanupFinalizer)
		cli := buildClientWithTenants(g, tenant)

		rr := &odhtypes.ReconciliationRequest{
			Client: cli,
			Instance: &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
			},
		}

		err := stripTenantFinalizer(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk.Tenant)
		g.Expect(cli.List(ctx, list)).Should(Succeed())
		g.Expect(list.Items).Should(HaveLen(1))
		g.Expect(list.Items[0].GetFinalizers()).Should(ConsistOf("some-other/finalizer"))
	})

	t.Run("should succeed when tenant has no finalizer", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		tenant := newTenantWithFinalizer("default-tenant")
		cli := buildClientWithTenants(g, tenant)

		rr := &odhtypes.ReconciliationRequest{
			Client: cli,
			Instance: &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
			},
		}

		err := stripTenantFinalizer(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should succeed when no tenants exist", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		cli := buildClientWithTenants(g)

		rr := &odhtypes.ReconciliationRequest{
			Client: cli,
			Instance: &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
			},
		}

		err := stripTenantFinalizer(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should succeed when Tenant CRD is not installed", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{
			Client: cli,
			Instance: &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
			},
		}

		err = stripTenantFinalizer(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should handle multiple tenants", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		tenant1 := newTenantWithFinalizer("tenant-1", tenantCleanupFinalizer)
		tenant2 := newTenantWithFinalizer("tenant-2", tenantCleanupFinalizer, "other/finalizer")
		cli := buildClientWithTenants(g, tenant1, tenant2)

		rr := &odhtypes.ReconciliationRequest{
			Client: cli,
			Instance: &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
			},
		}

		err := stripTenantFinalizer(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk.Tenant)
		g.Expect(cli.List(ctx, list)).Should(Succeed())
		g.Expect(list.Items).Should(HaveLen(2))

		for _, item := range list.Items {
			g.Expect(item.GetFinalizers()).ShouldNot(ContainElement(tenantCleanupFinalizer))
		}
	})
}
