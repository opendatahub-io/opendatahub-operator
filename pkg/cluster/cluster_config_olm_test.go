package cluster_test

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	olmv1 "github.com/operator-framework/operator-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
	testscheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

const managedAddonCatalogName = "addon-managed-odh-catalog"

func testClusterVersion() *configv1.ClusterVersion {
	return &configv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{Name: "version"},
		Status: configv1.ClusterVersionStatus{
			History: []configv1.UpdateHistory{{Version: "4.16.0"}},
		},
	}
}

func newPlatformTestClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	g := NewWithT(t)
	scheme, err := testscheme.New()
	g.Expect(err).NotTo(HaveOccurred())
	allObjs := append([]client.Object{testClusterVersion()}, objs...)
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(allObjs...).Build()
}

func initCluster(t *testing.T, cli client.Client, cfg operatorconfig.OperatorSettings) {
	t.Helper()
	g := NewWithT(t)
	t.Setenv("CI", "")
	t.Cleanup(cluster.ResetConfigForTest)
	g.Expect(cluster.Init(context.Background(), cli, cfg)).To(Succeed())
}

func TestInitDetectsManagedRhoai(t *testing.T) {
	g := NewWithT(t)

	t.Run("OLMv0 CatalogSource", func(t *testing.T) {
		cli := newPlatformTestClient(t, &ofapiv1alpha1.CatalogSource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      managedAddonCatalogName,
				Namespace: "redhat-ods-operator",
			},
		})
		initCluster(t, cli, operatorconfig.OperatorSettings{
			OperatorNamespace: "redhat-ods-operator",
		})

		g.Expect(cluster.GetRelease().Name).To(Equal(cluster.ManagedRhoai))
	})

	t.Run("OLMv1 ClusterCatalog", func(t *testing.T) {
		cli := newPlatformTestClient(t, &olmv1.ClusterCatalog{
			ObjectMeta: metav1.ObjectMeta{Name: managedAddonCatalogName},
		})
		initCluster(t, cli, operatorconfig.OperatorSettings{
			OperatorNamespace: "redhat-ods-operator",
		})

		g.Expect(cluster.GetRelease().Name).To(Equal(cluster.ManagedRhoai))
	})

	t.Run("no catalog returns OpenDataHub", func(t *testing.T) {
		cli := newPlatformTestClient(t)
		initCluster(t, cli, operatorconfig.OperatorSettings{
			OperatorNamespace: "opendatahub-operator-system",
		})

		g.Expect(cluster.GetRelease().Name).To(Equal(cluster.OpenDataHub))
	})
}

func TestInitDetectsSelfManagedRhoai(t *testing.T) {
	g := NewWithT(t)

	t.Run("OLMv0 OperatorCondition", func(t *testing.T) {
		cli := newPlatformTestClient(t, &ofapiv2.OperatorCondition{
			ObjectMeta: metav1.ObjectMeta{Name: "rhods-operator.v2.10.0"},
		})
		initCluster(t, cli, operatorconfig.OperatorSettings{
			OperatorNamespace: "redhat-ods-operator",
		})

		g.Expect(cluster.GetRelease().Name).To(Equal(cluster.SelfManagedRhoai))
	})

	t.Run("OLMv1 ClusterExtension", func(t *testing.T) {
		cli := newPlatformTestClient(t, &olmv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "rhoai-ext"},
			Spec: olmv1.ClusterExtensionSpec{
				Namespace: "redhat-ods-operator",
				Source: olmv1.SourceConfig{
					SourceType: olmv1.SourceTypeCatalog,
					Catalog: &olmv1.CatalogFilter{
						PackageName: cluster.OperatorOLMPackageName(cluster.SelfManagedRhoai),
					},
				},
			},
		})
		initCluster(t, cli, operatorconfig.OperatorSettings{
			OperatorNamespace: "redhat-ods-operator",
		})

		g.Expect(cluster.GetRelease().Name).To(Equal(cluster.SelfManagedRhoai))
	})

	t.Run("OLMv1 ODH extension only", func(t *testing.T) {
		cli := newPlatformTestClient(t, &olmv1.ClusterExtension{
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
		})
		initCluster(t, cli, operatorconfig.OperatorSettings{
			OperatorNamespace: "opendatahub-operator-system",
		})

		g.Expect(cluster.GetRelease().Name).To(Equal(cluster.OpenDataHub))
	})
}

func TestInitPlatformTypeBypass(t *testing.T) {
	g := NewWithT(t)
	cli := newPlatformTestClient(t)

	initCluster(t, cli, operatorconfig.OperatorSettings{
		OperatorNamespace: "opendatahub-operator-system",
		PlatformType:      "SelfManagedRHOAI",
	})

	g.Expect(cluster.GetRelease().Name).To(Equal(cluster.SelfManagedRhoai))
}

func TestInitReadsClusterExtensionVersion(t *testing.T) {
	g := NewWithT(t)

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
		Status: olmv1.ClusterExtensionStatus{
			Install: &olmv1.ClusterExtensionInstallStatus{
				Bundle: olmv1.BundleMetadata{Version: "2.10.0"},
			},
		},
	}
	cli := newPlatformTestClient(t, ext)
	initCluster(t, cli, operatorconfig.OperatorSettings{
		OperatorNamespace: "opendatahub-operator-system",
		PlatformType:      "OpenDataHub",
	})

	release := cluster.GetRelease()
	g.Expect(release.Name).To(Equal(cluster.OpenDataHub))
	g.Expect(release.Version.String()).To(Equal("2.10.0"))
}

func TestInitClusterExtensionWithoutInstallStatus(t *testing.T) {
	g := NewWithT(t)

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
	cli := newPlatformTestClient(t, ext)
	initCluster(t, cli, operatorconfig.OperatorSettings{
		OperatorNamespace: "opendatahub-operator-system",
		PlatformType:      "OpenDataHub",
	})

	g.Expect(cluster.GetRelease().Version.String()).To(Equal("0.0.0"))
}
