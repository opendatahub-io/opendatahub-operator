package upgrade_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestAttachHardwareProfileToInferenceServices(t *testing.T) {
	ctx := t.Context()
	namespace := "test-namespace"

	t.Run("should migrate AP annotation from ServingRuntime to InferenceService", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, namespace)
		servingRuntime := createTestServingRuntime(namespace, "test-runtime")
		servingRuntime.SetAnnotations(map[string]string{
			"opendatahub.io/accelerator-name": "nvidia Gpu",
		})

		isvc := createTestInferenceService(namespace, "isvc-with-runtime", "test-runtime")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, servingRuntime, isvc))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HWP annotation was added to InferenceService
		updatedIsvc := &unstructured.Unstructured{}
		updatedIsvc.SetGroupVersionKind(gvk.InferenceServices)
		err = cli.Get(ctx, client.ObjectKey{Name: "isvc-with-runtime", Namespace: namespace}, updatedIsvc)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedIsvc.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "nvidia-gpu-serving"))
	})

	t.Run("should match container size for InferenceService without AP", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, namespace)
		isvc := createTestInferenceServiceWithResources(namespace, "isvc-with-resources",
			"1", "4Gi", "2", "8Gi")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, isvc))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HWP annotation matches container size
		updatedIsvc := &unstructured.Unstructured{}
		updatedIsvc.SetGroupVersionKind(gvk.InferenceServices)
		err = cli.Get(ctx, client.ObjectKey{Name: "isvc-with-resources", Namespace: namespace}, updatedIsvc)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedIsvc.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "containersize-small-serving"))
	})

	t.Run("should use custom-serving for non-matching resources", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, namespace)
		isvc := createTestInferenceServiceWithResources(namespace, "isvc-custom",
			"3", "10Gi", "5", "20Gi")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, isvc))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify custom-serving HWP annotation
		updatedIsvc := &unstructured.Unstructured{}
		updatedIsvc.SetGroupVersionKind(gvk.InferenceServices)
		err = cli.Get(ctx, client.ObjectKey{Name: "isvc-custom", Namespace: namespace}, updatedIsvc)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedIsvc.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "custom-serving"))
	})

	t.Run("should use custom-serving for InferenceService without resources", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, namespace)
		isvc := createTestInferenceService(namespace, "isvc-no-resources", "")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, isvc))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify custom-serving HWP annotation
		updatedIsvc := &unstructured.Unstructured{}
		updatedIsvc.SetGroupVersionKind(gvk.InferenceServices)
		err = cli.Get(ctx, client.ObjectKey{Name: "isvc-no-resources", Namespace: namespace}, updatedIsvc)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedIsvc.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "custom-serving"))
	})

	t.Run("should skip InferenceService that already has HWP annotation", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, namespace)
		isvc := createTestInferenceService(namespace, "isvc-with-hwp", "")
		isvc.SetAnnotations(map[string]string{
			"opendatahub.io/hardware-profile-name": "existing-hwp",
		})

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, isvc))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify HWP annotation remains unchanged
		updatedIsvc := &unstructured.Unstructured{}
		updatedIsvc.SetGroupVersionKind(gvk.InferenceServices)
		err = cli.Get(ctx, client.ObjectKey{Name: "isvc-with-hwp", Namespace: namespace}, updatedIsvc)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedIsvc.GetAnnotations()).To(HaveKeyWithValue("opendatahub.io/hardware-profile-name", "existing-hwp"))
	})

	t.Run("should handle no InferenceServices gracefully", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, namespace)
		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should skip Serverless InferenceService with deploymentMode annotation", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, namespace)
		isvc := createTestInferenceService(namespace, "isvc-serverless-annotation", "")
		isvc.SetAnnotations(map[string]string{
			"serving.kserve.io/deploymentMode": "Serverless",
		})

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, isvc))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify NO HWP annotation added (Serverless ISVC should be skipped)
		updatedIsvc := &unstructured.Unstructured{}
		updatedIsvc.SetGroupVersionKind(gvk.InferenceServices)
		err = cli.Get(ctx, client.ObjectKey{Name: "isvc-serverless-annotation", Namespace: namespace}, updatedIsvc)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedIsvc.GetAnnotations()).ToNot(HaveKey("opendatahub.io/hardware-profile-name"))
	})

	t.Run("should skip Serverless InferenceService with deploymentMode in status", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, namespace)
		isvc := createTestInferenceService(namespace, "isvc-serverless-status", "")

		// Set deploymentMode in status
		status := map[string]interface{}{
			"deploymentMode": "Serverless",
		}
		isvc.Object["status"] = status

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig, isvc))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.AttachHardwareProfileToInferenceServices(ctx, cli, namespace, odhConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify NO HWP annotation added (Serverless ISVC should be skipped)
		updatedIsvc := &unstructured.Unstructured{}
		updatedIsvc.SetGroupVersionKind(gvk.InferenceServices)
		err = cli.Get(ctx, client.ObjectKey{Name: "isvc-serverless-status", Namespace: namespace}, updatedIsvc)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedIsvc.GetAnnotations()).ToNot(HaveKey("opendatahub.io/hardware-profile-name"))
	})
}
