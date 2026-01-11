package deploy_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

//nolint:dupl // Test cases intentionally have similar structure for consistency
func TestMergeObservabilityResourcesOverride(t *testing.T) {
	g := NewWithT(t)

	// Source represents the existing resource on the cluster with user modifications
	source := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "opentelemetry.io/v1beta1",
			"kind":       "OpenTelemetryCollector",
			"metadata": map[string]interface{}{
				"name":      "data-science-collector",
				"namespace": "test-namespace",
			},
			"spec": map[string]interface{}{
				"resources": map[string]interface{}{
					"requests": map[string]interface{}{
						"cpu":    "500m",
						"memory": "1Gi",
					},
					"limits": map[string]interface{}{
						"cpu":    "2",
						"memory": "2Gi",
					},
				},
			},
		},
	}

	// Target represents the new desired state from the template (with defaults)
	target := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "opentelemetry.io/v1beta1",
			"kind":       "OpenTelemetryCollector",
			"metadata": map[string]interface{}{
				"name":      "data-science-collector",
				"namespace": "test-namespace",
			},
			"spec": map[string]interface{}{
				"resources": map[string]interface{}{
					"requests": map[string]interface{}{
						"cpu":    "100m",
						"memory": "256Mi",
					},
					"limits": map[string]interface{}{
						"cpu":    "1",
						"memory": "512Mi",
					},
				},
			},
		},
	}

	err := deploy.MergeObservabilityResources(source, target)
	g.Expect(err).ShouldNot(HaveOccurred())

	// After merge, target should have source's resource values (user modifications preserved)
	g.Expect(target).Should(And(
		jq.Match(`.spec.resources.requests.cpu == "500m"`),
		jq.Match(`.spec.resources.requests.memory == "1Gi"`),
		jq.Match(`.spec.resources.limits.cpu == "2"`),
		jq.Match(`.spec.resources.limits.memory == "2Gi"`),
	))
}

func TestMergeObservabilityResourcesNoSourceResources(t *testing.T) {
	g := NewWithT(t)

	// Source has no resources section
	source := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "tempo.grafana.com/v1alpha1",
			"kind":       "TempoStack",
			"metadata": map[string]interface{}{
				"name":      "data-science-tempostack",
				"namespace": "test-namespace",
			},
			"spec": map[string]interface{}{
				"storage": map[string]interface{}{
					"secret": map[string]interface{}{
						"name": "tempo-secret",
						"type": "s3",
					},
				},
			},
		},
	}

	// Target has default resources
	target := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "tempo.grafana.com/v1alpha1",
			"kind":       "TempoStack",
			"metadata": map[string]interface{}{
				"name":      "data-science-tempostack",
				"namespace": "test-namespace",
			},
			"spec": map[string]interface{}{
				"resources": map[string]interface{}{
					"requests": map[string]interface{}{
						"cpu":    "100m",
						"memory": "256Mi",
					},
					"limits": map[string]interface{}{
						"cpu":    "1",
						"memory": "512Mi",
					},
				},
				"storage": map[string]interface{}{
					"secret": map[string]interface{}{
						"name": "tempo-secret",
						"type": "s3",
					},
				},
			},
		},
	}

	err := deploy.MergeObservabilityResources(source, target)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Target should keep its default resources since source has none
	g.Expect(target).Should(And(
		jq.Match(`.spec.resources.requests.cpu == "100m"`),
		jq.Match(`.spec.resources.requests.memory == "256Mi"`),
		jq.Match(`.spec.resources.limits.cpu == "1"`),
		jq.Match(`.spec.resources.limits.memory == "512Mi"`),
	))
}

//nolint:dupl // Test cases intentionally have similar structure for consistency
func TestMergeObservabilityResourcesMonitoringStack(t *testing.T) {
	g := NewWithT(t)

	// Source represents MonitoringStack with user-modified resources
	source := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "monitoring.rhobs/v1alpha1",
			"kind":       "MonitoringStack",
			"metadata": map[string]interface{}{
				"name":      "data-science-monitoringstack",
				"namespace": "test-namespace",
			},
			"spec": map[string]interface{}{
				"resources": map[string]interface{}{
					"requests": map[string]interface{}{
						"cpu":    "200m",
						"memory": "512Mi",
					},
					"limits": map[string]interface{}{
						"cpu":    "1500m",
						"memory": "1Gi",
					},
				},
			},
		},
	}

	// Target with defaults
	target := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "monitoring.rhobs/v1alpha1",
			"kind":       "MonitoringStack",
			"metadata": map[string]interface{}{
				"name":      "data-science-monitoringstack",
				"namespace": "test-namespace",
			},
			"spec": map[string]interface{}{
				"resources": map[string]interface{}{
					"requests": map[string]interface{}{
						"cpu":    "100m",
						"memory": "256Mi",
					},
					"limits": map[string]interface{}{
						"cpu":    "1",
						"memory": "512Mi",
					},
				},
			},
		},
	}

	err := deploy.MergeObservabilityResources(source, target)
	g.Expect(err).ShouldNot(HaveOccurred())

	// User modifications should be preserved
	g.Expect(target).Should(And(
		jq.Match(`.spec.resources.requests.cpu == "200m"`),
		jq.Match(`.spec.resources.requests.memory == "512Mi"`),
		jq.Match(`.spec.resources.limits.cpu == "1500m"`),
		jq.Match(`.spec.resources.limits.memory == "1Gi"`),
	))
}
