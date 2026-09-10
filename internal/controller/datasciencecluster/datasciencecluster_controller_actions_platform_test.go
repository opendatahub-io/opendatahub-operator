//nolint:testpackage
package datasciencecluster

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestSyncPlatformCR_SetsNonControllerOwner(t *testing.T) {
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dsc := newDSC()
	dsc.SetUID(types.UID("dsc-uid"))
	rr := &odhtype.ReconciliationRequest{
		Client:   cli,
		Instance: dsc,
	}

	g.Expect(syncPlatformCR(t.Context(), rr)).Should(Succeed())
	g.Expect(rr.Resources).Should(HaveLen(1))

	platform := rr.Resources[0]
	g.Expect(&platform).Should(And(
		jq.Match(`.kind == "%s"`, gvk.Platform.Kind),
		jq.Match(`.metadata.ownerReferences | length == 1`),
		jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
		jq.Match(`.metadata.ownerReferences[0].uid == "%s"`, dsc.UID),
		jq.Match(`(.metadata.ownerReferences[0].controller // false) == false`),
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
