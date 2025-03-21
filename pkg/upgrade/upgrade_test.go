package upgrade_test

// TODO: to be removed: https://issues.redhat.com/browse/RHOAIENG-21080
import (
	"context"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/api/pkg/lib/version"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func createOdhDashboardConfig() *unstructured.Unstructured {
	dashboardConfig := &unstructured.Unstructured{}
	dashboardConfig.Object = map[string]interface{}{
		"spec": map[string]any{},
	}
	dashboardConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)
	dashboardConfig.SetName("test-dashboard")
	dashboardConfig.SetNamespace("test-namespace")
	return dashboardConfig
}

func TestPatchOdhDashboardConfig(t *testing.T) {
	ctx := context.Background()
	releaseV1 := common.Release{Version: version.OperatorVersion{Version: semver.MustParse("1.0.0")}}
	releaseV2 := common.Release{Version: version.OperatorVersion{Version: semver.MustParse("1.1.0")}}
	t.Run("should skip patch if current version is not greated than previous version", func(t *testing.T) {
		g := NewWithT(t)

		dashboardConfig := resources.GvkToUnstructured(gvk.OdhDashboardConfig)
		cli, err := fakeclient.New(dashboardConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.PatchOdhDashboardConfig(
			ctx,
			cli,
			releaseV1,
			releaseV1,
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		updatedConfig := resources.GvkToUnstructured(gvk.OdhDashboardConfig)
		err = cli.Get(ctx, client.ObjectKey{Name: dashboardConfig.GetName(), Namespace: dashboardConfig.GetNamespace()}, updatedConfig)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedConfig.Object).To(Equal(dashboardConfig.Object), "Expected OdhDashboardConfig object to remain unchanged")
	})

	t.Run("should return error if fetching OdhDashboardConfig fails", func(t *testing.T) {
		g := NewWithT(t)

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.PatchOdhDashboardConfig(
			ctx,
			cli,
			releaseV1,
			releaseV2,
		)
		g.Expect(err).ToNot(HaveOccurred(), "The CRD is not installed, hence skipping")
	})

	t.Run("should return error if updateSpecFields fails", func(t *testing.T) {
		g := NewWithT(t)

		dashboardConfig := createOdhDashboardConfig()
		err := unstructured.SetNestedField(dashboardConfig.Object, "invalid_type", "spec")
		g.Expect(err).ShouldNot(HaveOccurred())

		cli, err := fakeclient.New(dashboardConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.PatchOdhDashboardConfig(
			ctx,
			cli,
			releaseV1,
			releaseV2,
		)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("failed to update odhdashboardconfig spec fields"))
	})

	t.Run("should skip patch if no changes are needed", func(t *testing.T) {
		g := NewWithT(t)

		dashboardConfig := createOdhDashboardConfig()
		expectedNotebookSizes := []any{
			map[string]any{"size": "Small", "cpu": "1", "memory": "2Gi"},
			map[string]any{"size": "Medium", "cpu": "2", "memory": "4Gi"},
		}
		expectedModelServerSizes := []any{
			map[string]any{"size": "Small", "cpu": "2", "memory": "4Gi"},
		}
		err := unstructured.SetNestedSlice(dashboardConfig.Object, expectedNotebookSizes, "spec", "notebookSizes")
		g.Expect(err).ShouldNot(HaveOccurred())
		err = unstructured.SetNestedSlice(dashboardConfig.Object, expectedModelServerSizes, "spec", "modelServerSizes")
		g.Expect(err).ShouldNot(HaveOccurred())

		cli, err := fakeclient.New(dashboardConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.PatchOdhDashboardConfig(ctx, cli, releaseV1, releaseV2)
		g.Expect(err).ShouldNot(HaveOccurred())

		updatedConfig := resources.GvkToUnstructured(gvk.OdhDashboardConfig)
		err = cli.Get(ctx, client.ObjectKey{Name: dashboardConfig.GetName(), Namespace: dashboardConfig.GetNamespace()}, updatedConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		notebookSizes, exists, err := unstructured.NestedSlice(updatedConfig.Object, "spec", "notebookSizes")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(exists).To(BeTrue(), "Expected 'notebookSizes' field to be set")
		g.Expect(notebookSizes).To(HaveLen(2), "Expected 'notebookSizes' to remain unchanged")

		modelServerSizes, modelServerExists, err := unstructured.NestedSlice(updatedConfig.Object, "spec", "modelServerSizes")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(modelServerExists).To(BeTrue(), "Expected 'modelServerSizes' field to be set")
		g.Expect(modelServerSizes).To(HaveLen(1), "Expected 'modelServerSizes' to remain unchanged")
	})

	t.Run("should patch OdhDashboardConfig if changes are needed", func(t *testing.T) {
		g := NewWithT(t)

		dashboardConfig := createOdhDashboardConfig()
		cli, err := fakeclient.New(dashboardConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.PatchOdhDashboardConfig(
			ctx,
			cli,
			releaseV1,
			releaseV2,
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		updatedConfig := resources.GvkToUnstructured(gvk.OdhDashboardConfig)
		err = cli.Get(ctx, client.ObjectKey{Name: dashboardConfig.GetName(), Namespace: dashboardConfig.GetNamespace()}, updatedConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		notebookSizes, noteBookexists, err := unstructured.NestedSlice(updatedConfig.Object, "spec", "notebookSizes")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(noteBookexists).To(BeTrue(), "Expected 'notebookSizes' field to be set")
		g.Expect(notebookSizes).ToNot(BeEmpty(), "Expected 'notebookSizes' to have values")
		g.Expect(notebookSizes).To(Equal(upgrade.NotebookSizesData), "Expected 'notebookSizes' to match expected values")

		modelServerSizes, modelServerExists, err := unstructured.NestedSlice(updatedConfig.Object, "spec", "modelServerSizes")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(modelServerExists).To(BeTrue(), "Expected 'modelServerSizes' field to be set")
		g.Expect(modelServerSizes).ToNot(BeEmpty(), "Expected 'modelServerSizes' to have values")
		g.Expect(modelServerSizes).To(Equal(upgrade.ModelServerSizeData), "Expected 'modelServerSizes' to match expected values")
	})
}
