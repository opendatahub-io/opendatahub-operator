package upgrade_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestAttachHardwareProfileToNotebooks(t *testing.T) {
	ctx := t.Context()
	namespace := "test-namespace"

	t.Run("should migrate AP annotation to HWP annotation", func(t *testing.T) {
		runNotebookHWPMigrationTest(t, ctx, namespace, "notebook-with-ap",
			map[string]string{"opendatahub.io/accelerator-name": "nvidia-gpu"},
			"nvidia-gpu-notebooks")
	})

	t.Run("should migrate valid container size annotation to HWP annotation", func(t *testing.T) {
		runNotebookHWPMigrationTest(t, ctx, namespace, "notebook-with-size",
			map[string]string{"notebooks.opendatahub.io/last-size-selection": "X Large"},
			"containersize-x-large-notebooks")
	})

	t.Run("should not migrate invalid container size annotation", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, namespace)
		notebook := createTestNotebook(namespace, "notebook-with-invalid-size")
		notebook.SetAnnotations(map[string]string{
			"notebooks.opendatahub.io/last-size-selection": "InvalidSize",
		})

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, notebook))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToNotebooks(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HWP annotation was NOT added (original annotation remains)
		updatedNotebook := &unstructured.Unstructured{}
		updatedNotebook.SetGroupVersionKind(gvk.Notebook)
		err = cli.Get(ctx, client.ObjectKey{Name: "notebook-with-invalid-size", Namespace: namespace}, updatedNotebook)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedNotebook.GetAnnotations()).ToNot(HaveKey("opendatahub.io/hardware-profile-name"))
		g.Expect(updatedNotebook.GetAnnotations()).To(HaveKeyWithValue("notebooks.opendatahub.io/last-size-selection", "InvalidSize"))
	})

	t.Run("should handle multiple notebooks with mixed scenarios", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, namespace)
		notebook1 := createTestNotebook(namespace, "notebook-ap")
		notebook1.SetAnnotations(map[string]string{
			"opendatahub.io/accelerator-name": "gpu-1",
		})

		notebook2 := createTestNotebook(namespace, "notebook-size")
		notebook2.SetAnnotations(map[string]string{
			"notebooks.opendatahub.io/last-size-selection": "Medium",
		})

		notebook3 := createTestNotebook(namespace, "notebook-existing-hwp")
		notebook3.SetAnnotations(map[string]string{
			"opendatahub.io/hardware-profile-name": "already-set",
		})

		// Create the HardwareProfiles that the migration expects to find
		hwp1 := createTestHardwareProfile(namespace, "gpu-1-notebooks")
		hwp2 := createTestHardwareProfile(namespace, "containersize-medium-notebooks")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, notebook1, notebook2, notebook3, hwp1, hwp2))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToNotebooks(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify first notebook
		nb1 := &unstructured.Unstructured{}
		nb1.SetGroupVersionKind(gvk.Notebook)
		err = cli.Get(ctx, client.ObjectKey{Name: "notebook-ap", Namespace: namespace}, nb1)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(nb1.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "gpu-1-notebooks"))

		// Verify second notebook
		nb2 := &unstructured.Unstructured{}
		nb2.SetGroupVersionKind(gvk.Notebook)
		err = cli.Get(ctx, client.ObjectKey{Name: "notebook-size", Namespace: namespace}, nb2)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(nb2.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "containersize-medium-notebooks"))

		// Verify third notebook
		nb3 := &unstructured.Unstructured{}
		nb3.SetGroupVersionKind(gvk.Notebook)
		err = cli.Get(ctx, client.ObjectKey{Name: "notebook-existing-hwp", Namespace: namespace}, nb3)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(nb3.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "already-set"))
	})

	t.Run("should handle no notebooks gracefully", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, namespace)
		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToNotebooks(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	// RHOAIENG-50667: skip HWP migration only when namespace is Kueue-managed AND workload lacks queue label
	t.Run("should skip Notebook in Kueue-managed namespace when notebook is missing queue label", func(t *testing.T) {
		g := NewWithT(t)

		ns := createTestKueueManagedNamespace(namespace)
		odhConfig := createTestOdhDashboardConfig(t, namespace)
		notebook := createTestNotebook(namespace, "notebook-kueue-ns-no-label")
		notebook.SetAnnotations(map[string]string{"opendatahub.io/accelerator-name": "nvidia-gpu"})
		// No kueue.x-k8s.io/queue-name label on notebook
		hwp := createTestHardwareProfile(namespace, "nvidia-gpu-notebooks")

		cli, err := fakeclient.New(fakeclient.WithObjects(ns, odhConfig, notebook, hwp))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToNotebooks(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Notebook should NOT get HWP annotation (skipped to avoid webhook rejection)
		updatedNotebook := &unstructured.Unstructured{}
		updatedNotebook.SetGroupVersionKind(gvk.Notebook)
		err = cli.Get(ctx, client.ObjectKey{Name: "notebook-kueue-ns-no-label", Namespace: namespace}, updatedNotebook)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedNotebook.GetAnnotations()).ToNot(HaveKey("opendatahub.io/hardware-profile-name"))
	})

	t.Run("should migrate Notebook in Kueue-managed namespace when notebook has queue label", func(t *testing.T) {
		g := NewWithT(t)

		ns := createTestKueueManagedNamespace(namespace)
		odhConfig := createTestOdhDashboardConfig(t, namespace)
		notebook := createTestNotebook(namespace, "notebook-kueue-ns-with-label")
		notebook.SetAnnotations(map[string]string{"opendatahub.io/accelerator-name": "nvidia-gpu"})
		notebook.SetLabels(map[string]string{cluster.KueueQueueNameLabel: "my-queue"})
		hwp := createTestHardwareProfile(namespace, "nvidia-gpu-notebooks")

		cli, err := fakeclient.New(fakeclient.WithObjects(ns, odhConfig, notebook, hwp))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToNotebooks(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		updatedNotebook := &unstructured.Unstructured{}
		updatedNotebook.SetGroupVersionKind(gvk.Notebook)
		err = cli.Get(ctx, client.ObjectKey{Name: "notebook-kueue-ns-with-label", Namespace: namespace}, updatedNotebook)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedNotebook.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "nvidia-gpu-notebooks"))
	})

	t.Run("should migrate Notebook in non-Kueue namespace without queue label", func(t *testing.T) {
		g := NewWithT(t)

		// Namespace exists but has no Kueue label → migration should proceed
		ns := createTestNamespace(namespace)
		odhConfig := createTestOdhDashboardConfig(t, namespace)
		notebook := createTestNotebook(namespace, "notebook-non-kueue-ns")
		notebook.SetAnnotations(map[string]string{"opendatahub.io/accelerator-name": "nvidia-gpu"})
		// No queue label on notebook; namespace is not Kueue-managed so we still migrate
		hwp := createTestHardwareProfile(namespace, "nvidia-gpu-notebooks")

		cli, err := fakeclient.New(fakeclient.WithObjects(ns, odhConfig, notebook, hwp))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToNotebooks(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		updatedNotebook := &unstructured.Unstructured{}
		updatedNotebook.SetGroupVersionKind(gvk.Notebook)
		err = cli.Get(ctx, client.ObjectKey{Name: "notebook-non-kueue-ns", Namespace: namespace}, updatedNotebook)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedNotebook.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "nvidia-gpu-notebooks"))
	})

	t.Run("should skip Notebook in namespace with legacy Kueue label when notebook is missing queue label", func(t *testing.T) {
		g := NewWithT(t)

		// Legacy label: kueue-managed = "true" (same behavior as kueue.openshift.io/managed)
		ns := createTestKueueManagedNamespaceLegacy(namespace)
		odhConfig := createTestOdhDashboardConfig(t, namespace)
		notebook := createTestNotebook(namespace, "notebook-legacy-kueue-ns")
		notebook.SetAnnotations(map[string]string{"opendatahub.io/accelerator-name": "nvidia-gpu"})
		hwp := createTestHardwareProfile(namespace, "nvidia-gpu-notebooks")

		cli, err := fakeclient.New(fakeclient.WithObjects(ns, odhConfig, notebook, hwp))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToNotebooks(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		updatedNotebook := &unstructured.Unstructured{}
		updatedNotebook.SetGroupVersionKind(gvk.Notebook)
		err = cli.Get(ctx, client.ObjectKey{Name: "notebook-legacy-kueue-ns", Namespace: namespace}, updatedNotebook)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedNotebook.GetAnnotations()).ToNot(HaveKey("opendatahub.io/hardware-profile-name"))
	})
}
