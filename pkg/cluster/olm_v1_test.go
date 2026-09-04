package cluster_test

import (
	"context"
	"testing"

	olmv1 "github.com/operator-framework/operator-controller/api/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	testscheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// noOLMv1Client simulates a cluster where OLMv1 CRDs are not installed.
type noOLMv1Client struct {
	client.Client
}

func (c *noOLMv1Client) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if _, ok := list.(*olmv1.ClusterExtensionList); ok {
		return &meta.NoKindMatchError{
			GroupKind: schema.GroupKind{Group: olmv1.GroupVersion.Group, Kind: "ClusterExtension"},
		}
	}
	return c.Client.List(ctx, list, opts...)
}

func newOLMv1NoMatchClient(t *testing.T) client.Client {
	t.Helper()
	g := NewWithT(t)
	scheme, err := testscheme.New()
	g.Expect(err).NotTo(HaveOccurred())
	return &noOLMv1Client{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
}

func newOLMv1TestClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	g := NewWithT(t)
	scheme, err := testscheme.New()
	g.Expect(err).NotTo(HaveOccurred())
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func TestDeleteClusterExtension(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	rhoaiPackage := cluster.OperatorOLMPackageName(cluster.SelfManagedRhoai)

	ext := &olmv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "rhoai-install"},
		Spec: olmv1.ClusterExtensionSpec{
			Namespace: "redhat-ods-operator",
			Source: olmv1.SourceConfig{
				SourceType: olmv1.SourceTypeCatalog,
				Catalog: &olmv1.CatalogFilter{
					PackageName: rhoaiPackage,
				},
			},
		},
	}
	odhExt := &olmv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "odh-install"},
		Spec: olmv1.ClusterExtensionSpec{
			Namespace: "opendatahub-operator-system",
			Source: olmv1.SourceConfig{
				SourceType: olmv1.SourceTypeCatalog,
				Catalog: &olmv1.CatalogFilter{
					PackageName: cluster.OperatorOLMPackageName(cluster.OpenDataHub),
				},
			},
		},
	}

	t.Run("deletes extension matched by package name not resource name", func(t *testing.T) {
		cli := newOLMv1TestClient(t, ext, odhExt)
		g.Expect(cluster.DeleteClusterExtension(ctx, cli, rhoaiPackage)).To(Succeed())

		remaining := &olmv1.ClusterExtension{}
		err := cli.Get(ctx, client.ObjectKey{Name: ext.Name}, remaining)
		g.Expect(err).To(HaveOccurred())
		g.Expect(k8serr.IsNotFound(err)).To(BeTrue())

		g.Expect(cli.Get(ctx, client.ObjectKey{Name: odhExt.Name}, &olmv1.ClusterExtension{})).To(Succeed())
	})

	t.Run("no-op when package is not installed", func(t *testing.T) {
		cli := newOLMv1TestClient(t, odhExt)
		g.Expect(cluster.DeleteClusterExtension(ctx, cli, rhoaiPackage)).To(Succeed())
	})

	t.Run("no-op when OLMv1 CRD is absent", func(t *testing.T) {
		cli := newOLMv1NoMatchClient(t)
		g.Expect(cluster.DeleteClusterExtension(ctx, cli, rhoaiPackage)).To(Succeed())
	})
}
