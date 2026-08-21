package workbenches_test

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	metadataannotations "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	workbenchesgate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

type workbenchesGateTestCtx struct {
	cli client.Client
}

func TestWorkbenchesGates(t *testing.T) {
	te, err := envt.New()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		_ = te.Stop()
	})
	if err := installWorkbenchesGateCRDs(t.Context(), te); err != nil {
		t.Fatalf("install Workbenches gate CRDs: %v", err)
	}

	tc := &workbenchesGateTestCtx{cli: te.Client()}

	t.Run("clean cluster passes", tc.testCleanClusterPasses)
	t.Run("notebook with existing hardware profile passes", tc.testNotebookWithExistingHardwareProfilePasses)
	t.Run("notebook with missing hardware profile blocks", tc.testNotebookWithMissingHardwareProfileBlocks)
	t.Run("notebook with existing connections passes", tc.testNotebookWithExistingConnectionsPasses)
	t.Run("notebook with missing connection blocks", tc.testNotebookWithMissingConnectionBlocks)
	t.Run("dashboard-managed notebook with matching container name passes", tc.testDashboardManagedNotebookWithMatchingContainerNamePasses)
	t.Run("dashboard-managed notebook with mismatched container name blocks", tc.testDashboardManagedNotebookWithMismatchedContainerNameBlocks)
}

func (tc *workbenchesGateTestCtx) testCleanClusterPasses(t *testing.T) {
	g := NewWithT(t)

	err := workbenchesgate.Check(t.Context(), tc.cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *workbenchesGateTestCtx) testNotebookWithExistingHardwareProfilePasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	namespace := "workbenches-hwp-present"

	g.Expect(tc.cli.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	})).ToNot(HaveOccurred())

	notebook := renderNotebook(t, map[string]any{
		"Name":      "migrated-notebook",
		"Namespace": namespace,
		"Annotations": map[string]string{
			hwpNameAnnotation:    "gpu-small-notebooks",
			hwpNamespaceAnnotion: namespace,
		},
	})
	hwp := renderHardwareProfile(t, map[string]any{
		"Name":      "gpu-small-notebooks",
		"Namespace": namespace,
	})
	g.Expect(tc.cli.Create(ctx, hwp)).ToNot(HaveOccurred())
	g.Expect(tc.cli.Create(ctx, notebook)).ToNot(HaveOccurred())
	cleanupObject(t, g, tc.cli, hwp)
	cleanupObject(t, g, tc.cli, notebook)

	err := workbenchesgate.Check(ctx, tc.cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *workbenchesGateTestCtx) testNotebookWithExistingConnectionsPasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	namespace := "workbenches-conn-present"

	g.Expect(tc.cli.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	})).ToNot(HaveOccurred())

	notebook := renderNotebook(t, map[string]any{
		"Name":      "connected-notebook",
		"Namespace": namespace,
		"Annotations": map[string]string{
			metadataannotations.Connection: "data-conn",
		},
	})
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "data-conn", Namespace: namespace}}
	g.Expect(tc.cli.Create(ctx, secret)).ToNot(HaveOccurred())
	g.Expect(tc.cli.Create(ctx, notebook)).ToNot(HaveOccurred())
	cleanupObject(t, g, tc.cli, secret)
	cleanupObject(t, g, tc.cli, notebook)

	err := workbenchesgate.Check(ctx, tc.cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *workbenchesGateTestCtx) testNotebookWithMissingHardwareProfileBlocks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	namespace := "workbenches-hwp-missing"

	g.Expect(tc.cli.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	})).ToNot(HaveOccurred())

	notebook := renderNotebook(t, map[string]any{
		"Name":      "broken-notebook",
		"Namespace": namespace,
		"Annotations": map[string]string{
			hwpNameAnnotation:    "missing-hwp",
			hwpNamespaceAnnotion: namespace,
		},
	})
	g.Expect(tc.cli.Create(ctx, notebook)).ToNot(HaveOccurred())
	cleanupObject(t, g, tc.cli, notebook)

	err := workbenchesgate.Check(ctx, tc.cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).To(HaveOccurred())

	var blockingErr *workbenchesgate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.NotebooksWithBrokenHardwareProfileRefs).To(Equal(1))
	g.Expect(blockingErr.NotebooksWithBrokenConnectionRefs).To(Equal(0))
}

func (tc *workbenchesGateTestCtx) testNotebookWithMissingConnectionBlocks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	namespace := "workbenches-conn-missing"

	g.Expect(tc.cli.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	})).ToNot(HaveOccurred())

	notebook := renderNotebook(t, map[string]any{
		"Name":      "broken-connection-notebook",
		"Namespace": namespace,
		"Annotations": map[string]string{
			metadataannotations.Connection: "missing-secret",
		},
	})
	g.Expect(tc.cli.Create(ctx, notebook)).ToNot(HaveOccurred())
	cleanupObject(t, g, tc.cli, notebook)

	err := workbenchesgate.Check(ctx, tc.cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).To(HaveOccurred())

	var blockingErr *workbenchesgate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.NotebooksWithBrokenHardwareProfileRefs).To(Equal(0))
	g.Expect(blockingErr.NotebooksWithBrokenConnectionRefs).To(Equal(1))
	g.Expect(blockingErr.NotebooksWithContainerNameMismatch).To(Equal(0))
}

func (tc *workbenchesGateTestCtx) testDashboardManagedNotebookWithMatchingContainerNamePasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	namespace := "workbenches-name-match"

	g.Expect(tc.cli.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	})).ToNot(HaveOccurred())

	notebook := renderNotebook(t, map[string]any{
		"Name":      "dashboard-notebook",
		"Namespace": namespace,
		"Annotations": map[string]string{
			lastSizeAnnotation: "Small",
		},
	})
	setNotebookContainers(t, notebook,
		container("dashboard-notebook", "jupyter:latest"),
		container("oauth-proxy", "registry/ose-oauth-proxy-rhel9:latest"),
	)
	g.Expect(tc.cli.Create(ctx, notebook)).ToNot(HaveOccurred())
	cleanupObject(t, g, tc.cli, notebook)

	err := workbenchesgate.Check(ctx, tc.cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *workbenchesGateTestCtx) testDashboardManagedNotebookWithMismatchedContainerNameBlocks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	namespace := "workbenches-name-mismatch"

	g.Expect(tc.cli.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	})).ToNot(HaveOccurred())

	notebook := renderNotebook(t, map[string]any{
		"Name":      "dashboard-notebook",
		"Namespace": namespace,
		"Annotations": map[string]string{
			acceleratorAnnotation: "gpu-profile",
		},
	})
	setNotebookContainers(t, notebook, container("legacy-name", "jupyter:latest"))
	g.Expect(tc.cli.Create(ctx, notebook)).ToNot(HaveOccurred())
	cleanupObject(t, g, tc.cli, notebook)

	err := workbenchesgate.Check(ctx, tc.cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).To(HaveOccurred())

	var blockingErr *workbenchesgate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.NotebooksWithBrokenHardwareProfileRefs).To(Equal(0))
	g.Expect(blockingErr.NotebooksWithBrokenConnectionRefs).To(Equal(0))
	g.Expect(blockingErr.NotebooksWithContainerNameMismatch).To(Equal(1))
}

func installWorkbenchesGateCRDs(ctx context.Context, te *envt.EnvT) error {
	for _, crd := range []struct {
		gvk      schema.GroupVersionKind
		plural   string
		singular string
	}{
		{gvk: gvk.Notebook, plural: "notebooks", singular: "notebook"},
		{gvk: gvk.HardwareProfile, plural: "hardwareprofiles", singular: "hardwareprofile"},
	} {
		if _, err := te.RegisterCRD(
			ctx,
			crd.gvk,
			crd.plural,
			crd.singular,
			apiextensionsv1.NamespaceScoped,
			envt.WithPermissiveSchema(),
		); err != nil {
			return err
		}
	}

	return nil
}

func cleanupObject(t *testing.T, g *WithT, cli client.Client, obj client.Object) {
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
