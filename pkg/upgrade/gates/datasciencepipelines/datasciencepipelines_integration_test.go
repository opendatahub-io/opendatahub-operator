package datasciencepipelines_test

import (
	"context"
	"errors"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dspgate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

type dataSciencePipelinesGateTestCtx struct {
	cli client.Client
}

func TestDataSciencePipelinesGates(t *testing.T) {
	te, err := envt.New()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		_ = te.Stop()
	})

	tc := &dataSciencePipelinesGateTestCtx{cli: te.Client()}

	t.Run("clean cluster passes", tc.testCleanClusterPasses)
	t.Run("stored version removed passes", tc.testStoredVersionRemovedPasses)
	t.Run("stored version present blocks", tc.testStoredVersionPresentBlocks)
}

func (tc *dataSciencePipelinesGateTestCtx) testCleanClusterPasses(t *testing.T) {
	g := NewWithT(t)

	err := dspgate.Check(t.Context(), tc.cli, componentApi.DataSciencePipelinesComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *dataSciencePipelinesGateTestCtx) testStoredVersionRemovedPasses(t *testing.T) {
	g := NewWithT(t)

	crd := newDSPACRD("v1")
	createDSPACRD(t, g, tc.cli, crd)
	defer deleteCRD(g, tc.cli, crd)

	err := dspgate.Check(t.Context(), tc.cli, componentApi.DataSciencePipelinesComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *dataSciencePipelinesGateTestCtx) testStoredVersionPresentBlocks(t *testing.T) {
	g := NewWithT(t)

	crd := newDSPACRD("v1alpha1", "v1")
	createDSPACRD(t, g, tc.cli, crd)
	defer deleteCRD(g, tc.cli, crd)

	err := dspgate.Check(t.Context(), tc.cli, componentApi.DataSciencePipelinesComponentName, "")
	g.Expect(err).To(HaveOccurred())

	var blockingErr *dspgate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.StoredVersion).To(Equal("v1alpha1"))
}

func createDSPACRD(
	_ *testing.T,
	g *WithT,
	cli client.Client,
	crd *apiextensionsv1.CustomResourceDefinition,
) {
	g.Expect(cli.Create(context.Background(), crd)).ToNot(HaveOccurred())
}

func deleteCRD(g *WithT, cli client.Client, crd *apiextensionsv1.CustomResourceDefinition) {
	key := client.ObjectKeyFromObject(crd)

	err := cli.Delete(context.Background(), crd)
	g.Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

	g.Eventually(func() error {
		current := &apiextensionsv1.CustomResourceDefinition{}
		return cli.Get(context.Background(), key, current)
	}).WithTimeout(envt.DefaultMaxWait).WithPolling(envt.DefaultPollInterval).Should(
		Satisfy(k8serr.IsNotFound),
	)
}
