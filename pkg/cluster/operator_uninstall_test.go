package cluster_test

import (
	"testing"

	olmv1 "github.com/operator-framework/operator-controller/api/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	testscheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestOperatorUninstallRemovesClusterExtension(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	ext := &olmv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "odh-ext"},
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

	scheme, err := testscheme.New()
	g.Expect(err).NotTo(HaveOccurred())
	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(testClusterVersion(), ext).
		Build()

	initCluster(t, cli, operatorconfig.OperatorSettings{
		OperatorNamespace: "opendatahub-operator-system",
		PlatformType:      "OpenDataHub",
	})

	g.Expect(upgrade.OperatorUninstall(ctx, cli, cluster.OpenDataHub)).To(Succeed())

	remaining := &olmv1.ClusterExtension{}
	err = cli.Get(ctx, client.ObjectKey{Name: ext.Name}, remaining)
	g.Expect(err).To(HaveOccurred())
	g.Expect(k8serr.IsNotFound(err)).To(BeTrue())
}
