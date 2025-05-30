package observability_test

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/observability"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestObservabilityAction(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()

	deployment := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name": "test-deployment",
				"labels": map[string]interface{}{
					"existing-top": "top-value",
				},
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{
							"existing-pod": "pod-value",
							"test-label":   "existing-value",
						},
					},
				},
			},
		},
	}

	configMap := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name": "test-configmap",
				"labels": map[string]interface{}{
					"cm-label": "cm-value",
				},
			},
		},
	}

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &types.ReconciliationRequest{
		Client:    cl,
		Resources: []unstructured.Unstructured{deployment, configMap},
	}

	action := observability.NewAction()
	err = action(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify Deployment labels
	deploymentLabels := deployment.GetLabels()
	g.Expect(deploymentLabels).To(HaveKeyWithValue(labels.Scrape, labels.True))
	g.Expect(deploymentLabels).To(HaveKeyWithValue("existing-top", "top-value"))

	// Verify Deployment pod template labels
	podLabels, found, err := unstructured.NestedStringMap(
		deployment.Object,
		"spec", "template", "metadata", "labels",
	)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(podLabels).To(HaveKeyWithValue(labels.Scrape, labels.True))
	g.Expect(podLabels).To(HaveKeyWithValue("test-label", "existing-value"))
	g.Expect(podLabels).To(HaveKeyWithValue("existing-pod", "pod-value"))

	// Verify ConfigMap is unchanged
	configMapLabels := configMap.GetLabels()
	g.Expect(configMapLabels).To(Equal(map[string]string{"cm-label": "cm-value"}))
	_, found, err = unstructured.NestedMap(
		configMap.Object,
		"spec", "template", "metadata", "labels",
	)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(found).To(BeFalse())
}
