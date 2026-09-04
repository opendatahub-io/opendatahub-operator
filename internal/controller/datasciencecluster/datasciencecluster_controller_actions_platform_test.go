//nolint:testpackage
package datasciencecluster

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestSyncPlatformCR_MergesNonControllerOwner(t *testing.T) {
	g := NewWithT(t)

	dsc := newDSC()
	dsc.SetUID(types.UID("dsc-uid"))
	cli, err := fakeclient.New(fakeclient.WithObjects(
		modules.NewPlatformCR(&modules.DSCContext{DSC: dsc}, modules.ConfigFromDSC),
	))
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &odhtype.ReconciliationRequest{
		Client:   cli,
		Instance: dsc,
	}

	g.Expect(syncPlatformCR(t.Context(), rr)).Should(Succeed())

	platform := &configv1alpha1.Platform{}
	g.Expect(cli.Get(t.Context(), client.ObjectKey{Name: configv1alpha1.PlatformInstanceName}, platform)).Should(Succeed())
	g.Expect(platform.GetOwnerReferences()).Should(HaveLen(1))
	g.Expect(platform.GetOwnerReferences()[0].Kind).Should(Equal(gvk.DataScienceCluster.Kind))
	g.Expect(platform.GetOwnerReferences()[0].UID).Should(Equal(dsc.UID))
	g.Expect(platform.GetOwnerReferences()[0].Controller).Should(BeNil())
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
