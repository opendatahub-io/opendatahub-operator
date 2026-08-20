package trustyai_test

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	trustyaigate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/trustyai"
	tp "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

type trustyAIGateTestCtx struct {
	cli client.Client
}

func TestTrustyAIGates(t *testing.T) {
	te, err := envt.New()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		_ = te.Stop()
	})
	if err := installTrustyAIGateCRD(t.Context(), te); err != nil {
		t.Fatalf("install TrustyAIService CRD: %v", err)
	}

	tc := &trustyAIGateTestCtx{cli: te.Client()}

	t.Run("clean cluster passes", tc.testCleanClusterPasses)
	t.Run("TrustyAIService with PVC storage blocks", tc.testTrustyAIServiceWithPVCStorageBlocks)
	t.Run("TrustyAIService with database storage passes", tc.testTrustyAIServiceWithDatabaseStoragePasses)
}

func (tc *trustyAIGateTestCtx) testCleanClusterPasses(t *testing.T) {
	g := NewWithT(t)

	err := trustyaigate.Check(t.Context(), tc.cli, componentApi.TrustyAIComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *trustyAIGateTestCtx) testTrustyAIServiceWithPVCStorageBlocks(t *testing.T) {
	g := NewWithT(t)
	namespace := "trustyai-gate-pvc"

	obj, err := tp.RenderObject(resourcesFS, "resources/trustyaiservice.tmpl.yaml", map[string]any{
		"APIVersion":    "trustyai.opendatahub.io/v1",
		"Name":          "trustyai-pvc",
		"Namespace":     namespace,
		"StorageFormat": "PVC",
	})
	g.Expect(err).ToNot(HaveOccurred())

	tc.assertBlockingCase(t, namespace, obj, 1)
}

func (tc *trustyAIGateTestCtx) testTrustyAIServiceWithDatabaseStoragePasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	namespace := "trustyai-gate-db"

	g.Expect(tc.cli.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	})).ToNot(HaveOccurred())

	obj, err := tp.RenderObject(resourcesFS, "resources/trustyaiservice.tmpl.yaml", map[string]any{
		"APIVersion":    "trustyai.opendatahub.io/v1",
		"Name":          "trustyai-db",
		"Namespace":     namespace,
		"StorageFormat": "DATABASE",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(tc.cli.Create(ctx, obj)).ToNot(HaveOccurred())
	cleanupTrustyAIService(t, g, tc.cli, obj)

	err = trustyaigate.Check(ctx, tc.cli, componentApi.TrustyAIComponentName, namespace)
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *trustyAIGateTestCtx) assertBlockingCase(
	t *testing.T,
	namespace string,
	obj client.Object,
	expectedBlocked int,
) {
	t.Helper()

	g := NewWithT(t)
	ctx := t.Context()

	g.Expect(tc.cli.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	})).ToNot(HaveOccurred())
	g.Expect(tc.cli.Create(ctx, obj)).ToNot(HaveOccurred())
	cleanupTrustyAIService(t, g, tc.cli, obj)

	err := trustyaigate.Check(ctx, tc.cli, componentApi.TrustyAIComponentName, namespace)
	g.Expect(err).To(HaveOccurred())

	var blockingErr *trustyaigate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.PVCStorageTrustyAIServices).To(Equal(expectedBlocked))
}

func installTrustyAIGateCRD(ctx context.Context, te *envt.EnvT) error {
	_, err := te.RegisterCRD(
		ctx,
		gvk.TrustyAIServiceV1,
		"trustyaiservices",
		"trustyaiservice",
		apiextensionsv1.NamespaceScoped,
		envt.WithPermissiveSchema(),
	)
	return err
}

func cleanupTrustyAIService(t *testing.T, g *WithT, cli client.Client, obj client.Object) {
	t.Helper()

	t.Cleanup(func() {
		key := client.ObjectKeyFromObject(obj)
		current, ok := obj.DeepCopyObject().(client.Object)
		g.Expect(ok).To(BeTrue())

		err := cli.Get(context.Background(), key, current)
		if k8serr.IsNotFound(err) {
			return
		}
		g.Expect(err).ToNot(HaveOccurred())
		err = cli.Delete(context.Background(), current)
		if err != nil && !k8serr.IsNotFound(err) {
			g.Expect(err).ToNot(HaveOccurred())
		}
		g.Eventually(func() error {
			return cli.Get(context.Background(), key, current)
		}).WithTimeout(envt.DefaultMaxWait).WithPolling(envt.DefaultPollInterval).Should(
			MatchError(k8serr.IsNotFound, "IsNotFound"),
		)
	})
}
