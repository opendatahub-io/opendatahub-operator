package modelmeshserving_test

import (
	"context"
	"errors"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	modelmeshgate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

type modelMeshServingGateTestCtx struct {
	cli client.Client
}

func TestModelMeshServingGates(t *testing.T) {
	te, err := envt.New()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		_ = te.Stop()
	})
	if err := installModelMeshServingCRD(t.Context(), te); err != nil {
		t.Fatalf("install ModelMeshServing CRD: %v", err)
	}

	tc := &modelMeshServingGateTestCtx{cli: te.Client()}

	t.Run("clean cluster passes", tc.testCleanClusterPasses)
	t.Run("ModelMeshServing CR blocks", tc.testModelMeshServingCRBlocks)
}

func (tc *modelMeshServingGateTestCtx) testCleanClusterPasses(t *testing.T) {
	g := NewWithT(t)

	err := modelmeshgate.Check(t.Context(), tc.cli, componentApi.ModelMeshServingComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *modelMeshServingGateTestCtx) testModelMeshServingCRBlocks(t *testing.T) {
	g := NewWithT(t)

	obj := renderModelMeshServing(t)

	g.Expect(tc.cli.Create(t.Context(), obj)).ToNot(HaveOccurred())
	envt.CleanupDelete(t, g, context.Background(), tc.cli, obj)

	err := modelmeshgate.Check(t.Context(), tc.cli, componentApi.ModelMeshServingComponentName, "")
	g.Expect(err).To(HaveOccurred())

	var blockingErr *modelmeshgate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
}

func installModelMeshServingCRD(ctx context.Context, te *envt.EnvT) error {
	_, err := te.RegisterCRD(
		ctx,
		gvk.ModelMeshServing,
		"modelmeshservings",
		"modelmeshserving",
		apiextensionsv1.ClusterScoped,
		envt.WithPermissiveSchema(),
	)
	return err
}
