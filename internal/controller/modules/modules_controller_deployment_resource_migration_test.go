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

const (
	migrationDeploymentName = "migrating-module-controller"
	migrationSidecarName    = "kube-rbac-proxy"
)

func previousDefaultResources(cpuLimit string) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpuLimit),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}
}

func TestModuleDeploymentCustomizerMigratesOnlyPreviousDefaults(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		deploymentName    string
		containerName     string
		liveCPULimit      string
		liveLimitMemory   string
		liveRequestMemory string
		wantLimitMemory   string
		wantRequestMemory string
	}{
		"previous default advances to rendered default": {
			deploymentName:    migrationDeploymentName,
			liveCPULimit:      "500m",
			liveLimitMemory:   "512Mi",
			liveRequestMemory: "128Mi",
			wantLimitMemory:   "3Gi",
			wantRequestMemory: "2Gi",
		},
		"semantically equal quantities match the previous default": {
			deploymentName:    migrationDeploymentName,
			liveCPULimit:      "0.5",
			liveLimitMemory:   "512Mi",
			liveRequestMemory: "128Mi",
			wantLimitMemory:   "3Gi",
			wantRequestMemory: "2Gi",
		},
		"custom value is preserved": {
			deploymentName:    migrationDeploymentName,
			liveCPULimit:      "500m",
			liveLimitMemory:   "1Gi",
			liveRequestMemory: "128Mi",
			wantLimitMemory:   "1Gi",
			wantRequestMemory: "128Mi",
		},
		"module with custom container name migrates that container": {
			deploymentName:    "custom-container-controller",
			containerName:     "operator",
			liveCPULimit:      "500m",
			liveLimitMemory:   "512Mi",
			liveRequestMemory: "128Mi",
			wantLimitMemory:   "3Gi",
			wantRequestMemory: "2Gi",
		},
		"deployment of a module without previous defaults keeps the same value": {
			deploymentName:    "another-module-controller",
			liveCPULimit:      "500m",
			liveLimitMemory:   "512Mi",
			liveRequestMemory: "128Mi",
			wantLimitMemory:   "512Mi",
			wantRequestMemory: "128Mi",
		},
	}

	reg := &Registry{}
	reg.Add(&testDSCConfiguredHandler{BaseHandler: BaseHandler{Config: ModuleConfig{
		Name:                     "migrating-module",
		DeploymentName:           migrationDeploymentName,
		PreviousDefaultResources: []corev1.ResourceRequirements{previousDefaultResources("500m")},
	}}})
	reg.Add(&testDSCConfiguredHandler{BaseHandler: BaseHandler{Config: ModuleConfig{
		Name:                     "custom-container-module",
		DeploymentName:           "custom-container-controller",
		ContainerName:            "operator",
		PreviousDefaultResources: []corev1.ResourceRequirements{previousDefaultResources("500m")},
	}}})
	reg.Add(&testDSCConfiguredHandler{BaseHandler: BaseHandler{Config: ModuleConfig{
		Name:           "another-module",
		DeploymentName: "another-module-controller",
	}}})
	customizer := newModuleDeploymentCustomizer(reg)

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			containerName := tc.containerName
			if containerName == "" {
				containerName = defaultContainerName
			}

			live := deploymentWithResources(tc.deploymentName, containerName, tc.liveCPULimit, tc.liveLimitMemory, tc.liveRequestMemory, "64Mi")
			desired := deploymentWithResources(tc.deploymentName, containerName, "500m", "3Gi", "2Gi", "32Mi")

			err := customizer(context.Background(), nil, &deploy.Action{}, &desired, &live)
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(containerMemory(&desired, containerName, "limits")).Should(Equal(tc.wantLimitMemory))
			g.Expect(containerMemory(&desired, containerName, "requests")).Should(Equal(tc.wantRequestMemory))
			// Sidecar tuning is never touched by the migration.
			g.Expect(containerMemory(&desired, migrationSidecarName, "limits")).Should(Equal("64Mi"))
		})
	}
}

func deploymentWithResources(name, containerName, cpuLimit, limitMemory, requestMemory, sidecarLimitMemory string) unstructured.Unstructured {
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: migrationSidecarName,
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse(sidecarLimitMemory),
								},
							},
						},
						{
							Name: containerName,
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse(cpuLimit),
									corev1.ResourceMemory: resource.MustParse(limitMemory),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse(requestMemory),
								},
							},
						},
					},
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

func containerMemory(deployment *unstructured.Unstructured, containerName, resourceType string) string {
	containers, found, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	if err != nil || !found {
		return ""
	}
	idx := findNamedContainer(containers, containerName)
	if idx < 0 {
		return ""
	}
	container, ok := containers[idx].(map[string]any)
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
