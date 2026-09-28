//nolint:testpackage
package datasciencecluster

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	testscheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestSyncPlatformCRPreservesExistingOwner(t *testing.T) {
	g := NewWithT(t)

	s, err := testscheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dsc := newDSC()
	dsc.SetUID(types.UID("dsc-uid"))
	dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{
		Name: "default-dsci",
		UID:  types.UID("dsci-uid"),
	}}
	existingPlatform := modules.NewPlatformCR(&modules.DSCContext{DSC: dsc}, modules.ConfigFromDSC)
	g.Expect(controllerutil.SetOwnerReference(dsci, existingPlatform, s)).Should(Succeed())
	cli, err := fakeclient.New(fakeclient.WithObjects(
		existingPlatform,
	))
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &odhtype.ReconciliationRequest{
		Client:   cli,
		Instance: dsc,
	}

	g.Expect(syncPlatformCR(t.Context(), rr)).Should(Succeed())

	foundPlatform := &configv1alpha1.Platform{}
	g.Expect(cli.Get(t.Context(), client.ObjectKey{Name: configv1alpha1.PlatformInstanceName}, foundPlatform)).Should(Succeed())
	g.Expect(foundPlatform.GetOwnerReferences()).Should(ContainElements(
		WithTransform(func(ref metav1.OwnerReference) types.UID { return ref.UID }, Equal(dsci.UID)),
		WithTransform(func(ref metav1.OwnerReference) types.UID { return ref.UID }, Equal(dsc.UID)),
	))
}

func TestDisableDSCModulesOnDelete_AppliesPlatform(t *testing.T) {
	g := NewWithT(t)

	applied := false
	cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		Apply: func(_ context.Context, _ client.WithWatch, _ runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
			applied = true
			return nil
		},
	}))
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &odhtype.ReconciliationRequest{
		Client:   cli,
		Instance: newDSC(),
	}

	g.Expect(disableDSCModulesOnDelete(t.Context(), rr)).Should(Succeed())
	g.Expect(applied).Should(BeTrue())
}
