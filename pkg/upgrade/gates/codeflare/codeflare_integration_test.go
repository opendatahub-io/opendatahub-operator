package codeflare_test

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	t.Run("AppWrapper blocks", tc.testAppWrapperBlocks)
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
	g.Expect(blockingErr.AppWrappers).To(Equal(0))
}

func (tc *codeFlareGateTestCtx) testAppWrapperBlocks(t *testing.T) {
	g := NewWithT(t)

	g.Expect(tc.cli.Create(t.Context(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})).ToNot(HaveOccurred())

	obj := renderAppWrapper(t, "test-appwrapper", "test-ns")

	g.Expect(tc.cli.Create(t.Context(), obj)).ToNot(HaveOccurred())
	envt.CleanupDelete(t, g, context.Background(), tc.cli, obj)

	err := codeflaregate.Check(t.Context(), tc.cli, componentApi.CodeFlareComponentName, "")
	g.Expect(err).To(HaveOccurred())

	var blockingErr *codeflaregate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.CodeFlareCRPresent).To(BeFalse())
	g.Expect(blockingErr.AppWrappers).To(Equal(1))
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

	_, err := te.RegisterCRD(
		ctx,
		gvk.AppWrapper,
		"appwrappers",
		"appwrapper",
		apiextensionsv1.NamespaceScoped,
		envt.WithPermissiveSchema(),
	)
	return err
}
