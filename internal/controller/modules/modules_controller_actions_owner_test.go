//nolint:testpackage
package modules

import (
	"context"
	"errors"
	"testing"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestEnsurePlatformOwnerReferenceMergesOwners(t *testing.T) {
	g := NewWithT(t)

	s, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: "default-dsci", UID: types.UID("dsci-uid")}}
	dsc := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: "default-dsc", UID: types.UID("dsc-uid")}}
	platform := &configv1alpha1.Platform{ObjectMeta: metav1.ObjectMeta{Name: configv1alpha1.PlatformInstanceName}}
	g.Expect(controllerutil.SetOwnerReference(dsci, platform, s)).Should(Succeed())

	cli, err := fakeclient.New(fakeclient.WithScheme(s), fakeclient.WithObjects(platform))
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(EnsurePlatformOwnerReference(t.Context(), cli, dsc, s)).Should(Succeed())

	updated := &configv1alpha1.Platform{}
	g.Expect(cli.Get(t.Context(), client.ObjectKey{Name: configv1alpha1.PlatformInstanceName}, updated)).Should(Succeed())
	g.Expect(updated.GetOwnerReferences()).Should(HaveLen(2))
	g.Expect(updated.GetOwnerReferences()).Should(ContainElements(
		WithTransform(func(ref metav1.OwnerReference) types.UID { return ref.UID }, Equal(dsci.UID)),
		WithTransform(func(ref metav1.OwnerReference) types.UID { return ref.UID }, Equal(dsc.UID)),
	))
}

func TestEnsurePlatformOwnerReferenceRetriesConflicts(t *testing.T) {
	g := NewWithT(t)

	s, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dsc := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: "default-dsc", UID: types.UID("dsc-uid")}}
	platform := &configv1alpha1.Platform{ObjectMeta: metav1.ObjectMeta{Name: configv1alpha1.PlatformInstanceName}}
	updates := 0
	cli, err := fakeclient.New(
		fakeclient.WithScheme(s),
		fakeclient.WithObjects(platform),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			Update: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				updates++
				if updates == 1 {
					return k8serr.NewConflict(schema.GroupResource{Group: configv1alpha1.GroupVersion.Group, Resource: "platforms"}, obj.GetName(), errors.New("conflict"))
				}
				return client.Update(ctx, obj, opts...)
			},
		}),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(EnsurePlatformOwnerReference(t.Context(), cli, dsc, s)).Should(Succeed())
	g.Expect(updates).Should(Equal(2))
}
