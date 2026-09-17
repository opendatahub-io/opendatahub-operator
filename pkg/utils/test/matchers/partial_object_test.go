package matchers_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers"

	. "github.com/onsi/gomega"
)

func TestMatchPartialObject(t *testing.T) {
	g := NewWithT(t)

	actual := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "example",
			Labels:    map[string]string{"component": "webhook"},
		},
		Data: map[string]string{
			"managed": "true",
			"extra":   "preserved",
		},
	}
	expected := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "example",
		},
		Data: map[string]string{
			"managed": "true",
		},
	}

	g.Expect(actual).To(matchers.MatchPartialObject(expected))
}

func TestMatchPartialObjectAcceptsUnstructuredObjects(t *testing.T) {
	g := NewWithT(t)

	actual := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name": "example",
		},
		"data": map[string]any{
			"managed": "true",
		},
	}}
	expected := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"name": "example",
		},
		"data": map[string]any{
			"managed": "true",
		},
	}}

	g.Expect(actual).To(matchers.MatchPartialObject(expected))
}

func TestMatchPartialObjectRejectsNonObjects(t *testing.T) {
	g := NewWithT(t)

	matched, err := matchers.MatchPartialObject(&corev1.ConfigMap{}).Match("not an object")

	g.Expect(matched).To(BeFalse())
	g.Expect(err).To(MatchError(ContainSubstring("client.Object")))
}

func TestMatchPartialObjectOmitsTypedZeroValues(t *testing.T) {
	g := NewWithT(t)

	actual := &dscv3.DataScienceCluster{
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{
				AIGateway: componentApi.DSCAIGateway{
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}

	g.Expect(actual).To(matchers.MatchPartialObject(&dscv3.DataScienceCluster{
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{
				AIGateway: componentApi.DSCAIGateway{
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
						ModelsAsAService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}))
}
