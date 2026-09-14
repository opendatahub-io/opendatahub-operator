//nolint:testpackage
package dscinitialization

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	testscheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestReconcileDSCIModulesPreservesExistingOwner(t *testing.T) {
	g := NewWithT(t)

	s, err := testscheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{
		Name: "default-dsci",
		UID:  types.UID("dsci-uid"),
	}}
	dsc := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{
		Name: "default-dsc",
		UID:  types.UID("dsc-uid"),
	}}
	platform := modules.NewPlatformCR(&modules.DSCContext{DSCI: dsci}, modules.ConfigFromDSCI)
	g.Expect(controllerutil.SetOwnerReference(dsc, platform, s)).Should(Succeed())

	cli, err := fakeclient.New(fakeclient.WithScheme(s), fakeclient.WithObjects(platform))
	g.Expect(err).ShouldNot(HaveOccurred())

	reconciler := &DSCInitializationReconciler{
		Client: cli,
		Scheme: s,
	}
	g.Expect(reconciler.reconcileDSCIModules(t.Context(), dsci)).Should(Succeed())

	foundPlatform := &configv1alpha1.Platform{}
	g.Expect(cli.Get(t.Context(), client.ObjectKey{Name: configv1alpha1.PlatformInstanceName}, foundPlatform)).Should(Succeed())
	g.Expect(foundPlatform.GetOwnerReferences()).Should(ContainElements(
		WithTransform(func(ref metav1.OwnerReference) types.UID { return ref.UID }, Equal(dsci.UID)),
		WithTransform(func(ref metav1.OwnerReference) types.UID { return ref.UID }, Equal(dsc.UID)),
	))
}
