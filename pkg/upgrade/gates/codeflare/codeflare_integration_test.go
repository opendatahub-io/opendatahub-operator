package codeflare_test

import (
	"context"
	"errors"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	codeflaregate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/codeflare"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

type codeFlareGateTestCtx struct {
	cli client.Client
}

func TestCodeFlareGates(t *testing.T) {
	te, err := envt.New()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		_ = te.Stop()
	})
	if err := installCodeFlareCRD(t.Context(), te); err != nil {
		t.Fatalf("install CodeFlare CRD: %v", err)
	}

	tc := &codeFlareGateTestCtx{cli: te.Client()}

	t.Run("clean cluster passes", tc.testCleanClusterPasses)
	t.Run("CodeFlare CR blocks", tc.testCodeFlareCRBlocks)
}

func (tc *codeFlareGateTestCtx) testCleanClusterPasses(t *testing.T) {
	g := NewWithT(t)

	err := codeflaregate.Check(t.Context(), tc.cli, componentApi.CodeFlareComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *codeFlareGateTestCtx) testCodeFlareCRBlocks(t *testing.T) {
	g := NewWithT(t)

	obj := renderCodeFlare(t)

	g.Expect(tc.cli.Create(t.Context(), obj)).ToNot(HaveOccurred())
	envt.CleanupDelete(t, g, context.Background(), tc.cli, obj)

	err := codeflaregate.Check(t.Context(), tc.cli, componentApi.CodeFlareComponentName, "")
	g.Expect(err).To(HaveOccurred())

	var blockingErr *codeflaregate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.CodeFlareCRPresent).To(BeTrue())
}

func installCodeFlareCRD(ctx context.Context, te *envt.EnvT) error {
	if _, err := te.RegisterCRD(
		ctx,
		gvk.CodeFlare,
		"codeflares",
		"codeflare",
		apiextensionsv1.ClusterScoped,
		envt.WithPermissiveSchema(),
	); err != nil {
		return err
	}

	return nil
}
