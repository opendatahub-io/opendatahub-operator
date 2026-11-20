//nolint:testpackage // Exercises package-private deployment migration wiring directly.
package modules

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"

	. "github.com/onsi/gomega"
)

func TestCustomizeModuleDeploymentsMigratesOnlyPreviousObservabilityDefault(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		name              string
		liveLimitMemory   string
		wantLimitMemory   string
		liveRequestMemory string
		wantRequestMemory string
	}{
		"previous default advances to rendered default": {
			name:              "odh-observability",
			liveLimitMemory:   "512Mi",
			wantLimitMemory:   "3Gi",
			liveRequestMemory: "128Mi",
			wantRequestMemory: "2Gi",
		},
		"custom value is preserved": {
			name:              "odh-observability",
			liveLimitMemory:   "1Gi",
			wantLimitMemory:   "1Gi",
			liveRequestMemory: "128Mi",
			wantRequestMemory: "128Mi",
		},
		"another deployment keeps the same legacy value": {
			name:              "another-module",
			liveLimitMemory:   "512Mi",
			wantLimitMemory:   "512Mi",
			liveRequestMemory: "128Mi",
			wantRequestMemory: "128Mi",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			live := deploymentWithResources(tc.name, tc.liveLimitMemory, tc.liveRequestMemory)
			desired := deploymentWithResources(tc.name, "3Gi", "2Gi")

			err := customizeModuleDeployments(context.Background(), nil, &deploy.Action{}, &desired, &live)
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(managerMemoryLimit(&desired)).Should(Equal(tc.wantLimitMemory))
			g.Expect(managerMemoryRequest(&desired)).Should(Equal(tc.wantRequestMemory))
		})
	}
}

func deploymentWithResources(name, limitMemory, requestMemory string) unstructured.Unstructured {
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "manager",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse(limitMemory),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse(requestMemory),
							},
						},
					}},
				},
			},
		},
	})
	if err != nil {
		panic(err)
	}

	deployment := unstructured.Unstructured{Object: obj}
	deployment.SetName(name)
	deployment.SetNamespace("redhat-ods-applications")
	return deployment
}

func managerMemoryLimit(deployment *unstructured.Unstructured) string {
	return managerMemory(deployment, "limits")
}

func managerMemoryRequest(deployment *unstructured.Unstructured) string {
	return managerMemory(deployment, "requests")
}

func managerMemory(deployment *unstructured.Unstructured, resourceType string) string {
	containers, found, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || len(containers) == 0 {
		return ""
	}
	container, ok := containers[0].(map[string]any)
	if !ok {
		return ""
	}
	resources, ok := container["resources"].(map[string]any)
	if !ok {
		return ""
	}
	resourceList, ok := resources[resourceType].(map[string]any)
	if !ok {
		return ""
	}
	memory, _ := resourceList["memory"].(string)
	return memory
}
