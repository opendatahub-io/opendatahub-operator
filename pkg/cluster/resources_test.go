package cluster_test

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestGetSingletonWithConfigMap(t *testing.T) {
	ctx := context.Background()

	t.Run("should return not found error when no ConfigMap exists", func(t *testing.T) {
		g := NewWithT(t)
		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		obj := &corev1.ConfigMap{}
		err = cluster.GetSingleton(ctx, cli, obj)

		g.Expect(err).To(HaveOccurred())
		g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "Expected NotFound error")
	})

	t.Run("should retrieve the singleton ConfigMap successfully", func(t *testing.T) {
		g := NewWithT(t)

		cli, err := fakeclient.New(
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "singleton-configmap"}})

		g.Expect(err).ShouldNot(HaveOccurred())

		result := &corev1.ConfigMap{}
		err = cluster.GetSingleton(ctx, cli, result)

		g.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("should return an error if multiple ConfigMaps exist", func(t *testing.T) {
		g := NewWithT(t)

		cli, err := fakeclient.New(
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "configmap1"}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "configmap2"}},
		)

		g.Expect(err).ShouldNot(HaveOccurred())

		result := &corev1.ConfigMap{}
		err = cluster.GetSingleton(ctx, cli, result)

		g.Expect(err).To(HaveOccurred())
	})

	t.Run("should return an error when obj is nil", func(t *testing.T) {
		g := NewWithT(t)

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		err = cluster.GetSingleton[*corev1.ConfigMap](ctx, cli, (*corev1.ConfigMap)(nil))

		g.Expect(err).To(HaveOccurred())
	})
}

func TestGetClusterSingletons(t *testing.T) {
	g := NewWithT(t)

	dsciFn := func(ctx context.Context, c client.Client) (client.Object, error) {
		return cluster.GetDSCI(ctx, c)
	}
	dscFn := func(ctx context.Context, c client.Client) (client.Object, error) {
		return cluster.GetDSC(ctx, c)
	}

	// Define test cases
	tests := []struct {
		name string
		objs []client.Object
		err  error
		fn   func(context.Context, client.Client) (client.Object, error)
	}{

		{
			name: "Single DSCInitialization instance found",
			objs: []client.Object{&dsciv1.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: "test-dsci"}}},
			err:  nil,
			fn:   dsciFn,
		},
		{
			name: "No DSCInitialization instances found",
			objs: []client.Object{},
			err:  k8serr.NewNotFound(schema.GroupResource{Group: gvk.DSCInitialization.Group, Resource: "dscinitializations"}, ""),
			fn:   dsciFn,
		},
		{
			name: "Multiple DSCInitialization instances found",
			objs: []client.Object{
				&dsciv1.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: "dsci-1"}},
				&dsciv1.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: "dsci-2"}},
			},
			err: errors.New("failed to get a valid dscinitialization.opendatahub.io/v1, Kind=DSCInitialization instance, expected to find 1 instance, found 2"),
			fn:  dsciFn,
		},

		{
			name: "Single DataScienceCluster instance found",
			objs: []client.Object{&dscv1.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-dsc"}}},
			err:  nil,
			fn:   dscFn,
		},
		{
			name: "No DataScienceCluster instances found",
			objs: []client.Object{},
			err:  k8serr.NewNotFound(schema.GroupResource{Group: gvk.DataScienceCluster.Group, Resource: "datascienceclusters"}, ""),
			fn:   dscFn,
		},
		{
			name: "Multiple DataScienceCluster instances found",
			objs: []client.Object{
				&dscv1.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: "dsc-1"}},
				&dscv1.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: "dsc-2"}},
			},
			err: errors.New("failed to get a valid datasciencecluster.opendatahub.io/v1, Kind=DataScienceCluster instance, expected to find 1 instance, found 2"),
			fn:  dscFn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cli, err := fakeclient.New(tt.objs...)
			g.Expect(err).ShouldNot(HaveOccurred())

			ctx := context.Background()

			result, err := tt.fn(ctx, cli)

			// Validate results
			if tt.err == nil {
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(result).ShouldNot(BeNil())
			} else {
				g.Expect(err).Should(MatchError(tt.err))
				g.Expect(result).Should(BeNil())
			}
		})
	}
}

func TestHasCRDWithVersion(t *testing.T) {
	ctx := context.Background()

	t.Run("should succeed if version is present", func(t *testing.T) {
		g := NewWithT(t)

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		crd := apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: "dashboards.components.platform.opendatahub.io",
			},
			Status: apiextensionsv1.CustomResourceDefinitionStatus{
				StoredVersions: []string{gvk.Dashboard.Version},
			},
		}

		err = cli.Create(ctx, &crd)
		g.Expect(err).ShouldNot(HaveOccurred())

		hasCRD, err := cluster.HasCRDWithVersion(ctx, cli, gvk.Dashboard.GroupKind(), gvk.Dashboard.Version)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(hasCRD).Should(BeTrue())
	})

	t.Run("should fails if version is not present", func(t *testing.T) {
		g := NewWithT(t)

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		crd := apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: "dashboards.components.platform.opendatahub.io",
			},
			Status: apiextensionsv1.CustomResourceDefinitionStatus{
				StoredVersions: []string{"v1alpha2"},
			},
		}

		err = cli.Create(ctx, &crd)
		g.Expect(err).ShouldNot(HaveOccurred())

		hasCRD, err := cluster.HasCRDWithVersion(ctx, cli, gvk.Dashboard.GroupKind(), gvk.Dashboard.Version)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(hasCRD).Should(BeFalse())
	})

	t.Run("should fails if terminating", func(t *testing.T) {
		g := NewWithT(t)

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		crd := apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: "dashboards.components.platform.opendatahub.io",
			},
			Status: apiextensionsv1.CustomResourceDefinitionStatus{
				StoredVersions: []string{gvk.Dashboard.Version},
				Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{{
					Type:   apiextensionsv1.Terminating,
					Status: apiextensionsv1.ConditionTrue,
				}},
			},
		}

		err = cli.Create(ctx, &crd)
		g.Expect(err).ShouldNot(HaveOccurred())

		hasCRD, err := cluster.HasCRDWithVersion(ctx, cli, gvk.Dashboard.GroupKind(), gvk.Dashboard.Version)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(hasCRD).Should(BeFalse())
	})
}
