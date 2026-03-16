package deploy_test

import (
	"context"
	"testing"

	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

func createTestDeployment(ns string, replicas *int32, res *corev1.ResourceRequirements) *appsv1.Deployment {
	dep := &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: appsv1.SchemeGroupVersion.String(), Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: "test-deploy", Namespace: ns},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "test", Image: "test:latest"}}},
			},
		},
	}
	if res != nil {
		dep.Spec.Template.Spec.Containers[0].Resources = *res
	}
	return dep
}

func TestRevertManagedDeploymentDrift(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	et, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = et.Stop() })

	i32 := func(i int32) *int32 { return &i }

	type deploymentSpec struct {
		replicas  *int32
		resources *corev1.ResourceRequirements
	}

	tests := []struct {
		name            string
		deployed        deploymentSpec
		manifest        deploymentSpec
		expectRep       int32
		expectResources *corev1.ResourceRequirements
		expectNoPatch   bool
	}{
		{
			name: "clears resources",
			deployed: deploymentSpec{
				replicas: i32(3),
				resources: &corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")},
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
				},
			},
			manifest:  deploymentSpec{replicas: i32(3)},
			expectRep: 3,
		},
		{
			name:      "replicas drift",
			deployed:  deploymentSpec{replicas: i32(5)},
			manifest:  deploymentSpec{},
			expectRep: 1,
		},
		{
			name: "both resources and replicas drift",
			deployed: deploymentSpec{
				replicas: i32(5),
				resources: &corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")},
				},
			},
			manifest:  deploymentSpec{},
			expectRep: 1,
		},
		{
			name: "resets resources to manifest values when both differ",
			deployed: deploymentSpec{
				replicas: i32(1),
				resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("64Mi"),
						corev1.ResourceCPU:    resource.MustParse("250m"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("128Mi"),
						corev1.ResourceCPU:    resource.MustParse("500m"),
					},
				},
			},
			manifest: deploymentSpec{
				replicas: i32(1),
				resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
					Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
				},
			},
			expectRep: 1,
			expectResources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
				Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
			},
		},
		{
			name:          "no drift",
			deployed:      deploymentSpec{},
			manifest:      deploymentSpec{},
			expectRep:     1,
			expectNoPatch: true,
		},
		{
			name: "no drift with resources and replicas",
			deployed: deploymentSpec{
				replicas: i32(3),
				resources: &corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")},
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
				},
			},
			manifest: deploymentSpec{
				replicas: i32(3),
				resources: &corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")},
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
				},
			},
			expectRep:     3,
			expectNoPatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ns := xid.New().String()
			g.Expect(et.Client().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})).To(Succeed())

			deployed := createTestDeployment(ns, tt.deployed.replicas, tt.deployed.resources)
			g.Expect(et.Client().Create(ctx, deployed)).To(Succeed())
			originalResourceVersion := deployed.ResourceVersion

			manifest := createTestDeployment(ns, tt.manifest.replicas, tt.manifest.resources)

			manifestU, err := resources.ObjectToUnstructured(et.Scheme(), manifest)
			g.Expect(err).NotTo(HaveOccurred())
			deployedU, err := resources.ObjectToUnstructured(et.Scheme(), deployed)
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(deploy.RevertManagedDeploymentDrift(ctx, et.Client(), manifestU, deployedU)).To(Succeed())

			result := &appsv1.Deployment{}
			g.Expect(et.Client().Get(ctx, client.ObjectKey{Namespace: ns, Name: "test-deploy"}, result)).To(Succeed())
			g.Expect(*result.Spec.Replicas).To(Equal(tt.expectRep))

			if tt.expectResources != nil {
				g.Expect(result.Spec.Template.Spec.Containers[0].Resources.Requests).To(Equal(tt.expectResources.Requests))
				g.Expect(result.Spec.Template.Spec.Containers[0].Resources.Limits).To(Equal(tt.expectResources.Limits))
			} else if tt.manifest.resources == nil {
				g.Expect(result.Spec.Template.Spec.Containers[0].Resources.Limits).To(BeEmpty())
				g.Expect(result.Spec.Template.Spec.Containers[0].Resources.Requests).To(BeEmpty())
			}

			if tt.expectNoPatch {
				g.Expect(result.ResourceVersion).To(Equal(originalResourceVersion),
					"ResourceVersion should be unchanged when no drift exists")
			}
		})
	}
}
